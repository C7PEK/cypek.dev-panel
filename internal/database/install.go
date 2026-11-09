package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/C7PEK/cypek.dev-panel/internal/safefs"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// InstallOptions tune the root-side installer.
type InstallOptions struct {
	PhpMyAdminPort int
	CredsPath      string
	LogPath        string
	// PanelUser owns the log and may read the credentials.
	PanelUser string
}

// Install sets up MariaDB, the panel admin account, phpMyAdmin behind PHP's
// built-in server and the credentials file. Idempotent; must run as root.
// Every external program is exec'd with an argument list, never a shell.
func Install(ctx context.Context, opts InstallOptions, out io.Writer) (err error) {
	if opts.PhpMyAdminPort == 0 {
		opts.PhpMyAdminPort = DefaultPMAPort
	}
	if opts.CredsPath == "" {
		opts.CredsPath = CredsPath
	}
	if opts.LogPath == "" {
		opts.LogPath = InstallLog
	}
	if opts.PanelUser == "" {
		opts.PanelUser = "homepanel"
	}
	if os.Geteuid() != 0 {
		return errors.New("install-database must run as root")
	}
	logf, closeLog := openLog(opts.LogPath, opts.PanelUser, out)
	defer closeLog()
	defer func() {
		if err != nil {
			logf("FAILED: %v", err)
		} else {
			logf("DONE")
		}
	}()
	env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a")
	run := func(stdin string, name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = env
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		w := lineWriter{logf: logf}
		cmd.Stdout, cmd.Stderr = &w, &w
		err := cmd.Run()
		w.flush()
		if err != nil {
			return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return nil
	}

	logf("Step 1/5: installing packages (MariaDB, PHP, phpMyAdmin)")
	// Keep phpMyAdmin's package from asking about databases and web servers.
	_ = run("phpmyadmin phpmyadmin/dbconfig-install boolean false\nphpmyadmin phpmyadmin/reconfigure-webserver multiselect\n", "debconf-set-selections")
	if err := run("", "apt-get", "update", "-q"); err != nil {
		return err
	}
	pkgs := []string{"install", "-y", "-q", "--no-install-recommends", "mariadb-server", "mariadb-client",
		"php-cli", "php-mysql", "php-mbstring", "php-xml", "php-zip", "php-gd", "php-curl", "php-intl", "phpmyadmin"}
	if err := run("", "apt-get", pkgs...); err != nil {
		return err
	}

	logf("Step 2/5: tuning and starting MariaDB")
	serverVersion := InstalledServerVersion()
	tuning := TuningConfig(TotalRAM(), SupportsBindList(serverVersion))
	if err := os.MkdirAll("/etc/mysql/mariadb.conf.d", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(TuningPath, []byte(tuning), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", TuningPath, err)
	}
	_ = os.MkdirAll("/var/log/mysql", 0o750)
	if u, err := user.Lookup("mysql"); err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		_ = os.Chown("/var/log/mysql", uid, gid)
	}
	for _, l := range strings.Split(strings.TrimSpace(tuning), "\n") {
		if strings.Contains(l, "=") && !strings.HasPrefix(l, "#") {
			logf("  %s", strings.Join(strings.Fields(l), " "))
		}
	}
	if err := run("", "systemctl", "enable", "mariadb"); err != nil {
		return err
	}
	// Restart (not just start) so the tuning applies on re-runs too.
	if err := run("", "systemctl", "restart", "mariadb"); err != nil {
		return err
	}
	if err := waitSocket(ctx, DefaultSocket, 60*time.Second); err != nil {
		return err
	}

	logf("Step 3/5: creating the panel's database account")
	creds, _ := LoadCreds(opts.CredsPath)
	password := ""
	if creds != nil && ValidPassword(creds.AdminPassword) {
		password = creds.AdminPassword
	} else if password, err = NewPassword(32); err != nil {
		return err
	}
	// root authenticates through the unix socket on Debian/Ubuntu.
	sql := strings.Join([]string{
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'", AdminUser, password),
		fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s'", AdminUser, password),
		// Full control over hp_* databases only, plus account management for
		// the per-server users it creates.
		fmt.Sprintf("GRANT ALL PRIVILEGES ON `hp\\_%%`.* TO '%s'@'localhost' WITH GRANT OPTION", AdminUser),
		fmt.Sprintf("GRANT CREATE USER, PROCESS ON *.* TO '%s'@'localhost'", AdminUser),
		"FLUSH PRIVILEGES",
	}, ";\n") + ";\n"
	if err := runQuiet(ctx, env, sql, "mariadb", "--protocol=socket", "-u", "root"); err != nil {
		return fmt.Errorf("create admin account: %w", err)
	}
	version := ""
	if b, err := exec.CommandContext(ctx, "mariadb", "--protocol=socket", "-u", "root", "-N", "-e", "SELECT VERSION()").Output(); err == nil {
		version = strings.TrimSpace(string(b))
	}

	logf("Step 4/5: setting up phpMyAdmin on port %d", opts.PhpMyAdminPort)
	php, err := exec.LookPath("php")
	if err != nil {
		return errors.New("php was not installed")
	}
	if err := os.MkdirAll(filepath.Dir(PhpMyAdminRouter), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(PhpMyAdminRouter, []byte(phpMyAdminRouter), 0o644); err != nil {
		return err
	}
	unit := fmt.Sprintf(`[Unit]
Description=phpMyAdmin for panel.cypek.dev (PHP built-in server)
After=network-online.target mariadb.service
Wants=mariadb.service

[Service]
Type=simple
User=www-data
Group=www-data
Environment=PHP_CLI_SERVER_WORKERS=4
ExecStart=%s -d memory_limit=256M -d upload_max_filesize=256M -d post_max_size=256M -d max_execution_time=600 -S 0.0.0.0:%d -t /usr/share/phpmyadmin %s
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
# PHP keeps sessions here; phpMyAdmin keeps its temp files in /var/lib/phpmyadmin.
ReadWritePaths=/var/lib/phpmyadmin /var/lib/php/sessions

[Install]
WantedBy=multi-user.target
`, php, opts.PhpMyAdminPort, PhpMyAdminRouter)
	if err := os.WriteFile("/etc/systemd/system/"+PhpMyAdminUnit, []byte(unit), 0o644); err != nil {
		return err
	}
	if err := run("", "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := run("", "systemctl", "enable", "--now", PhpMyAdminUnit); err != nil {
		return err
	}
	if err := run("", "systemctl", "restart", PhpMyAdminUnit); err != nil {
		return err
	}

	logf("Step 5/5: saving credentials for the panel")
	b, _ := json.MarshalIndent(Creds{Socket: DefaultSocket, Host: "127.0.0.1", Port: 3306, AdminUser: AdminUser, AdminPassword: password, PhpMyAdminPort: opts.PhpMyAdminPort, Version: version}, "", "  ")
	// We run as root in /etc/homepanel, which the panel user owns: create
	// the temp file exclusively (no planted symlink) and fchown it.
	tmp := opts.CredsPath + ".tmp"
	f, err := safefs.CreateExclusive(tmp, 0o640)
	if err != nil {
		return err
	}
	if u, err := user.Lookup(opts.PanelUser); err == nil {
		gid, _ := strconv.Atoi(u.Gid)
		_ = f.Chown(0, gid)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, opts.CredsPath); err != nil {
		return err
	}
	logf("MariaDB %s ready; phpMyAdmin at http://<this-machine>:%d", version, opts.PhpMyAdminPort)
	return nil
}

// runQuiet feeds SQL on stdin and never logs it (it contains a password).
func runQuiet(ctx context.Context, env []string, stdin, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func waitSocket(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("MariaDB socket %s did not appear", path)
}

// openLog truncates the progress log, makes it readable by the panel user
// and returns a logger writing to it and to out.
func openLog(path, panelUser string, out io.Writer) (func(string, ...any), func()) {
	// Root writing into /var/lib/homepanel (owned by the panel user): never
	// truncate or chown through a planted symlink.
	f, err := safefs.CreateExclusive(path, 0o640)
	if err == nil {
		if u, err := user.Lookup(panelUser); err == nil {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			_ = f.Chown(uid, gid)
		}
	}
	logf := func(format string, args ...any) {
		line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...) + "\n"
		if out != nil {
			_, _ = io.WriteString(out, line)
		}
		if f != nil {
			_, _ = f.WriteString(line)
		}
	}
	return logf, func() {
		if f != nil {
			f.Close()
		}
	}
}

// lineWriter turns command output into log lines.
type lineWriter struct {
	logf func(string, ...any)
	buf  []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		if line := strings.TrimSpace(string(w.buf[:i])); line != "" {
			w.logf("  %s", line)
		}
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if line := strings.TrimSpace(string(w.buf)); line != "" {
		w.logf("  %s", line)
	}
	w.buf = nil
}

// ReadInstallLog returns the installer's progress log.
func ReadInstallLog(path string) (lines []string, done, failed bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false, false
	}
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if l == "" {
			continue
		}
		lines = append(lines, l)
	}
	if n := len(lines); n > 0 {
		last := lines[n-1]
		done = strings.HasSuffix(last, " DONE")
		failed = strings.Contains(last, " FAILED: ")
	}
	if len(lines) > 400 {
		lines = lines[len(lines)-400:]
	}
	return lines, done, failed
}

// PhpMyAdminRouter limits what PHP's built-in server exposes.
const PhpMyAdminRouter = "/usr/local/lib/homepanel/phpmyadmin-router.php"

// phpMyAdminRouter serves only the entry points and assets of phpMyAdmin's
// web UI. Apache's Debian config denies libraries/, templates/, sql/,
// vendor/, setup/ and friends; PHP's built-in server has no such rules and
// would otherwise run any .php file under the document root.
const phpMyAdminRouter = `<?php
// Installed by panel.cypek.dev; regenerated by the database installer.
$path = rawurldecode(parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH) ?? '/');
if (strpos($path, "\0") !== false || preg_match('#(^|/)\.\.?(/|$)#', $path)) {
    http_response_code(400);
    exit;
}
// Entry points phpMyAdmin 5 links to.
if (in_array($path, ['/', '/index.php', '/url.php', '/js/messages.php'], true)) {
    return false;
}
// Static assets only (never a .php file under these folders).
if (preg_match('#^/(js|themes)/[A-Za-z0-9_./-]+\.(js|css|map|png|svg|gif|jpg|jpeg|ico|woff2?|ttf|eot)$#', $path)
    || $path === '/favicon.ico' || $path === '/robots.txt') {
    return false;
}
http_response_code(404);
echo 'Not found';
`

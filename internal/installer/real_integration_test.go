package installer

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
	"github.com/C7PEK/cypek.dev-panel/internal/runtimes"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
	builtin "github.com/C7PEK/cypek.dev-panel/templates"
)

// TestRealPaperInstall downloads the real Paper jar and a real Temurin JRE
// (~110 MB), then boots the server through the shim until it reports ready
// and stops it. Opt-in: HOMEPANEL_REAL_INSTALL=1, optional HOMEPANEL_REAL_DIR
// for the data root (default a temp dir; use /var/lib/homepanel to prime the
// runtime cache on a real host).
func TestRealPaperInstall(t *testing.T) {
	if os.Getenv("HOMEPANEL_REAL_INSTALL") != "1" {
		t.Skip("set HOMEPANEL_REAL_INSTALL=1 to download real artifacts")
	}
	realBoot(t, "minecraft-paper", map[string]string{"MC_VERSION": "latest", "EULA": "true", "RAM_MB": "1024", "PORT": "25599", "MOTD": "panel.cypek.dev test"})
}

// TestRealTemplates installs and boots each template named in
// HOMEPANEL_REAL_TEMPLATES (comma separated, e.g.
// minecraft-forge,minecraft-neoforge) with its defaults.
func TestRealTemplates(t *testing.T) {
	list := os.Getenv("HOMEPANEL_REAL_TEMPLATES")
	if list == "" {
		t.Skip("set HOMEPANEL_REAL_TEMPLATES=id,id to download real artifacts")
	}
	extra := map[string]map[string]string{
		"minecraft-forge-1.12.2":    {"MC_VERSION": "1.12.2"},
		"minecraft-forge-1.20.1":    {"MC_VERSION": "1.20.1"},
		"minecraft-neoforge-1.21.1": {"MC_VERSION": "1.21.1"},
	}
	for i, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		id, vars := name, map[string]string{"EULA": "true", "RAM_MB": "1024", "PORT": strconv.Itoa(25700 + i)}
		for k, v := range extra[name] {
			vars[k] = v
			id = strings.TrimSuffix(name, "-"+v)
		}
		t.Run(name, func(t *testing.T) { realBoot(t, id, vars) })
	}
}

// realBoot runs a real install of template id (real downloads, a real
// Temurin JRE), boots it through the shim until ready, then stops it.
// HOMEPANEL_REAL_DIR sets the data root (default a temp dir; use
// /var/lib/homepanel to reuse the runtime cache on a real host).
func realBoot(t *testing.T, id string, values map[string]string) {
	root := os.Getenv("HOMEPANEL_REAL_DIR")
	if root == "" {
		root = t.TempDir()
	}
	serverDir := filepath.Join(root, "servers", "realtest-"+id)
	os.RemoveAll(serverDir)
	t.Cleanup(func() { os.RemoveAll(serverDir) })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	m := New(Deps{Paper: providers.NewPaper(), Java: &runtimes.Java{Dir: filepath.Join(root, "runtimes"), Adoptium: providers.NewAdoptium()}, Log: log})
	reg, _ := templates.Load(builtin.Builtin, "")
	tpl, ok := reg.Get(id)
	if !ok {
		t.Fatalf("no template %s", id)
	}
	for k := range values {
		if tpl.Variable(k) == nil {
			delete(values, k)
		}
	}
	vars, err := tpl.ValidateValues(values)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	done := make(chan struct{})
	var res *Result
	var ierr error
	installStart := time.Now()
	if err := m.Start(ctx, "realtest", serverDir, tpl, vars, func(r *Result, e error) { res, ierr = r, e; close(done) }); err != nil {
		t.Fatal(err)
	}
	<-done
	job, _ := m.Get("realtest")
	if ierr != nil {
		t.Fatalf("install: %v\n%s", ierr, tail(strings.Join(job.Lines, "\n"), 40))
	}
	t.Logf("installed in %s:\n%s", time.Since(installStart).Round(time.Second), tail(strings.Join(job.Lines, "\n"), 12))
	if os.Getenv("HOMEPANEL_REAL_NOBOOT") == "1" {
		// Install only (e.g. a binary this host cannot or may not run).
		inspectInstall(t, id, serverDir, m, tpl, vars)
		return
	}
	if err := shim.SaveRunConfig(serverDir, &res.Run); err != nil {
		t.Fatal(err)
	}
	cfg, _ := shim.LoadRunConfig(serverDir)
	sock := filepath.Join(os.TempDir(), "hp-realtest.sock")
	s, err := shim.New(cfg, shim.Options{ID: "realtest", ServerDir: serverDir, SocketPath: sock, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	exit := make(chan int, 1)
	go func() {
		code, err := s.Run(runCtx)
		if err != nil {
			t.Errorf("shim: %v", err)
		}
		exit <- code
	}()
	var c *shim.Client
	for i := 0; i < 100 && c == nil; i++ {
		time.Sleep(100 * time.Millisecond)
		c, _ = shim.Dial(ctx, sock)
	}
	if c == nil {
		t.Fatal("shim socket never came up")
	}
	defer c.Close()
	start := time.Now()
	var out strings.Builder
	ready := c.Hello.State == shim.StateRunning
	for !ready {
		select {
		case f, ok := <-c.Frames():
			if !ok {
				t.Fatalf("shim closed before ready; output:\n%s", tail(out.String(), 40))
			}
			if f.Type == shim.TypeOutput {
				out.WriteString(f.Data)
			}
			if f.Type == shim.TypeState && f.State == shim.StateRunning {
				ready = true
			}
			if f.Type == shim.TypeState && f.State == shim.StateStopped {
				t.Fatalf("server exited before ready; output:\n%s", tail(out.String(), 60))
			}
		case <-time.After(6 * time.Minute):
			t.Fatalf("server not ready after 6 minutes; output:\n%s", tail(out.String(), 60))
		}
	}
	t.Logf("%s ready after %s", id, time.Since(start).Round(time.Second))
	_ = c.Send(shim.Frame{Type: shim.TypeStop})
	select {
	case code := <-exit:
		if code != 0 {
			t.Errorf("exit code %d", code)
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("server did not stop")
	}
}

// inspectInstall checks an install that was not booted: the Bedrock
// server is executable, its settings were applied, and a reinstall keeps
// the user's server.properties.
func inspectInstall(t *testing.T, id, dir string, m *Manager, tpl *templates.Template, vars map[string]string) {
	if id != "minecraft-bedrock" {
		return
	}
	st, err := os.Stat(filepath.Join(dir, "bedrock_server"))
	if err != nil || st.Mode()&0o100 == 0 {
		t.Fatalf("bedrock_server missing or not executable: %v %v", st, err)
	}
	props, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.Contains(string(props), "server-port="+vars["PORT"]+"\n") || !strings.Contains(string(props), "server-portv6="+vars["PORT_V6"]+"\n") {
		t.Fatalf("server.properties not updated")
	}
	if _, err := os.Stat(filepath.Join(dir, "bedrock-server.zip")); err == nil {
		t.Error("zip left behind")
	}
	// The user edits a setting; reinstall must keep it.
	edited := strings.Replace(string(props), "max-players=", "difficulty=hard\nmax-players=", 1)
	_ = os.WriteFile(filepath.Join(dir, "server.properties"), []byte(edited), 0o644)
	done := make(chan error, 1)
	if err := m.Start(context.Background(), "realtest", dir, tpl, vars, func(_ *Result, e error) { done <- e }); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.Contains(string(after), "difficulty=hard\n") {
		t.Error("reinstall overwrote the user's server.properties")
	}
	t.Logf("bedrock install checked (%d bytes of server.properties kept)", len(after))
}

func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

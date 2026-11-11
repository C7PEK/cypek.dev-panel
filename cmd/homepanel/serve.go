package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/alerts"
	"github.com/C7PEK/cypek.dev-panel/internal/api"
	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
	"github.com/C7PEK/cypek.dev-panel/internal/backups"
	"github.com/C7PEK/cypek.dev-panel/internal/config"
	"github.com/C7PEK/cypek.dev-panel/internal/database"
	"github.com/C7PEK/cypek.dev-panel/internal/db"
	"github.com/C7PEK/cypek.dev-panel/internal/installer"
	"github.com/C7PEK/cypek.dev-panel/internal/netinfo"
	"github.com/C7PEK/cypek.dev-panel/internal/providers"
	"github.com/C7PEK/cypek.dev-panel/internal/runtimes"
	"github.com/C7PEK/cypek.dev-panel/internal/scheduler"
	"github.com/C7PEK/cypek.dev-panel/internal/secrets"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/sftpd"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
	"github.com/C7PEK/cypek.dev-panel/internal/tlsutil"
	builtinTemplates "github.com/C7PEK/cypek.dev-panel/templates"
	"github.com/C7PEK/cypek.dev-panel/web"
)

const devViteURL = "http://127.0.0.1:5173"

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath, "path to config.toml")
	dev := fs.Bool("dev", false, "development mode: data in ./.devdata, frontend proxied from the Vite dev server")
	listen := fs.String("listen", "", "override listen address (e.g. 127.0.0.1:8420)")
	logLevel := fs.String("log-level", "info", "log level: debug, info, warn, error")
	if err := fs.Parse(args); err != nil {
		return err
	}

	log := newLogger(*logLevel)

	cfg, err := loadConfig(*configPath, *dev)
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	sqlDB, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := db.Migrate(ctx, sqlDB, cfg.DBPath(), log); err != nil {
		return err
	}

	store := auth.NewStore(sqlDB)
	auditLog := audit.New(sqlDB, log)
	serverStore := servers.NewStore(sqlDB, cfg.ServersDir())

	sup, err := newSupervisor(ctx, cfg, log)
	if err != nil {
		return err
	}
	if closer, ok := sup.(interface{ Close() }); ok {
		defer closer.Close()
	}

	setupToken, _, err := store.EnsureSetupToken(ctx, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("prepare setup token: %w", err)
	}

	registry, err := templates.Load(builtinTemplates.Builtin, cfg.UserTemplatesDir())
	if err != nil {
		return fmt.Errorf("load templates: %w", err)
	}
	log.Info("templates loaded", "count", len(registry.List()))
	key, err := cfg.SecretKeyBytes()
	if err != nil {
		return fmt.Errorf("encryption_key: %w", err)
	}
	box, err := secrets.New(key)
	if err != nil {
		return err
	}
	paper := providers.NewPaper()
	fivem := providers.NewFiveM()
	inst := installer.New(installer.Deps{
		Paper:  paper,
		FiveM:  fivem,
		Java:   &runtimes.Java{Dir: cfg.RuntimesDir(), Adoptium: providers.NewAdoptium()},
		Node:   &runtimes.Node{Dir: cfg.RuntimesDir(), Provider: providers.NewNode()},
		Python: &runtimes.Python{Dir: cfg.RuntimesDir(), Provider: providers.NewPython()},
		Log:    log,
	})

	deps := api.Deps{
		Config:    cfg,
		Store:     store,
		Audit:     auditLog,
		Log:       log,
		Version:   version,
		Servers:   serverStore,
		Sup:       sup,
		Templates: registry,
		Installer: inst,
		Paper:     paper,
		FiveM:     fivem,
		Secrets:   box,
		Schedules: scheduler.NewStore(sqlDB),
		Backups:   backups.NewStore(sqlDB),
		DBRecords: database.NewStore(sqlDB),
		Alerts:    alerts.NewStore(sqlDB, box),
		Databases: database.NewManager(database.CredsPath),
		BgCtx:     ctx,
		Dist:      mustSub(web.Dist, "dist"),
	}
	if cfg.Dev {
		deps.DevProxy = devViteURL
		log.Info("dev mode: proxying the frontend from Vite when it is running, embedded build otherwise", "vite", devViteURL)
	}
	var sftpSrv *sftpd.Server
	if cfg.SFTP.Enabled {
		sftpSrv = &sftpd.Server{Listen: cfg.SFTP.Listen, HostKeyPath: filepath.Join(cfg.DataDir, "sftp_host_ed25519_key"), Log: log}
		if err := sftpSrv.LoadHostKey(); err != nil {
			log.Warn("SFTP disabled: host key", "err", err)
			sftpSrv = nil
		}
	}
	if sftpSrv != nil {
		deps.SFTPInfo = api.SFTPInfo{Enabled: true, Port: portOf(cfg.SFTP.Listen), Fingerprint: sftpSrv.Fingerprint()}
		deps.ExposeSFTP = func(b sftpd.Backend) { sftpSrv.Backend = b }
	}
	handler, err := api.New(deps)
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}
	if sftpSrv != nil {
		go func() {
			log.Info("SFTP server listening", "listen", cfg.SFTP.Listen, "hostKey", sftpSrv.Fingerprint())
			if err := sftpSrv.Run(ctx); err != nil {
				log.Error("SFTP server stopped", "err", err)
			}
		}()
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}

	if cfg.TLS.Enabled && cfg.TLS.SelfSigned && (cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "") {
		hosts := []string{}
		if h, err := os.Hostname(); err == nil {
			hosts = append(hosts, h)
		}
		if addrs, err := netinfo.LocalAddresses(); err == nil {
			for _, a := range addrs {
				hosts = append(hosts, a.IP)
			}
		}
		certFile, keyFile, fp, err := tlsutil.EnsureSelfSigned(filepath.Join(cfg.DataDir, "tls"), hosts)
		if err != nil {
			return fmt.Errorf("self-signed certificate: %w", err)
		}
		cfg.TLS.CertFile, cfg.TLS.KeyFile = certFile, keyFile
		log.Info("HTTPS with a self-signed certificate; the browser will warn once, compare this fingerprint", "sha256", fp)
		fmt.Printf("\n  HTTPS certificate (self-signed) SHA-256:\n    %s\n", fp)
	}
	printBanner(cfg, setupToken)
	log.Info("panel started", "version", version, "listen", cfg.Listen, "dev", cfg.Dev, "data", cfg.DataDir, "supervisor", sup.Name())

	go sessionJanitor(ctx, store, log)
	go autostartServers(ctx, serverStore, registry, sup, log)

	errCh := make(chan error, 1)
	go func() {
		var err error
		if cfg.TLS.Enabled {
			err = srv.ServeTLS(ln, cfg.TLS.CertFile, cfg.TLS.KeyFile)
		} else {
			err = srv.Serve(ln)
		}
		if !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func loadConfig(path string, dev bool) (*config.Config, error) {
	if !dev {
		cfg, err := config.Load(path)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}
	devDir, err := filepath.Abs(config.DevDataDir)
	if err != nil {
		return nil, err
	}
	base := config.Default()
	base.Listen = fmt.Sprintf("127.0.0.1:%d", config.DefaultPort)
	base.SFTP.Listen = fmt.Sprintf("127.0.0.1:%d", config.DefaultSFTPPort)
	base.DataDir = devDir
	base.LogDir = filepath.Join(devDir, "log")
	base.Supervisor = "local"
	if path == config.DefaultPath {
		path = filepath.Join(devDir, "config.toml")
	}
	cfg, err := config.LoadOrCreate(path, base)
	if err != nil {
		return nil, err
	}
	cfg.Dev = true
	cfg.Supervisor = "local"
	return cfg, nil
}

func newSupervisor(ctx context.Context, cfg *config.Config, log *slog.Logger) (supervisor.Supervisor, error) {
	if cfg.Supervisor == "local" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locate own binary for shim: %w", err)
		}
		layout := supervisor.Layout{ServersDir: cfg.ServersDir(), RunDir: cfg.LocalRunDir()}
		return supervisor.NewLocal(layout, exe, log), nil
	}
	layout := supervisor.Layout{ServersDir: cfg.ServersDir(), RunDir: "/run", FlatRun: true}
	sup, err := supervisor.NewSystemd(ctx, layout, log)
	if err != nil {
		return nil, fmt.Errorf("systemd supervisor: %w (set supervisor = \"local\" in config.toml to run without systemd)", err)
	}
	return sup, nil
}

func printBanner(cfg *config.Config, setupToken string) {
	addrs, _ := netinfo.LocalAddresses()
	urls := netinfo.URLs(cfg.Listen, cfg.TLS.Enabled, addrs)
	var b strings.Builder
	b.WriteString("\n============================================================\n")
	b.WriteString(fmt.Sprintf("  panel.cypek.dev %s is running\n\n", version))
	b.WriteString("  Open in your browser:\n")
	for _, u := range urls {
		b.WriteString("    " + u + "\n")
	}
	if setupToken != "" {
		b.WriteString("\n  First run: create the owner account with this setup token:\n")
		b.WriteString("    " + setupToken + "\n")
		b.WriteString(fmt.Sprintf("  (also saved in %s)\n", filepath.Join(cfg.DataDir, auth.SetupTokenFile)))
	}
	b.WriteString("============================================================\n\n")
	fmt.Fprint(os.Stderr, b.String())
}

func autostartServers(ctx context.Context, store *servers.Store, registry *templates.Registry, sup supervisor.Supervisor, log *slog.Logger) {
	list, err := store.List(ctx, nil)
	if err != nil {
		log.Error("autostart: list servers", "err", err)
		return
	}
	for _, srv := range list {
		if !srv.Autostart || ctx.Err() != nil {
			continue
		}
		if srv.InstallState != servers.InstallReady {
			log.Warn("autostart: server not installed, skipping", "server", srv.ID, "name", srv.Name)
			continue
		}
		if err := store.CheckStartable(ctx, srv.ID); err != nil {
			log.Info("autostart: server expired, not starting", "server", srv.ID, "name", srv.Name)
			continue
		}
		if tpl, ok := registry.Get(srv.TemplateID); ok && srv.TemplateID != "custom" {
			if changed, err := templates.ApplyConfigDefaults(store.Dir(srv.ID), tpl); err == nil && len(changed) > 0 {
				log.Info("autostart: config defaults added", "server", srv.ID, "files", changed)
			}
		}
		if msg := servers.CommandProblem(srv.Run.Command, store.Dir(srv.ID)); msg != "" {
			log.Warn("autostart: skipping server with a broken command", "server", srv.ID, "name", srv.Name, "problem", msg)
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := sup.Start(sctx, srv.ID)
		cancel()
		switch {
		case err == nil:
			log.Info("autostart: server started", "server", srv.ID, "name", srv.Name)
		case errors.Is(err, supervisor.ErrAlreadyRunning):
		default:
			log.Warn("autostart: start failed", "server", srv.ID, "name", srv.Name, "err", err)
		}
	}
}

func sessionJanitor(ctx context.Context, store *auth.Store, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := store.PurgeExpiredSessions(ctx); err != nil {
				log.Warn("purge sessions", "err", err)
			} else if n > 0 {
				log.Debug("purged expired sessions", "count", n)
			}
		}
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

func portOf(listen string) int {
	_, p, err := net.SplitHostPort(listen)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

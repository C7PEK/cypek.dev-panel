// Package api wires HTTP routes, middleware and the embedded frontend.
package api

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"testing/fstest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/C7PEK/cypek.dev-panel/internal/alerts"
	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
	"github.com/C7PEK/cypek.dev-panel/internal/backups"
	"github.com/C7PEK/cypek.dev-panel/internal/config"
	"github.com/C7PEK/cypek.dev-panel/internal/database"
	"github.com/C7PEK/cypek.dev-panel/internal/installer"
	"github.com/C7PEK/cypek.dev-panel/internal/metrics"
	"github.com/C7PEK/cypek.dev-panel/internal/mods"
	"github.com/C7PEK/cypek.dev-panel/internal/providers"
	"github.com/C7PEK/cypek.dev-panel/internal/query"
	"github.com/C7PEK/cypek.dev-panel/internal/scheduler"
	"github.com/C7PEK/cypek.dev-panel/internal/secrets"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/sftpd"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

// Deps are the collaborators the API needs.
type Deps struct {
	Config  *config.Config
	Store   *auth.Store
	Audit   *audit.Logger
	Log     *slog.Logger
	Version string
	Servers *servers.Store
	Sup     supervisor.Supervisor
	// Templates, Installer, Paper and Secrets back server creation from templates.
	Templates *templates.Registry
	Installer *installer.Manager
	Paper     *providers.Paper
	// Alerts stores Discord alert settings (nil disables alerts).
	Alerts *alerts.Store
	// SFTPInfo describes the built-in SFTP server for the UI; ExposeSFTP
	// receives the backend the SFTP server authenticates against.
	SFTPInfo   SFTPInfo
	ExposeSFTP func(sftpd.Backend)
	// onServer hands tests the built server (package-internal).
	onServer func(*Server)
	// AlertSender overrides the Discord client (tests).
	AlertSender alerts.Sender
	// Modrinth backs the Minecraft plugin/mod manager (nil = public API).
	Modrinth *providers.Modrinth
	FiveM    *providers.FiveM
	Secrets  *secrets.Box
	// Metrics is the running collector (created and started by New when nil).
	Metrics *metrics.Collector
	// Schedules is the schedule store; the engine is created and started by New.
	Schedules *scheduler.Store
	// Backups is the backup record store.
	Backups *backups.Store
	// DBRecords stores per-server MariaDB databases; Databases talks to MariaDB.
	DBRecords *database.Store
	Databases *database.Manager
	// BgCtx outlives requests; background installs run under it.
	BgCtx context.Context
	// Dist is the built frontend. Ignored when DevProxy is set.
	Dist fs.FS
	// DevProxy, when non-empty, forwards non-API requests to a Vite dev server
	// whenever it is reachable, and serves Dist otherwise.
	DevProxy string
}

// Server holds handler state.
type Server struct {
	cfg     *config.Config
	store   *auth.Store
	audit   *audit.Logger
	log     *slog.Logger
	version string
	servers *servers.Store
	sup     supervisor.Supervisor

	templates *templates.Registry
	installer *installer.Manager
	paper     *providers.Paper
	fivem     *providers.FiveM
	secrets   *secrets.Box
	bgCtx     context.Context
	players   *query.Cache
	metrics   *metrics.Collector

	schedules      *scheduler.Store
	scheduleEngine *scheduler.Engine
	backups        *backups.Store
	backupJobs     *backupJobs
	dbRecords      *database.Store
	databases      *database.Manager
	mods           *mods.Manager
	alertStore     *alerts.Store
	alertMon       *alerts.Monitor
	alertSender    alerts.Sender
	sftpInfo       SFTPInfo
	fileJobs       fileJobs

	loginLimitIP   *auth.RateLimiter
	loginLimitUser *auth.RateLimiter
	setupLimit     *auth.RateLimiter

	pendingMu sync.Mutex
	pending   *auth.PendingLogins
}

// New builds the router.
func New(d Deps) (http.Handler, error) {
	s := &Server{
		cfg:            d.Config,
		store:          d.Store,
		audit:          d.Audit,
		log:            d.Log,
		version:        d.Version,
		servers:        d.Servers,
		sup:            d.Sup,
		templates:      d.Templates,
		installer:      d.Installer,
		paper:          d.Paper,
		fivem:          d.FiveM,
		secrets:        d.Secrets,
		bgCtx:          d.BgCtx,
		players:        query.NewCache(3 * time.Second),
		metrics:        d.Metrics,
		schedules:      d.Schedules,
		backups:        d.Backups,
		dbRecords:      d.DBRecords,
		databases:      d.Databases,
		backupJobs:     &backupJobs{jobs: map[string]*backupJob{}},
		pending:        auth.NewPendingLogins(),
		loginLimitIP:   auth.NewRateLimiter(20, 10*time.Minute),
		loginLimitUser: auth.NewRateLimiter(8, 15*time.Minute),
		setupLimit:     auth.NewRateLimiter(10, 15*time.Minute),
	}

	if s.bgCtx == nil {
		s.bgCtx = context.Background()
	}
	if s.templates == nil {
		s.templates, _ = templates.Load(fstest.MapFS{}, "")
	}
	if s.installer == nil {
		s.installer = installer.New(installer.Deps{Log: d.Log})
	}
	s.installer.SetScriptRunner(scriptRunner{s: s})
	if d.Modrinth == nil {
		d.Modrinth = providers.NewModrinth()
	}
	s.mods = mods.NewManager(d.Modrinth)
	s.alertStore = d.Alerts
	s.alertSender = d.AlertSender
	s.sftpInfo = d.SFTPInfo
	if d.ExposeSFTP != nil {
		d.ExposeSFTP(s.SFTPBackend())
	}
	if s.paper == nil {
		s.paper = providers.NewPaper()
	}
	if s.fivem == nil {
		s.fivem = providers.NewFiveM()
	}
	if s.secrets == nil {
		s.secrets, _ = secrets.New(make([]byte, 32))
	}
	if s.servers != nil {
		s.recoverInstalls(s.bgCtx)
	}
	if s.databases == nil {
		s.databases = database.NewManager(database.CredsPath)
	}
	if s.dbRecords != nil {
		go s.repairDatabaseAccounts(s.bgCtx)
	}
	if s.metrics == nil && s.servers != nil && s.sup != nil {
		// 1200 samples at 3 s = one hour of history per server (~60 kB each).
		s.metrics = metrics.New(storeSource{servers: s.servers, sup: s.sup, api: s}, 3*time.Second, 1200, d.Config.DataDir, d.Log)
		go s.metrics.Run(s.bgCtx)
	}
	s.startAlerts()
	if s.servers != nil && s.sup != nil {
		go s.runExpiryLoop(s.bgCtx)
	}
	if s.schedules != nil && s.sup != nil {
		target := scheduler.Target{
			WarnCommand: s.warnCommandFor,
			Players: func(ctx context.Context, id string) int {
				srv, err := s.servers.Get(ctx, id)
				if err != nil {
					return -1
				}
				if p := s.playersOf(ctx, srv); p != nil {
					return p.Online
				}
				return -1
			},
		}
		if s.backups != nil {
			target.Backup = s.scheduledBackup
		}
		target.BeforeStart = s.beforeStart
		target.CanStart = s.servers.CheckStartable
		s.scheduleEngine = scheduler.New(s.schedules, s.sup, target, d.Log)
		if err := s.scheduleEngine.Start(s.bgCtx); err != nil {
			return nil, fmt.Errorf("start scheduler: %w", err)
		}
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(func(next http.Handler) http.Handler { return securityHeadersWith(next, d.DevProxy == "") })
	r.Use(requestLogger(d.Log))

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(noCache, limitBody, rejectCrossSite, s.loadSession)

		api.Get("/version", s.handleVersion)
		api.Get("/setup/status", s.handleSetupStatus)
		api.Get("/branding", s.handleGetBranding)
		api.Post("/setup", s.handleSetup)
		api.Post("/auth/login", s.handleLogin)
		api.Post("/auth/login/totp", s.handleLoginTOTP)

		api.Group(func(priv chi.Router) {
			priv.Use(s.requireAuth, s.enforceTOTP)
			priv.Get("/auth/totp", s.handleTOTPStatus)
			priv.Post("/auth/totp/setup", s.handleTOTPSetup)
			priv.Post("/auth/totp/enable", s.handleTOTPEnable)
			priv.Post("/auth/totp/disable", s.handleTOTPDisable)
			priv.With(s.requireRole(auth.RoleOwner)).Get("/settings/security", s.handleGetSecurity)
			priv.With(s.requireRole(auth.RoleOwner)).Put("/settings/security", s.handleSetSecurity)
			priv.Post("/auth/logout", s.handleLogout)
			priv.Get("/auth/me", s.handleMe)
			priv.Get("/system/info", s.handleSystemInfo)
			priv.Get("/system/stats", s.handleSystemStats)
			priv.Get("/system/firewall", s.handleFirewall)
			priv.Get("/system/cron/preview", s.handleCronPreview)
			priv.With(s.requireRole(auth.RoleAdmin)).Get("/system/database", s.handleDatabaseStatus)
			priv.With(s.requireRole(auth.RoleOwner)).Post("/system/database/install", s.handleDatabaseInstall)
			priv.With(s.requireRole(auth.RoleAdmin)).Put("/settings/branding", s.handleSetBranding)
			priv.With(s.requireRole(auth.RoleOwner)).Get("/system/update", s.handleUpdateInfo)
			priv.With(s.requireRole(auth.RoleOwner)).Post("/system/update", s.handleStartUpdate)
			priv.With(s.requireRole(auth.RoleAdmin)).Post("/system/database/phpmyadmin/restart", s.handleRestartPhpMyAdmin)
			priv.Route("/system/database/root", func(rr chi.Router) {
				rr.Use(s.requireRole(auth.RoleOwner))
				rr.Get("/", s.handleGetRootLogin)
				rr.Put("/", s.handleSetRootLogin)
				rr.Post("/reveal", s.handleRevealRootLogin)
				rr.Delete("/", s.handleDisableRootLogin)
			})
			priv.Get("/events", s.handleEvents)

			priv.With(s.requireRole(auth.RoleAdmin)).Get("/audit", s.handleAuditRecent)
			priv.Post("/auth/password", s.handleChangePassword)
			priv.Get("/auth/api-keys", s.handleListAPIKeys)
			priv.Post("/auth/api-keys", s.handleCreateAPIKey)
			priv.Delete("/auth/api-keys/{kid}", s.handleDeleteAPIKey)

			priv.Route("/users", func(ur chi.Router) {
				ur.Use(s.requireRole(auth.RoleAdmin))
				ur.Get("/", s.handleListUsers)
				ur.Post("/", s.handleCreateUser)
				ur.Patch("/{uid}", s.handleUpdateUser)
				ur.Delete("/{uid}", s.handleDeleteUser)
			})

			priv.Route("/templates", func(tr chi.Router) {
				tr.Use(s.requireRole(auth.RoleAdmin))
				tr.Get("/", s.handleListTemplates)
				tr.Post("/", s.handleCreateTemplate)
				tr.Post("/validate", s.handleValidateTemplate)
				tr.Post("/import", s.handleImportTemplate)
				tr.Get("/{id}/yaml", s.handleTemplateYAML)
				tr.Put("/{id}", s.handleUpdateTemplate)
				tr.Delete("/{id}", s.handleDeleteTemplate)
				tr.Get("/{id}/sources/{source}", s.handleTemplateSource)
			})

			priv.Get("/mods/game-versions", s.handleGameVersions)
			priv.Get("/system/sftp", s.handleSFTPInfo)

			priv.Route("/alerts", func(ar chi.Router) {
				ar.Use(s.requireRole(auth.RoleAdmin))
				ar.Get("/", s.handleGetAlerts)
				ar.Put("/", s.handlePutAlerts)
				ar.Post("/test", s.handleTestAlerts)
			})

			priv.With(s.requireRole(auth.RoleAdmin)).Post("/ports/check", s.handlePortsCheck)
			priv.Get("/servers", s.handleListServers)
			priv.With(s.requireRole(auth.RoleAdmin)).Post("/servers", s.handleCreateServer)
			priv.Route("/servers/{id}", func(sr chi.Router) {
				sr.With(s.withServer(auth.PermConsoleRead)).Get("/", s.handleGetServer)
				sr.With(s.withServer(auth.PermSettings)).Patch("/", s.handleUpdateServer)
				sr.With(s.withServer(auth.PermDelete)).Delete("/", s.handleDeleteServer)
				sr.With(s.withServer(auth.PermPower)).Post("/power", s.handlePower)
				sr.With(s.withServer(auth.PermConsoleRead)).Get("/console", s.handleConsole)
				sr.With(s.withServer(auth.PermConsoleRead)).Get("/console/hints", s.handleConsoleHints)
				sr.With(s.withServer(auth.PermConsoleRead)).Get("/stats", s.handleServerStats)
				sr.With(s.withServer(auth.PermConsoleRead)).Get("/events", s.handleServerEvents)
				sr.With(s.withServer(auth.PermConsoleRead)).Get("/install", s.handleInstallStatus)
				sr.With(s.withServer(auth.PermSettings)).Post("/install", s.handleReinstall)
				sr.With(s.withServer(auth.PermSettings)).Post("/git-update", s.handleGitUpdate)
				sr.With(s.requireRole(auth.RoleAdmin), s.withServer(auth.PermSettings)).Put("/expiry", s.handleSetExpiry)

				sr.Route("/users", func(sur chi.Router) {
					sur.Use(s.requireRole(auth.RoleAdmin), s.withServer(auth.PermSettings))
					sur.Get("/", s.handleListServerUsers)
					sur.Put("/{uid}", s.handleSetServerUser)
				})

				if s.dbRecords != nil {
					sr.Route("/databases", func(dr chi.Router) {
						dr.Use(s.withServer(auth.PermSettings))
						dr.Get("/", s.handleListServerDatabases)
						dr.Post("/", s.handleCreateServerDatabase)
						dr.Get("/{dbid}/credentials", s.handleDatabaseCredentials)
						dr.Post("/{dbid}/password", s.handleRotateDatabasePassword)
						dr.Delete("/{dbid}", s.handleDeleteServerDatabase)
					})
				}

				sr.Route("/schedules", func(scr chi.Router) {
					scr.Use(s.withServer(auth.PermSchedules))
					scr.Get("/", s.handleListSchedules)
					scr.Post("/", s.handleCreateSchedule)
					scr.Patch("/{sid}", s.handleUpdateSchedule)
					scr.Delete("/{sid}", s.handleDeleteSchedule)
					scr.Post("/{sid}/run", s.handleRunSchedule)
					scr.Post("/{sid}/cancel", s.handleCancelSchedule)
					scr.Get("/{sid}/runs", s.handleScheduleRuns)
				})

				if s.backups != nil {
					sr.Route("/backups", func(br chi.Router) {
						br.Use(s.withServer(auth.PermBackups))
						br.Get("/", s.handleListBackups)
						br.Post("/", s.handleCreateBackup)
						br.Put("/settings", s.handleBackupSettings)
						br.Delete("/{bid}", s.handleDeleteBackup)
						br.Get("/{bid}/download", s.handleDownloadBackup)
						br.Post("/{bid}/restore", s.handleRestoreBackup)
					})
				}

				sr.With(s.withServer(auth.PermSettings)).Get("/alerts", s.handleGetServerAlerts)
				sr.With(s.withServer(auth.PermSettings)).Put("/alerts", s.handlePutServerAlerts)
				sr.With(s.withServer(auth.PermSettings)).Post("/alerts/test", s.handleTestServerAlerts)

				sr.Route("/mods", func(mr chi.Router) {
					mr.With(s.withServer(auth.PermFilesRead)).Get("/", s.handleModsList)
					mr.With(s.withServer(auth.PermFilesRead)).Get("/updates", s.handleModsUpdates)
					mr.With(s.withServer(auth.PermFilesRead)).Get("/search", s.handleModsSearch)
					mr.With(s.withServer(auth.PermFilesRead)).Get("/projects/{pid}/versions", s.handleModVersions)
					mr.With(s.withServer(auth.PermFilesWrite)).Post("/", s.handleModInstall)
					mr.With(s.withServer(auth.PermFilesWrite)).Put("/platform", s.handleModPlatform)
					mr.With(s.withServer(auth.PermFilesWrite)).Post("/files/{file}/update", s.handleModUpdate)
					mr.With(s.withServer(auth.PermFilesWrite)).Patch("/files/{file}", s.handleModToggle)
					mr.With(s.withServer(auth.PermFilesWrite)).Delete("/files/{file}", s.handleModDelete)
				})

				sr.Route("/files", func(fr chi.Router) {
					fr.With(s.withServer(auth.PermFilesRead)).Get("/", s.handleFilesList)
					fr.With(s.withServer(auth.PermFilesRead)).Get("/content", s.handleFilesRead)
					fr.With(s.withServer(auth.PermFilesRead)).Get("/download", s.handleFilesDownload)
					fr.With(s.withServer(auth.PermFilesWrite)).Put("/content", s.handleFilesWrite)
					fr.With(s.withServer(auth.PermFilesWrite)).Post("/mkdir", s.handleFilesMkdir)
					fr.With(s.withServer(auth.PermFilesWrite)).Post("/rename", s.handleFilesRename)
					fr.With(s.withServer(auth.PermFilesWrite)).Post("/delete", s.handleFilesDelete)
					fr.With(s.withServer(auth.PermFilesWrite)).Post("/extract", s.handleFilesExtract)
					fr.With(s.withServer(auth.PermFilesWrite)).Post("/upload", s.handleFilesUpload)
					fr.With(s.withServer(auth.PermFilesWrite)).Post("/fetch", s.handleFilesFetch)
					fr.With(s.withServer(auth.PermFilesRead)).Get("/jobs", s.handleFileJobs)
					fr.With(s.withServer(auth.PermFilesWrite)).Delete("/jobs/{jid}", s.handleCancelFileJob)
				})
			})
		})

		api.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, CodeNotFound, "Unknown API endpoint.")
		})
		api.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusMethodNotAllowed, CodeBadRequest, "Method not allowed.")
		})
	})

	static := spaHandler(d.Dist)
	if d.DevProxy != "" {
		var err error
		static, err = devHandler(d.DevProxy, static)
		if err != nil {
			return nil, fmt.Errorf("dev proxy: %w", err)
		}
	}
	r.Handle("/*", static)
	if d.onServer != nil {
		d.onServer(s)
	}
	return r, nil
}

// internalError logs the cause and returns a generic 500 without leaking details.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err, "reqId", middleware.GetReqID(r.Context()))
	writeError(w, http.StatusInternalServerError, CodeInternal, "Something went wrong on the server. Check the panel logs.")
}

// Package api wires HTTP routes, middleware and the embedded frontend.
package api

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/cypek019/cypek.dev---panel/internal/audit"
	"github.com/cypek019/cypek.dev---panel/internal/auth"
	"github.com/cypek019/cypek.dev---panel/internal/config"
)

// Deps are the collaborators the API needs.
type Deps struct {
	Config  *config.Config
	Store   *auth.Store
	Audit   *audit.Logger
	Log     *slog.Logger
	Version string
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

	loginLimitIP   *auth.RateLimiter
	loginLimitUser *auth.RateLimiter
	setupLimit     *auth.RateLimiter
}

// New builds the router.
func New(d Deps) (http.Handler, error) {
	s := &Server{
		cfg:            d.Config,
		store:          d.Store,
		audit:          d.Audit,
		log:            d.Log,
		version:        d.Version,
		loginLimitIP:   auth.NewRateLimiter(20, 10*time.Minute),
		loginLimitUser: auth.NewRateLimiter(8, 15*time.Minute),
		setupLimit:     auth.NewRateLimiter(10, 15*time.Minute),
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(requestLogger(d.Log))

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(noCache, limitBody, rejectCrossSite, s.loadSession)

		api.Get("/version", s.handleVersion)
		api.Get("/setup/status", s.handleSetupStatus)
		api.Post("/setup", s.handleSetup)
		api.Post("/auth/login", s.handleLogin)

		api.Group(func(priv chi.Router) {
			priv.Use(s.requireAuth)
			priv.Post("/auth/logout", s.handleLogout)
			priv.Get("/auth/me", s.handleMe)
			priv.Get("/system/info", s.handleSystemInfo)

			priv.With(s.requireRole(auth.RoleAdmin)).Get("/audit", s.handleAuditRecent)
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
	return r, nil
}

// internalError logs the cause and returns a generic 500 without leaking details.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err, "reqId", middleware.GetReqID(r.Context()))
	writeError(w, http.StatusInternalServerError, CodeInternal, "Something went wrong on the server. Check the panel logs.")
}

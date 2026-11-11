package api

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

const (
	sessionCookie = "homepanel_session"
	csrfHeader    = "X-CSRF-Token"

	maxJSONBody = 1 << 20
)

type ctxKey int

const (
	ctxSession ctxKey = iota
	ctxUser
)

func sessionFromContext(ctx context.Context) (*auth.Session, *auth.User) {
	sess, _ := ctx.Value(ctxSession).(*auth.Session)
	user, _ := ctx.Value(ctxUser).(*auth.User)
	return sess, user
}

func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			key, user, err := s.store.LookupAPIKey(r.Context(), strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
			if err != nil {
				writeError(w, http.StatusUnauthorized, CodeUnauthorized, "Invalid or expired API key.")
				return
			}
			ctx := context.WithValue(r.Context(), ctxSession, &auth.Session{UserID: user.ID, APIKey: true})
			ctx = context.WithValue(ctx, ctxUser, user)
			ctx = context.WithValue(ctx, ctxAPIKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		c, err := r.Cookie(s.cookieName(r))
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		sess, user, err := s.store.LookupSession(r.Context(), c.Value)
		if err != nil {
			s.clearSessionCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}

		if ip := clientIP(r); sess.IP != "" && !sameClient(sess.IP, ip) {
			_ = s.store.DeleteSession(r.Context(), sess.ID)
			s.clearSessionCookie(w, r)
			s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "auth.session_ip_mismatch", IP: ip, Details: map[string]any{"sessionIp": sess.IP}})
			next.ServeHTTP(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxSession, sess)
		ctx = context.WithValue(ctx, ctxUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := sessionFromContext(r.Context())
		if sess == nil {
			writeError(w, http.StatusUnauthorized, CodeUnauthorized, "You must be logged in.")
			return
		}
		if !sess.APIKey && !isSafeMethod(r.Method) && subtle.ConstantTimeCompare([]byte(r.Header.Get(csrfHeader)), []byte(sess.CSRFToken)) != 1 {
			writeError(w, http.StatusForbidden, CodeCSRF, "CSRF token missing or invalid. Reload the page and try again.")
			return
		}

		if sess.APIKey && strings.HasPrefix(r.URL.Path, "/api/v1/auth/") && r.URL.Path != "/api/v1/auth/me" {
			writeError(w, http.StatusForbidden, CodeForbidden, "API keys cannot manage the account.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireRole(min auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, user := sessionFromContext(r.Context())
			if user == nil || !user.Role.AtLeast(min) {
				writeError(w, http.StatusForbidden, CodeForbidden, "You do not have permission to do this.")
				return
			}

			if key := apiKeyFromContext(r); key != nil && !key.AllowsPermission("*") {
				writeError(w, http.StatusForbidden, CodeForbidden, "This API key is limited to server permissions.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func rejectCrossSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSafeMethod(r.Method) {
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
				writeError(w, http.StatusForbidden, CodeCSRF, "Cross-site requests are not allowed.")
				return
			}

			if ct := r.Header.Get("Content-Type"); r.ContentLength != 0 && !strings.HasPrefix(ct, "application/json") && !isFilesRoute(r) {
				writeError(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Content-Type must be application/json.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

var filesRoute = regexp.MustCompile(`^/api/v1/servers/[a-z0-9]+/files(/|$)`)

func isFilesRoute(r *http.Request) bool { return filesRoute.MatchString(r.URL.EscapedPath()) }

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isFilesRoute(r) {
			r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		}
		next.ServeHTTP(w, r)
	})
}

func contentSecurityPolicy(r *http.Request) string {
	ws := "ws://" + r.Host
	if r.TLS != nil {
		ws = "wss://" + r.Host
	}
	return "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://cdn.modrinth.com; " +
		"font-src 'self' data:; connect-src 'self' " + ws + "; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"
}

func securityHeaders(next http.Handler) http.Handler {
	return securityHeadersWith(next, true)
}

func securityHeadersWith(next http.Handler, csp bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if csp {
			h.Set("Content-Security-Policy", contentSecurityPolicy(r))
		}
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			level := slog.LevelDebug
			if ww.Status() >= 500 {
				level = slog.LevelError
			} else if ww.Status() >= 400 {
				level = slog.LevelInfo
			}
			log.Log(r.Context(), level, "http",
				"method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"bytes", ww.BytesWritten(), "dur", time.Since(start).Round(time.Millisecond),
				"ip", clientIP(r), "reqId", middleware.GetReqID(r.Context()))
		})
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func (s *Server) cookieName(r *http.Request) string {
	if r.TLS != nil || s.cfg.TLS.Enabled {
		return "__Host-" + sessionCookie
	}
	return sessionCookie
}

func sameClient(a, b string) bool {
	if a == b {
		return true
	}
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	if ia == nil || ib == nil || ia.To4() != nil || ib.To4() != nil {
		return false
	}
	m := net.CIDRMask(64, 128)
	return ia.Mask(m).Equal(ib.Mask(m))
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(r),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || s.cfg.TLS.Enabled,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(auth.SessionTTL.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(r),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || s.cfg.TLS.Enabled,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

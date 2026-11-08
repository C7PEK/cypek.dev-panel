package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/cypek019/cypek.dev---panel/internal/auth"
)

const (
	sessionCookie = "homepanel_session"
	csrfHeader    = "X-CSRF-Token"
	// maxJSONBody bounds API request bodies; file uploads get their own limit later.
	maxJSONBody = 1 << 20
)

type ctxKey int

const (
	ctxSession ctxKey = iota
	ctxUser
)

// sessionFromContext returns the session and user attached by loadSession, or nil.
func sessionFromContext(ctx context.Context) (*auth.Session, *auth.User) {
	sess, _ := ctx.Value(ctxSession).(*auth.Session)
	user, _ := ctx.Value(ctxUser).(*auth.User)
	return sess, user
}

// loadSession attaches the session and user for a valid cookie; it never rejects.
func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
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
		ctx := context.WithValue(r.Context(), ctxSession, sess)
		ctx = context.WithValue(ctx, ctxUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAuth rejects unauthenticated requests and enforces CSRF on
// state-changing methods. Every protected route must sit behind it.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := sessionFromContext(r.Context())
		if sess == nil {
			writeError(w, http.StatusUnauthorized, CodeUnauthorized, "You must be logged in.")
			return
		}
		if !isSafeMethod(r.Method) && r.Header.Get(csrfHeader) != sess.CSRFToken {
			writeError(w, http.StatusForbidden, CodeCSRF, "CSRF token missing or invalid. Reload the page and try again.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireRole additionally demands a minimum global role.
func (s *Server) requireRole(min auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, user := sessionFromContext(r.Context())
			if user == nil || !user.Role.AtLeast(min) {
				writeError(w, http.StatusForbidden, CodeForbidden, "You do not have permission to do this.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// rejectCrossSite blocks state-changing API calls that a browser marks as
// cross-site. Together with SameSite=Strict cookies and the JSON content type
// this also covers login/setup, which have no session to carry a CSRF token.
func rejectCrossSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSafeMethod(r.Method) {
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
				writeError(w, http.StatusForbidden, CodeCSRF, "Cross-site requests are not allowed.")
				return
			}
			// File routes take text and multipart bodies; they are protected by
			// the CSRF token requireAuth checks on every unsafe request.
			if ct := r.Header.Get("Content-Type"); r.ContentLength != 0 && !strings.HasPrefix(ct, "application/json") && !strings.Contains(r.URL.Path, "/files") {
				writeError(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Content-Type must be application/json.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// File manager routes carry uploads and editor content; they set
		// their own, larger limits.
		if !strings.Contains(r.URL.Path, "/files") {
			r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
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

// noCache marks API responses as non-cacheable.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// requestLogger logs one line per request via slog.
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

// clientIP returns the TCP peer address. X-Forwarded-For is deliberately
// ignored until a trusted-proxy setting exists; a LAN client could spoof it.
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

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
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
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || s.cfg.TLS.Enabled,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

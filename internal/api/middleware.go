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

// loadSession attaches the session and user for a valid cookie or a bearer
// API key; it never rejects. API-key requests get a synthetic session (no
// CSRF: there is no cookie for a cross-site page to ride on) and the key is
// kept in the context so permission checks can apply its scope.
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
		// Cookies are not port-scoped: any other service on this host (a
		// bot's web page, a game's HTTP endpoint) receives the panel cookie
		// when the user opens it. A session therefore only works from the
		// address it was created from.
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

// requireAuth rejects unauthenticated requests and enforces CSRF on
// state-changing methods. Every protected route must sit behind it.
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
		// API keys automate servers; they never manage the account itself
		// (password, 2FA, more keys).
		if sess.APIKey && strings.HasPrefix(r.URL.Path, "/api/v1/auth/") && r.URL.Path != "/api/v1/auth/me" {
			writeError(w, http.StatusForbidden, CodeForbidden, "API keys cannot manage the account.")
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
			// A scoped key (e.g. console.read for a status script) must not
			// act with its owner's full admin role.
			if key := apiKeyFromContext(r); key != nil && !key.AllowsPermission("*") {
				writeError(w, http.StatusForbidden, CodeForbidden, "This API key is limited to server permissions.")
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
			if ct := r.Header.Get("Content-Type"); r.ContentLength != 0 && !strings.HasPrefix(ct, "application/json") && !isFilesRoute(r) {
				writeError(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Content-Type must be application/json.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// filesRoute matches the file manager routes on the escaped path, so an
// encoded "%2Ffiles" inside another route's parameter does not count.
var filesRoute = regexp.MustCompile(`^/api/v1/servers/[a-z0-9]+/files(/|$)`)

func isFilesRoute(r *http.Request) bool { return filesRoute.MatchString(r.URL.EscapedPath()) }

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// File manager routes carry uploads and editor content; they set
		// their own, larger limits.
		if !isFilesRoute(r) {
			r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		}
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy: only the panel's own scripts, no inline scripts,
// images from the panel and Modrinth's CDN, API/WebSockets only to the
// panel itself, never framed.
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

// cookieName adds the __Host- prefix over HTTPS: the browser then only
// accepts the cookie as Secure, host-only and for path "/".
func (s *Server) cookieName(r *http.Request) string {
	if r.TLS != nil || s.cfg.TLS.Enabled {
		return "__Host-" + sessionCookie
	}
	return sessionCookie
}

// sameClient compares IPv4 addresses exactly and IPv6 by /64, because
// IPv6 privacy addresses rotate within the same network.
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

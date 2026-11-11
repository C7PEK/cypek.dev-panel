package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

type sessionResponse struct {
	User      *auth.User `json:"user"`
	CSRFToken string     `json:"csrfToken"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if ok, retry := s.loginLimitIP.Allow(ip); !ok {
		rateLimited(w, retry)
		return
	}
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, CodeValidation, "Username and password are required.")
		return
	}

	userKey := ip + "|" + strings.ToLower(req.Username)
	if ok, retry := s.loginLimitUser.Allow(userKey); !ok {
		rateLimited(w, retry)
		return
	}
	user, err := s.store.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		s.audit.Log(r.Context(), audit.Entry{Username: req.Username, Action: "auth.login_failed", IP: ip})
		if !writeAuthError(w, err) {
			s.internalError(w, r, err)
		}
		return
	}
	if !user.TOTPEnabled {
		s.loginLimitIP.Reset(ip)
		s.loginLimitUser.Reset(userKey)
	}

	if user.TOTPEnabled {
		s.pendingMu.Lock()
		token, err := s.pending.Add(user.ID)
		s.pendingMu.Unlock()
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"totpRequired": true, "token": token})
		return
	}
	s.startSession(w, r, user, "auth.login")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sess, user := sessionFromContext(r.Context())
	if sess.APIKey {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.store.DeleteSession(r.Context(), sess.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.clearSessionCookie(w, r)
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "auth.logout", IP: clientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess, user := sessionFromContext(r.Context())
	writeJSON(w, http.StatusOK, sessionResponse{User: user, CSRFToken: sess.CSRFToken})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user *auth.User, action string) {
	token, sess, err := s.store.CreateSession(r.Context(), user.ID, clientIP(r), r.UserAgent())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	_ = s.store.TouchLogin(r.Context(), user.ID)
	s.setSessionCookie(w, r, token)
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: action, IP: clientIP(r)})
	writeJSON(w, http.StatusOK, sessionResponse{User: user, CSRFToken: sess.CSRFToken})
}

func rateLimited(w http.ResponseWriter, retry time.Duration) {
	secs := int(retry.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusTooManyRequests, CodeRateLimited, "Too many login attempts. Try again later.")
}

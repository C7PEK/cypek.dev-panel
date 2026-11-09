package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

const ctxAPIKey ctxKey = 20

func apiKeyFromContext(r *http.Request) *auth.APIKey {
	k, _ := r.Context().Value(ctxAPIKey).(*auth.APIKey)
	return k
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	keys, err := s.store.ListAPIKeys(r.Context(), user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys, "permissions": auth.AllPermissions})
}

type createAPIKeyRequest struct {
	Name      string   `json:"name"`
	Scope     []string `json:"scope"`
	ExpiresIn int      `json:"expiresInDays"`
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	if apiKeyFromContext(r) != nil {
		writeError(w, http.StatusForbidden, CodeForbidden, "API keys cannot create other API keys.")
		return
	}
	var req createAPIKeyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var expires *time.Time
	if req.ExpiresIn < 0 || req.ExpiresIn > 3650 {
		writeFieldError(w, CodeValidation, "expiresInDays", "Expiry must be 0 (never) to 3650 days.")
		return
	}
	if req.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresIn) * 24 * time.Hour)
		expires = &t
	}
	plaintext, key, err := s.store.CreateAPIKey(r.Context(), user.ID, req.Name, req.Scope, expires)
	if err != nil {
		writeFieldError(w, CodeValidation, "name", err.Error())
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "apikey.create", Target: key.Name, IP: clientIP(r), Details: map[string]any{"prefix": key.Prefix, "scope": key.Scope}})
	writeJSON(w, http.StatusCreated, map[string]any{"key": key, "token": plaintext})
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "kid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "Key not found.")
		return
	}
	if err := s.store.DeleteAPIKey(r.Context(), user.ID, id); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			writeError(w, http.StatusNotFound, CodeNotFound, "Key not found.")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "apikey.delete", Target: strconv.FormatInt(id, 10), IP: clientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

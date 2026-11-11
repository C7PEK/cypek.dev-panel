package api

import (
	"net/http"
	"strings"
)

type setupStatusResponse struct {
	SetupRequired bool `json:"setupRequired"`
}

type setupRequest struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	required, err := s.store.SetupRequired(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{SetupRequired: required})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if ok, retry := s.setupLimit.Allow(ip); !ok {
		rateLimited(w, retry)
		return
	}
	var req setupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	req.Username = strings.TrimSpace(req.Username)
	if req.Token == "" {
		writeFieldError(w, CodeValidation, "token", "Setup token is required.")
		return
	}
	user, err := s.store.CompleteSetup(r.Context(), s.cfg.DataDir, req.Token, req.Username, req.Password)
	if err != nil {
		if !writeAuthError(w, err) {
			s.internalError(w, r, err)
		}
		return
	}
	s.setupLimit.Reset(ip)
	s.log.Info("setup completed, owner account created", "username", user.Username, "ip", ip)
	s.startSession(w, r, user, "auth.setup")
}

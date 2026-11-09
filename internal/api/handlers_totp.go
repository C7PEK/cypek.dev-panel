package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"strings"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

const (
	CodeTOTPRequired = "totp_required" // login needs a second step / policy demands enrolment
	CodeTOTPInvalid  = "totp_invalid"
	CodeTOTPState    = "totp_state"
)

// Security policy stored in settings: who must have 2FA enabled.
const settingRequireTOTP = "security.require_totp" // none | admins | all

func (s *Server) requireTOTPPolicy(ctx context.Context) string {
	v, _ := s.store.GetSetting(ctx, settingRequireTOTP)
	if v == "" {
		return "none"
	}
	return v
}

// totpPolicyApplies reports whether the policy demands 2FA from this user.
func totpPolicyApplies(policy string, u *auth.User) bool {
	switch policy {
	case "all":
		return true
	case "admins":
		return u.Role.AtLeast(auth.RoleAdmin)
	}
	return false
}

// enforceTOTP blocks users the policy covers until they enrol. Only the
// endpoints needed to enrol (and to leave) stay reachable.
func (s *Server) enforceTOTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, user := sessionFromContext(r.Context())
		if user != nil && !user.TOTPEnabled && totpPolicyApplies(s.requireTOTPPolicy(r.Context()), user) {
			p := r.URL.Path
			if !strings.HasPrefix(p, "/api/v1/auth/totp") && p != "/api/v1/auth/me" && p != "/api/v1/auth/logout" {
				writeError(w, http.StatusForbidden, CodeTOTPRequired, "Two-factor authentication is required for your account. Enable it in your profile.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type totpSetupResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauthUrl"`
	QRPNG      string `json:"qrPng"` // base64 PNG
}

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	key, err := s.store.BeginTOTP(r.Context(), user, s.secrets)
	if errors.Is(err, auth.ErrTOTPEnabled) {
		writeError(w, http.StatusConflict, CodeTOTPState, "Two-factor authentication is already enabled.")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	img, err := key.Image(240, 240)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, totpSetupResponse{Secret: key.Secret(), OTPAuthURL: key.URL(), QRPNG: base64.StdEncoding.EncodeToString(buf.Bytes())})
}

type totpCodeRequest struct {
	Code string `json:"code"`
}

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	var req totpCodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	codes, err := s.store.ConfirmTOTP(r.Context(), user, s.secrets, req.Code)
	switch {
	case errors.Is(err, auth.ErrTOTPInvalid):
		writeFieldError(w, CodeTOTPInvalid, "code", "That code is not valid. Check the time on your phone and try again.")
		return
	case errors.Is(err, auth.ErrTOTPEnabled), errors.Is(err, auth.ErrTOTPNotPending):
		writeError(w, http.StatusConflict, CodeTOTPState, err.Error())
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "auth.totp_enabled", Target: user.Username, IP: clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"backupCodes": codes})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	var req totpCodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	err := s.store.DisableTOTP(r.Context(), user, s.secrets, req.Code)
	switch {
	case errors.Is(err, auth.ErrTOTPInvalid):
		writeFieldError(w, CodeTOTPInvalid, "code", "That code is not valid.")
		return
	case errors.Is(err, auth.ErrTOTPDisabled):
		writeError(w, http.StatusConflict, CodeTOTPState, "Two-factor authentication is not enabled.")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "auth.totp_disabled", Target: user.Username, IP: clientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTOTPStatus(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":         user.TOTPEnabled,
		"backupCodesLeft": s.store.BackupCodesLeft(r.Context(), user.ID),
		"required":        totpPolicyApplies(s.requireTOTPPolicy(r.Context()), user),
		"policy":          s.requireTOTPPolicy(r.Context()),
	})
}

type totpLoginRequest struct {
	Token string `json:"token"`
	Code  string `json:"code"`
}

// handleLoginTOTP finishes a login that returned totpRequired.
func (s *Server) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	if ok, retry := s.loginLimitIP.Allow(clientIP(r)); !ok {
		rateLimited(w, retry)
		return
	}
	var req totpLoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	s.pendingMu.Lock()
	userID, ok := s.pending.Take(req.Token, false)
	s.pendingMu.Unlock()
	if !ok {
		writeError(w, http.StatusUnauthorized, CodeUnauthorized, "This login attempt has expired. Start again.")
		return
	}
	totpKey := fmt.Sprintf("totp|%d", userID)
	if ok, retry := s.loginLimitUser.Allow(totpKey); !ok {
		rateLimited(w, retry)
		return
	}
	valid, err := s.store.VerifyTOTP(r.Context(), userID, s.secrets, req.Code)
	if err != nil && !errors.Is(err, auth.ErrTOTPDisabled) {
		s.internalError(w, r, err)
		return
	}
	if !valid {
		s.audit.Log(r.Context(), audit.Entry{UserID: &userID, Action: "auth.totp_failed", IP: clientIP(r)})
		writeFieldError(w, CodeTOTPInvalid, "code", "That code is not valid.")
		return
	}
	s.pendingMu.Lock()
	s.pending.Take(req.Token, true)
	s.pendingMu.Unlock()
	s.loginLimitIP.Reset(clientIP(r))
	s.loginLimitUser.Reset(totpKey)
	user, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil || user.Disabled {
		writeError(w, http.StatusUnauthorized, CodeUnauthorized, "Login failed.")
		return
	}
	s.loginLimitIP.Reset(clientIP(r))
	s.startSession(w, r, user, "auth.login")
}

type securitySettingsRequest struct {
	RequireTOTP string `json:"requireTotp"`
}

func (s *Server) handleGetSecurity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"requireTotp": s.requireTOTPPolicy(r.Context())})
}

func (s *Server) handleSetSecurity(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	var req securitySettingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.RequireTOTP {
	case "none", "admins", "all":
	default:
		writeFieldError(w, CodeValidation, "requireTotp", "Must be none, admins or all.")
		return
	}
	if req.RequireTOTP != "none" && !user.TOTPEnabled {
		writeError(w, http.StatusConflict, CodeTOTPState, "Enable two-factor authentication on your own account first.")
		return
	}
	if err := s.store.SetSetting(r.Context(), settingRequireTOTP, req.RequireTOTP); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "security.require_totp", Target: req.RequireTOTP, IP: clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"requireTotp": req.RequireTOTP})
}

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cypek019/cypek.dev---panel/internal/auth"
)

// Error codes are stable identifiers the frontend maps to translated messages.
const (
	CodeBadRequest         = "bad_request"
	CodeValidation         = "validation_failed"
	CodeUnauthorized       = "unauthorized"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
	CodeRateLimited        = "rate_limited"
	CodeInternal           = "internal_error"
	CodeInvalidCredentials = "invalid_credentials"
	CodeAccountDisabled    = "account_disabled"
	CodeSetupRequired      = "setup_required"
	CodeSetupCompleted     = "setup_completed"
	CodeInvalidSetupToken  = "invalid_setup_token"
	CodeUsernameTaken      = "username_taken"
	CodeInvalidUsername    = "invalid_username"
	CodeWeakPassword       = "weak_password"
	CodeCSRF               = "csrf_failed"
	CodePayloadTooLarge    = "payload_too_large"
	CodeFrontendMissing    = "frontend_not_built"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Field names the offending input when the error is a validation error.
	Field string `json:"field,omitempty"`
	// Params fill the translated message (e.g. port, server name).
	Params map[string]any `json:"params,omitempty"`
}

// writeError renders the shared error envelope.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func writeFieldError(w http.ResponseWriter, code, field, message string) {
	writeJSON(w, http.StatusUnprocessableEntity, errorBody{Error: errorDetail{Code: code, Message: message, Field: field}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

// writeAuthError maps auth package errors to API responses; returns false if
// err was not one of them (caller should treat it as internal).
func writeAuthError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, CodeInvalidCredentials, "Invalid username or password.")
	case errors.Is(err, auth.ErrAccountDisabled):
		writeError(w, http.StatusForbidden, CodeAccountDisabled, "This account is disabled.")
	case errors.Is(err, auth.ErrSetupNotRequired):
		writeError(w, http.StatusConflict, CodeSetupCompleted, "Setup has already been completed.")
	case errors.Is(err, auth.ErrSetupTokenInvalid):
		writeError(w, http.StatusForbidden, CodeInvalidSetupToken, "The setup token is invalid.")
	case errors.Is(err, auth.ErrUsernameTaken):
		writeFieldError(w, CodeUsernameTaken, "username", "This username is already taken.")
	case errors.Is(err, auth.ErrInvalidUsername):
		writeFieldError(w, CodeInvalidUsername, "username", "Username must be 2–32 characters: letters, digits, dot, underscore or dash.")
	case errors.Is(err, auth.ErrPasswordTooShort):
		writeFieldError(w, CodeWeakPassword, "password", "Password must be at least 8 characters.")
	case errors.Is(err, auth.ErrPasswordTooLong):
		writeFieldError(w, CodeWeakPassword, "password", "Password is too long.")
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeNotFound, "Not found.")
	default:
		return false
	}
	return true
}

// decodeJSON reads a JSON body into v, rejecting unknown fields and oversize payloads.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "Request body is too large.")
			return false
		}
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Malformed JSON body: "+err.Error())
		return false
	}
	return true
}

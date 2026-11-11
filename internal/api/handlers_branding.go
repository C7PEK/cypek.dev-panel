package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
)

const brandingKey = "branding"

type branding struct {
	Name   string `json:"name"`
	Accent string `json:"accent"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (b *branding) validate() (field, msg string) {
	b.Name = strings.TrimSpace(b.Name)
	if utf8.RuneCountInString(b.Name) > 40 {
		return "name", "The name can have at most 40 characters."
	}
	for _, r := range b.Name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "name", "The name cannot contain control characters."
		}
	}
	b.Accent = strings.ToLower(strings.TrimSpace(b.Accent))
	if b.Accent != "" && !hexColor.MatchString(b.Accent) {
		return "accent", "The colour must look like #5865f2."
	}
	return "", ""
}

func (s *Server) loadBranding(r *http.Request) branding {
	var b branding
	if raw, err := s.store.GetSetting(r.Context(), brandingKey); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &b)
	}
	return b
}

func (s *Server) handleGetBranding(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.loadBranding(r))
}

func (s *Server) handleSetBranding(w http.ResponseWriter, r *http.Request) {
	var b branding
	if !decodeJSON(w, r, &b) {
		return
	}
	if field, msg := b.validate(); field != "" {
		writeFieldError(w, CodeValidation, field, msg)
		return
	}
	raw, _ := json.Marshal(b)
	if err := s.store.SetSetting(r.Context(), brandingKey, string(raw)); err != nil {
		s.internalError(w, r, err)
		return
	}
	_, user := sessionFromContext(r.Context())
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "settings.branding", Target: "panel", IP: clientIP(r),
		Details: map[string]any{"name": b.Name, "accent": b.Accent}})
	writeJSON(w, http.StatusOK, b)
}

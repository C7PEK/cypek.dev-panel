package api

import (
	"net/http"
	"testing"

	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

func TestBranding(t *testing.T) {
	e := newEnv(t)
	resp, body := e.do(http.MethodGet, "/api/v1/branding", nil)
	if resp.StatusCode != 200 || body["name"] != "" || body["accent"] != "" {
		t.Fatalf("default (public): %d %v", resp.StatusCode, body)
	}
	e.completeSetup("owner", "password123")
	for field, in := range map[string]map[string]string{
		"accent": {"name": "x", "accent": "red; background:url(x)"},
		"name":   {"name": "a\x07b", "accent": ""},
	} {
		resp, body = e.do(http.MethodPut, "/api/v1/settings/branding", in)
		if resp.StatusCode != 422 || body["error"].(map[string]any)["field"] != field {
			t.Errorf("%s: %d %v", field, resp.StatusCode, body)
		}
	}
	resp, body = e.do(http.MethodPut, "/api/v1/settings/branding", map[string]string{"name": "  Mój Homelab  ", "accent": "#22C55E"})
	if resp.StatusCode != 200 || body["name"] != "Mój Homelab" || body["accent"] != "#22c55e" {
		t.Fatalf("save: %d %v", resp.StatusCode, body)
	}
	_, body = e.do(http.MethodGet, "/api/v1/branding", nil)
	if body["accent"] != "#22c55e" {
		t.Fatalf("read back: %v", body)
	}
	plain, _ := e.store.CreateUser(t.Context(), "plain", "password123", auth.RoleUser)
	_ = plain
	e.loginAs("plain")
	resp, _ = e.do(http.MethodPut, "/api/v1/settings/branding", map[string]string{"name": "hacked"})
	if resp.StatusCode != 403 {
		t.Fatalf("user may not change branding: %d", resp.StatusCode)
	}
}

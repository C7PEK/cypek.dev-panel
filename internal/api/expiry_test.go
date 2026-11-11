package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

func TestServerExpiry(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	resp, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "Kolega", "expiresInDays": 0, "run": map[string]any{"command": "/bin/sleep", "args": []string{"600"}}})
	if resp.StatusCode != 422 {
		t.Fatalf("0 days accepted: %d", resp.StatusCode)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "Kolega", "expiresInDays": 30, "run": map[string]any{"command": "/bin/sleep", "args": []string{"600"}}})
	if resp.StatusCode != 201 || body["expiresAt"] == nil || body["expired"] != false {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	exp, _ := time.Parse(time.RFC3339, body["expiresAt"].(string))
	if d := time.Until(exp); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Fatalf("expiresAt %v", exp)
	}
	resp, _ = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]any{"action": "start"})
	if resp.StatusCode >= 300 {
		t.Fatalf("start: %d", resp.StatusCode)
	}
	e.waitServerState(id, "running")

	srv := e.api()
	srv.checkExpiries(context.Background(), exp.Add(-48*time.Hour))
	if _, body = e.do(http.MethodGet, "/api/v1/servers/"+id, nil); body["expired"] != false {
		t.Fatalf("expired too early: %v", body)
	}
	srv.checkExpiries(context.Background(), exp.Add(time.Minute))
	e.waitServerState(id, "stopped")
	_, body = e.do(http.MethodGet, "/api/v1/servers/"+id, nil)
	if body["expired"] != true || body["name"] != "Kolega" {
		t.Fatalf("after expiry (must still exist): %v", body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]any{"action": "start"})
	if resp.StatusCode != 409 || errCode(body) != CodeServerExpired {
		t.Fatalf("start expired: %d %v", resp.StatusCode, body)
	}

	friend, _ := e.store.CreateUser(t.Context(), "kolega", "password123", auth.RoleUser)
	_ = e.store.GrantPermissions(t.Context(), id, friend.ID, auth.AllPermissions)
	owner := e.client
	ownerCSRF := e.csrf
	e.loginAs("kolega")
	resp, _ = e.do(http.MethodPut, "/api/v1/servers/"+id+"/expiry", map[string]any{"extendDays": 30})
	if resp.StatusCode != 403 {
		t.Fatalf("user extended: %d", resp.StatusCode)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]any{"action": "start"})
	if resp.StatusCode != 409 {
		t.Fatalf("user started expired server: %d %v", resp.StatusCode, body)
	}
	e.client, e.csrf = owner, ownerCSRF

	resp, body = e.do(http.MethodPut, "/api/v1/servers/"+id+"/expiry", map[string]any{"extendDays": 7})
	if resp.StatusCode != 200 || body["expired"] != false {
		t.Fatalf("extend: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(http.MethodPut, "/api/v1/servers/"+id+"/expiry", map[string]any{"expiresAt": "2000-01-01"})
	if resp.StatusCode != 422 {
		t.Fatalf("past date: %d", resp.StatusCode)
	}
	resp, body = e.do(http.MethodPut, "/api/v1/servers/"+id+"/expiry", map[string]any{"expiresAt": nil})
	if resp.StatusCode != 200 || body["expiresAt"] != nil {
		t.Fatalf("clear: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]any{"action": "start"})
	if resp.StatusCode >= 300 {
		t.Fatalf("start after clearing: %d", resp.StatusCode)
	}
	e.waitServerState(id, "running")
	e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]any{"action": "stop"})
}

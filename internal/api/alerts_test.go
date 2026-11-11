package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/C7PEK/cypek.dev-panel/internal/alerts"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

type captureSender struct {
	mu    sync.Mutex
	hooks []string
	msgs  []alerts.Message
	fail  error
}

func (c *captureSender) Send(_ context.Context, webhook string, msg alerts.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hooks = append(c.hooks, webhook)
	c.msgs = append(c.msgs, msg)
	return c.fail
}

const testHook = "https://discord.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz_SECRET-9876"

func TestAlertsAPI(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")

	resp, body := e.do(http.MethodGet, "/api/v1/alerts", nil)
	st := body["settings"].(map[string]any)
	if resp.StatusCode != 200 || st["webhookSet"] != false || st["graceSec"].(float64) != 20 {
		t.Fatalf("defaults: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/alerts/test", map[string]any{})
	if resp.StatusCode != 409 || errCode(body) != CodeNoWebhook {
		t.Fatalf("test without webhook: %d %v", resp.StatusCode, body)
	}
	for field, in := range map[string]map[string]any{
		"webhook":  {"webhook": "https://evil.example/api/webhooks/1/x", "graceSec": 20},
		"mention":  {"mention": "@admins", "graceSec": 20},
		"graceSec": {"graceSec": 2},
		"panelUrl": {"graceSec": 20, "panelUrl": "javascript:alert(1)"},
	} {
		resp, body = e.do(http.MethodPut, "/api/v1/alerts", in)
		if resp.StatusCode != 422 || body["error"].(map[string]any)["field"] != field {
			t.Errorf("%s: %d %v", field, resp.StatusCode, body)
		}
	}
	resp, body = e.do(http.MethodPut, "/api/v1/alerts", map[string]any{"webhook": testHook, "mention": "<@&111111111111>", "graceSec": 30, "notifyRecovered": true, "includeLogs": true, "language": "pl", "panelUrl": "http://192.168.1.5:8420/servers"})
	st = body["settings"].(map[string]any)
	if resp.StatusCode != 200 || st["webhookSet"] != true || st["webhookHint"] != "…/9876" || st["panelUrl"] != "http://192.168.1.5:8420" {
		t.Fatalf("save: %d %v", resp.StatusCode, body)
	}

	resp, body = e.do(http.MethodPut, "/api/v1/alerts", map[string]any{"mention": "", "graceSec": 25})
	_, raw := e.do(http.MethodGet, "/api/v1/alerts", nil)
	if resp.StatusCode != 200 || raw["settings"].(map[string]any)["webhookSet"] != true {
		t.Fatalf("keep webhook: %d %v", resp.StatusCode, raw)
	}
	if strings.Contains(stringify(raw), "SECRET") || strings.Contains(stringify(body), "SECRET") {
		t.Fatal("webhook token leaked in API response")
	}
	resp, body = e.do(http.MethodPost, "/api/v1/alerts/test", map[string]any{})
	if resp.StatusCode != 200 || len(e.sender.msgs) != 1 || e.sender.hooks[0] != testHook {
		t.Fatalf("test: %d %v %d", resp.StatusCode, body, len(e.sender.msgs))
	}
	e.sender.fail = errors.New("Discord rejected the webhook (HTTP 404)")
	resp, body = e.do(http.MethodPost, "/api/v1/alerts/test", map[string]any{})
	if resp.StatusCode != 502 || errCode(body) != CodeWebhookFailed {
		t.Fatalf("failing test: %d %v", resp.StatusCode, body)
	}
	e.sender.fail = nil
	_, body = e.do(http.MethodGet, "/api/v1/alerts", nil)
	if h := body["history"].([]any); len(h) != 2 || h[0].(map[string]any)["ok"] != false {
		t.Errorf("history: %v", h)
	}

	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "Bot", "run": map[string]any{"command": "/bin/true"}})
	id := body["id"].(string)
	resp, body = e.do(http.MethodGet, "/api/v1/servers/"+id+"/alerts", nil)
	if resp.StatusCode != 200 || body["enabled"] != true || body["globalWebhookSet"] != true || body["webhookSet"] != false {
		t.Fatalf("server alerts: %d %v", resp.StatusCode, body)
	}
	own := strings.Replace(testHook, "123456789012345678", "999999999999999999", 1)
	resp, body = e.do(http.MethodPut, "/api/v1/servers/"+id+"/alerts", map[string]any{"enabled": true, "webhook": own, "mention": "@here"})
	if resp.StatusCode != 200 || body["webhookSet"] != true || body["mention"] != "@here" {
		t.Fatalf("server save: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(http.MethodPost, "/api/v1/servers/"+id+"/alerts/test", nil)
	if resp.StatusCode != 200 || e.sender.hooks[len(e.sender.hooks)-1] != own {
		t.Fatalf("server test: %d %v", resp.StatusCode, e.sender.hooks)
	}
	last := e.sender.msgs[len(e.sender.msgs)-1]
	if !strings.HasPrefix(last.Content, "@here") || !strings.Contains(last.Embeds[0].Title, "Bot") {
		t.Errorf("server test message: %+v", last)
	}

	plain, _ := e.store.CreateUser(t.Context(), "plain", "password123", auth.RoleUser)
	_ = e.store.GrantPermissions(t.Context(), id, plain.ID, []auth.Permission{auth.PermConsoleRead})
	e.loginAs("plain")
	for _, c := range []struct{ m, p string }{
		{http.MethodGet, "/api/v1/alerts"}, {http.MethodPut, "/api/v1/alerts"}, {http.MethodPost, "/api/v1/alerts/test"},
		{http.MethodGet, "/api/v1/servers/" + id + "/alerts"}, {http.MethodPut, "/api/v1/servers/" + id + "/alerts"},
	} {
		resp, _ := e.do(c.m, c.p, map[string]any{})
		if resp.StatusCode != 403 {
			t.Errorf("%s %s as plain user: %d", c.m, c.p, resp.StatusCode)
		}
	}
}

func stringify(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

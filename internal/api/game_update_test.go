package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGameUpdateStopsRunsOnlyUpdateStepsAndRestarts(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	yml := "id: upd-test\nname: Update test\ncategory: other\nvariables: []\ninstall:\n" +
		"  - write: {path: server.cfg, content: \"original\\n\"}\n" +
		"  - exec: {command: /bin/sh, args: [\"-c\", \"mkdir -p artifact && echo v1 > artifact/version\"]}\n" +
		"update:\n" +
		"  - exec: {command: /bin/sh, args: [\"-c\", \"echo v2 > artifact/version\"]}\n" +
		"start: {command: /bin/sleep, args: [\"60\"]}\n"
	if resp, body := e.do(http.MethodPost, "/api/v1/templates", map[string]any{"yaml": yml}); resp.StatusCode != 201 {
		t.Fatalf("template: %d %v", resp.StatusCode, body)
	}
	resp, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "U", "templateId": "upd-test", "variables": map[string]string{}})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	if inst := e.waitInstall(id); inst["state"] != "done" {
		t.Fatalf("install: %v", inst)
	}
	dir := filepath.Join(e.cfg.ServersDir(), id)
	_ = os.WriteFile(filepath.Join(dir, "server.cfg"), []byte("edited by the user\n"), 0o644)

	_, body = e.do(http.MethodGet, "/api/v1/servers/"+id, nil)
	if body["gameUpdate"] != true {
		t.Fatalf("gameUpdate flag missing: %v", body)
	}
	resp, body = e.do(http.MethodGet, "/api/v1/servers/"+id+"/game-update", nil)
	if resp.StatusCode != 200 || body["kind"] != "files" {
		t.Fatalf("info: %d %v", resp.StatusCode, body)
	}

	e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]any{"action": "start"})
	e.waitServerState(id, "running")
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/game-update", map[string]any{"restart": true})
	if resp.StatusCode != 202 {
		t.Fatalf("update: %d %v", resp.StatusCode, body)
	}
	inst := e.waitInstall(id)
	if inst["state"] != "done" {
		t.Fatalf("update job: %v", inst)
	}
	e.waitServerState(id, "running")
	if b, _ := os.ReadFile(filepath.Join(dir, "artifact", "version")); strings.TrimSpace(string(b)) != "v2" {
		t.Errorf("artifact/version = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "server.cfg")); string(b) != "edited by the user\n" {
		t.Errorf("update rewrote server.cfg: %q", b)
	}
	log := strings.Join(toStrings(inst["lines"]), "\n")
	if !strings.Contains(log, "Stopping the server for the update") || !strings.Contains(log, "Update finished.") {
		t.Errorf("update log:\n%s", log)
	}
	e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]any{"action": "stop"})
	e.waitServerState(id, "stopped")

	_, body = e.do(http.MethodGet, "/api/v1/audit", nil)
	found := false
	for _, it := range body["items"].([]any) {
		if it.(map[string]any)["action"] == "server.game_update" {
			found = true
		}
	}
	if !found {
		t.Error("audit has no server.game_update")
	}
}

func TestGameUpdateNeedsAnUpdateSection(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	yml := "id: no-upd\nname: N\ncategory: other\nvariables: []\ninstall: []\nstart: {command: /bin/sleep, args: [\"60\"]}\n"
	if resp, body := e.do(http.MethodPost, "/api/v1/templates", map[string]any{"yaml": yml}); resp.StatusCode != 201 {
		t.Fatalf("template: %d %v", resp.StatusCode, body)
	}
	_, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "N", "templateId": "no-upd", "variables": map[string]string{}})
	id := body["id"].(string)
	e.waitInstall(id)
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		resp, body := e.do(m, "/api/v1/servers/"+id+"/game-update", map[string]any{})
		if resp.StatusCode != 409 || errCode(body) != CodeNoUpdate {
			t.Errorf("%s: %d %v", m, resp.StatusCode, body)
		}
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestCreateWebsite(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	for _, c := range []struct{ tpl, category string }{{"website-static", "website"}} {
		resp, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": c.tpl, "templateId": c.tpl, "variables": map[string]string{}})
		if resp.StatusCode != 201 {
			t.Fatalf("create %s: %d %v", c.tpl, resp.StatusCode, body)
		}
		_, got := e.do(http.MethodGet, "/api/v1/servers/"+body["id"].(string), nil)
		if got["category"] != c.category {
			t.Errorf("%s category = %v", c.tpl, got["category"])
		}
	}
}

package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

func TestModsAPI(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	resp, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "Other", "run": map[string]any{"command": "/bin/true"}})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	resp, body = e.do(http.MethodGet, "/api/v1/servers/"+id+"/mods", nil)
	if resp.StatusCode != 409 || errCode(body) != CodeNotMinecraft {
		t.Fatalf("non-minecraft: %d %v", resp.StatusCode, body)
	}

	withFakeUpstream(t, e)
	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "MC", "templateId": "minecraft-paper",
		"variables": map[string]string{"MC_VERSION": "latest", "EULA": "true", "PORT": "25571"}})
	if resp.StatusCode != 201 {
		t.Fatalf("create mc: %d %v", resp.StatusCode, body)
	}
	id = body["id"].(string)
	if st := e.waitInstall(id); st["state"] != "done" {
		t.Fatalf("install: %v", st)
	}
	dir := filepath.Join(e.cfg.ServersDir(), id)
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	os.WriteFile(filepath.Join(dir, "spigot.yml"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "plugins", "a.jar"), []byte("x"), 0o644)

	resp, body = e.do(http.MethodGet, "/api/v1/servers/"+id+"/mods", nil)
	plat, _ := body["platform"].(map[string]any)
	if resp.StatusCode != 200 || plat["loader"] != "spigot" || plat["dir"] != "plugins" || plat["gameVersion"] == "" || len(body["installed"].([]any)) != 1 {
		t.Fatalf("list: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPut, "/api/v1/servers/"+id+"/mods/platform", map[string]any{"loader": "paper", "gameVersion": "1.21.4"})
	if resp.StatusCode != 200 || body["loader"] != "paper" || body["overridden"] != true {
		t.Fatalf("platform: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(http.MethodPut, "/api/v1/servers/"+id+"/mods/platform", map[string]any{"loader": "rm -rf", "gameVersion": ""})
	if resp.StatusCode != 422 {
		t.Fatalf("bad loader: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodPatch, "/api/v1/servers/"+id+"/mods/files/a.jar", map[string]any{"enabled": false})
	if resp.StatusCode != 200 {
		t.Fatalf("disable: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodDelete, "/api/v1/servers/"+id+"/mods/files/..%2Fspigot.yml", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("traversal delete: %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "spigot.yml")); err != nil {
		t.Fatal("spigot.yml was deleted")
	}
	resp, _ = e.do(http.MethodPost, "/api/v1/servers/"+id+"/mods", map[string]any{"projectId": "../x"})
	if resp.StatusCode != 400 {
		t.Fatalf("bad project id: %d", resp.StatusCode)
	}

	plain, _ := e.store.CreateUser(t.Context(), "plain", "password123", auth.RoleUser)
	_ = e.store.GrantPermissions(t.Context(), id, plain.ID, []auth.Permission{auth.PermFilesRead})
	e.loginAs("plain")
	resp, _ = e.do(http.MethodGet, "/api/v1/servers/"+id+"/mods", nil)
	if resp.StatusCode != 200 {
		t.Errorf("files.read list: %d", resp.StatusCode)
	}
	for _, c := range []struct{ m, p string }{
		{http.MethodPost, "/mods"}, {http.MethodPut, "/mods/platform"},
		{http.MethodDelete, "/mods/files/a.jar.disabled"}, {http.MethodPatch, "/mods/files/a.jar.disabled"},
	} {
		resp, _ := e.do(c.m, "/api/v1/servers/"+id+c.p, map[string]any{})
		if resp.StatusCode != 403 {
			t.Errorf("%s %s as files.read: %d", c.m, c.p, resp.StatusCode)
		}
	}
}

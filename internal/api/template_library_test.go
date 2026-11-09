package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A minimal Pterodactyl egg: the script writes a file from a variable whose
// default is a shell injection attempt; the start command echoes stdin.
func testEgg(t *testing.T) string {
	t.Helper()
	egg := map[string]any{
		"_comment":    "DO NOT EDIT: FILE GENERATED AUTOMATICALLY BY PTERODACTYL PANEL - PTERODACTYL.IO",
		"meta":        map[string]any{"version": "PTDL_v2"},
		"name":        "Echo Game",
		"author":      "tests@example.com",
		"description": "Test egg",
		"docker_images": map[string]string{
			"Debian": "ghcr.io/parkervcp/yolks:debian",
		},
		"startup": `echo "Done (0.01s)! port {{SERVER_PORT}} greeting {{GREETING}}"; while read -r line; do [ "$line" = "stop" ] && exit 0; echo "> $line"; done`,
		"config": map[string]any{
			"files":   `{"server.properties": {"parser": "properties", "find": {"server-port": "{{server.build.default.port}}"}}}`,
			"startup": `{"done": "Done ("}`,
			"logs":    "{}",
			"stop":    "stop",
		},
		"scripts": map[string]any{"installation": map[string]any{
			"script":     "#!/bin/bash\r\napt update\r\napt install -y curl jq\r\ncd /mnt/server\r\nmkdir -p data\r\nprintf '%s' \"${GREETING}\" > /mnt/server/data/hello.txt\r\necho installed-ok\r\n",
			"container":  "ghcr.io/ptero-eggs/installers:debian",
			"entrypoint": "bash",
		}},
		"variables": []any{
			map[string]any{"name": "Greeting", "env_variable": "GREETING", "default_value": "$(touch pwned)`touch pwned2`", "user_viewable": true, "user_editable": true, "rules": "required|string|max:64", "field_type": "text"},
		},
	}
	b, _ := json.Marshal(egg)
	return string(b)
}

func TestTemplateLibraryImportInstallAndDelete(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")

	// Invalid YAML is rejected with a readable message.
	resp, body := e.do(http.MethodPost, "/api/v1/templates/validate", map[string]any{"yaml": "id: x\n"})
	if resp.StatusCode != 422 || errCode(body) != CodeTemplateInvalid {
		t.Fatalf("validate invalid: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/templates/import", map[string]any{"content": "{\"hello\": 1}"})
	if resp.StatusCode != 422 || errCode(body) != CodeImportFailed {
		t.Fatalf("import junk: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/templates/import", map[string]any{"url": "http://example.com/egg.json"})
	if resp.StatusCode != 422 || errCode(body) != CodeImportFailed {
		t.Fatalf("import http url: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/templates/import", map[string]any{"url": "https://127.0.0.1:1/egg.json"})
	if resp.StatusCode != 422 || !strings.Contains(body["error"].(map[string]any)["message"].(string), "not a public address") {
		t.Fatalf("import loopback url: %d %v", resp.StatusCode, body)
	}

	// Import converts the egg; the YAML is saved as a custom template.
	resp, body = e.do(http.MethodPost, "/api/v1/templates/import", map[string]any{"content": testEgg(t)})
	if resp.StatusCode != 200 || body["format"] != "pterodactyl" {
		t.Fatalf("import: %d %v", resp.StatusCode, body)
	}
	yamlText := body["yaml"].(string)
	tpl := body["template"].(map[string]any)
	if tpl["id"] != "egg-echo-game" || tpl["usesScript"] != true || tpl["source"].(map[string]any)["kind"] != "pterodactyl" {
		t.Fatalf("imported template: %v", tpl)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/templates", map[string]any{"yaml": yamlText})
	if resp.StatusCode != 201 {
		t.Fatalf("save: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/templates", map[string]any{"yaml": yamlText})
	if resp.StatusCode != 409 || errCode(body) != CodeTemplateExists {
		t.Fatalf("save twice: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodGet, "/api/v1/templates/egg-echo-game/yaml", nil)
	if resp.StatusCode != 200 || body["editable"] != true || !strings.Contains(body["yaml"].(string), "egg-echo-game") {
		t.Fatalf("yaml: %d %v", resp.StatusCode, body)
	}
	// Editing must keep the id.
	resp, _ = e.do(http.MethodPut, "/api/v1/templates/egg-echo-game", map[string]any{"yaml": strings.Replace(yamlText, "id: egg-echo-game", "id: egg-other", 1)})
	if resp.StatusCode != 400 {
		t.Fatalf("rename via PUT: %d", resp.StatusCode)
	}
	resp, body = e.do(http.MethodPut, "/api/v1/templates/egg-echo-game", map[string]any{"yaml": strings.Replace(yamlText, "name: Echo Game", "name: Echo Game 2", 1)})
	if resp.StatusCode != 200 || body["template"].(map[string]any)["name"] != "Echo Game 2" {
		t.Fatalf("update: %d %v", resp.StatusCode, body)
	}

	// Built-ins are read-only.
	resp, _ = e.do(http.MethodPut, "/api/v1/templates/minecraft-paper", map[string]any{"yaml": yamlText})
	if resp.StatusCode != 403 {
		t.Fatalf("PUT builtin: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodDelete, "/api/v1/templates/minecraft-paper", nil)
	if resp.StatusCode != 403 {
		t.Fatalf("DELETE builtin: %d", resp.StatusCode)
	}

	// Install runs the script in the server's own (local) supervisor unit.
	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{
		"name": "Echo", "templateId": "egg-echo-game",
		"variables": map[string]string{"SERVER_PORT": "25599", "GREETING": "$(touch pwned)`touch pwned2`"},
	})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	inst := e.waitInstall(id)
	if inst["state"] != "done" {
		t.Fatalf("install failed: %v", inst)
	}
	log, _ := json.Marshal(inst["lines"])
	if !strings.Contains(string(log), "installed-ok") || !strings.Contains(string(log), "skipped (no root): apt") {
		t.Errorf("install log misses script output: %s", log)
	}
	dir := filepath.Join(e.cfg.ServersDir(), id)
	got, err := os.ReadFile(filepath.Join(dir, "data", "hello.txt"))
	if err != nil || string(got) != "$(touch pwned)`touch pwned2`" {
		t.Fatalf("hello.txt = %q, %v", got, err)
	}
	for _, f := range []string{"pwned", "pwned2", filepath.Join(".homepanel", "install.sh")} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s exists", f)
		}
	}
	props, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.Contains(string(props), "server-port=25599") {
		t.Errorf("server.properties = %q", props)
	}

	// The start command runs through bash with values from the environment.
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]any{"action": "start"})
	if resp.StatusCode != 200 && resp.StatusCode != 202 && resp.StatusCode != 204 {
		t.Fatalf("start: %d %v", resp.StatusCode, body)
	}
	e.waitServerState(id, "running")
	out := e.consoleSession(id, "hello\n", true)
	if !strings.Contains(out, "port 25599 greeting $(touch pwned)`touch pwned2`") {
		t.Errorf("console: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("start command executed a variable value")
	}
	e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]any{"action": "stop"})
	e.waitServerState(id, "stopped")

	// Deleting a template in use needs force.
	resp, body = e.do(http.MethodDelete, "/api/v1/templates/egg-echo-game", nil)
	if resp.StatusCode != 409 || errCode(body) != CodeTemplateInUse {
		t.Fatalf("delete in use: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(http.MethodDelete, "/api/v1/templates/egg-echo-game?force=1", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("force delete: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodGet, "/api/v1/templates/egg-echo-game/yaml", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("deleted template still there: %d", resp.StatusCode)
	}
}

func TestTemplateLibraryNeedsAdmin(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	resp, _ := e.do(http.MethodPost, "/api/v1/users", map[string]any{"username": "bob", "password": "password123", "role": "user"})
	if resp.StatusCode != 201 {
		t.Fatalf("create user: %d", resp.StatusCode)
	}
	e.loginAs("bob")
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/templates"},
		{http.MethodPost, "/api/v1/templates/import"},
		{http.MethodPost, "/api/v1/templates"},
		{http.MethodDelete, "/api/v1/templates/x"},
	} {
		resp, _ := e.do(c.method, c.path, map[string]any{})
		if resp.StatusCode != 403 {
			t.Errorf("%s %s as user: %d", c.method, c.path, resp.StatusCode)
		}
	}
}

// Install exec steps run through the server's supervisor unit (the sandbox
// in production), not as a child of the panel.
func TestExecStepRunsThroughServerUnit(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	yml := "id: exec-test\nname: Exec Test\ncategory: other\nvariables: []\ninstall:\n" +
		"  - exec: {command: /bin/sh, args: [\"-c\", \"echo ran-as-unit $HOMEPANEL_UNIT_MARK; touch made-by-exec\"]}\n" +
		"start: {command: /bin/sleep, args: [\"60\"]}\n"
	if resp, body := e.do(http.MethodPost, "/api/v1/templates", map[string]any{"yaml": yml}); resp.StatusCode != 201 {
		t.Fatalf("template: %d %v", resp.StatusCode, body)
	}
	resp, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "X", "templateId": "exec-test", "variables": map[string]string{}})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	inst := e.waitInstall(id)
	if inst["state"] != "done" {
		t.Fatalf("install: %v", inst)
	}
	if _, err := os.Stat(filepath.Join(e.cfg.ServersDir(), id, "made-by-exec")); err != nil {
		t.Fatal("exec step did not run")
	}
	// The shim's console log only exists when the shim ran the program.
	logs, _ := filepath.Glob(filepath.Join(e.cfg.ServersDir(), id, "logs", "console-*.log"))
	if len(logs) == 0 {
		t.Fatal("exec step ran outside the server unit (no shim console log)")
	}
}

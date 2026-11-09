package api

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestPortConflicts(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	create := func(name string, port int, extra map[string]any) (*http.Response, map[string]any) {
		body := map[string]any{"name": name, "run": map[string]any{"command": "/bin/sleep", "args": []string{"60"}},
			"ports": []map[string]any{{"name": "game", "port": port, "protocol": "tcp+udp"}}}
		for k, v := range extra {
			body[k] = v
		}
		return e.do(http.MethodPost, "/api/v1/servers", body)
	}
	resp, body := create("FiveM A", 30120, nil)
	if resp.StatusCode != 201 {
		t.Fatalf("create A: %d %v", resp.StatusCode, body)
	}
	idA := body["id"].(string)

	resp, body = create("FiveM B", 30120, nil)
	errBody := body["error"].(map[string]any)
	params, _ := errBody["params"].(map[string]any)
	if resp.StatusCode != 409 || errBody["code"] != CodePortInUse || errBody["field"] != "ports" || params["server"] != "FiveM A" || params["suggestion"].(float64) != 30121 {
		t.Fatalf("conflict: %d %v", resp.StatusCode, body)
	}
	if !strings.Contains(errBody["message"].(string), `"FiveM A"`) {
		t.Errorf("message: %v", errBody["message"])
	}
	// UDP-only on a TCP port of another server is fine; forcing is allowed.
	resp, _ = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "UDP", "run": map[string]any{"command": "/bin/true"},
		"ports": []map[string]any{{"name": "voice", "port": 9987, "protocol": "udp"}}})
	if resp.StatusCode != 201 {
		t.Fatalf("udp: %d", resp.StatusCode)
	}
	resp, body = create("FiveM B", 30120, map[string]any{"forcePorts": true})
	if resp.StatusCode != 201 {
		t.Fatalf("forced: %d %v", resp.StatusCode, body)
	}
	idB := body["id"].(string)

	// Both may exist, but not run together.
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+idA+"/power", map[string]any{"action": "start"})
	if resp.StatusCode >= 300 {
		t.Fatalf("start A: %d %v", resp.StatusCode, body)
	}
	e.waitServerState(idA, "running")
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+idB+"/power", map[string]any{"action": "start"})
	if resp.StatusCode != 409 || errCode(body) != CodePortInUse || body["error"].(map[string]any)["params"].(map[string]any)["running"] != true {
		t.Fatalf("start B while A runs: %d %v", resp.StatusCode, body)
	}
	// A restart of A itself is not blocked by its own port.
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+idA+"/power", map[string]any{"action": "stop"})
	if resp.StatusCode >= 300 {
		t.Fatalf("stop A: %d %v", resp.StatusCode, body)
	}

	// Saving unrelated settings keeps working; changing to a taken port does not.
	resp, _ = e.do(http.MethodPatch, "/api/v1/servers/"+idB, map[string]any{"name": "FiveM B2"})
	if resp.StatusCode != 200 {
		t.Fatalf("rename with old conflict: %d", resp.StatusCode)
	}
	resp, body = e.do(http.MethodPatch, "/api/v1/servers/"+idB, map[string]any{"ports": []map[string]any{{"name": "game", "port": 9987, "protocol": "udp"}}})
	if resp.StatusCode != 409 || errCode(body) != CodePortInUse {
		t.Fatalf("patch to taken port: %d %v", resp.StatusCode, body)
	}

	// Another program on the host.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port
	resp, body = e.do(http.MethodPost, "/api/v1/ports/check", map[string]any{"ports": []map[string]any{{"name": "game", "port": busy, "protocol": "tcp"}, {"name": "x", "port": 30120, "protocol": "udp"}}})
	cs := body["conflicts"].([]any)
	if resp.StatusCode != 200 || len(cs) != 2 || cs[0].(map[string]any)["host"] != true || cs[1].(map[string]any)["serverName"] == "" {
		t.Fatalf("check: %d %v", resp.StatusCode, body)
	}
	resp, body = create("Busy", busy, nil)
	if resp.StatusCode != 409 || errCode(body) != CodePortInUseHost {
		t.Fatalf("host conflict: %d %v", resp.StatusCode, body)
	}
}

func TestTemplatePortConflictPointsAtVariable(t *testing.T) {
	e := newEnv(t)
	withFakeUpstream(t, e)
	e.completeSetup("owner", "password123")
	vars := map[string]string{"MC_VERSION": "latest", "EULA": "true", "PORT": "25570"}
	resp, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "MC1", "templateId": "minecraft-paper", "variables": vars})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	if st := e.waitInstall(body["id"].(string)); st["state"] != "done" {
		t.Fatalf("install: %v", st)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "MC2", "templateId": "minecraft-paper", "variables": vars})
	if resp.StatusCode != 409 || body["error"].(map[string]any)["field"] != "variables.PORT" {
		t.Fatalf("template conflict: %d %v", resp.StatusCode, body)
	}
}

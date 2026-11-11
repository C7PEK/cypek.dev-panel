package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
)

func withFakeUpstream(t *testing.T, e *testEnv) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	script := "#!/bin/sh\necho 'Done (0.1s)! For help, type \"help\"'\nwhile read l; do [ \"$l\" = stop ] && exit 0; echo \"> $l\"; done\n"
	_ = tw.WriteHeader(&tar.Header{Name: "jre/bin/java", Mode: 0o755, Typeflag: tar.TypeReg, Size: int64(len(script))})
	_, _ = tw.Write([]byte(script))
	tw.Close()
	gz.Close()
	jre := buf.Bytes()
	jreSum := sha256.Sum256(jre)
	jar := []byte("jar")
	jarSum := sha256.Sum256(jar)

	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/paper", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"versions": map[string][]string{"1.21": {"1.21.11", "1.21.11-rc1", "1.21.10"}}})
	})
	for _, v := range []string{"1.21.11", "1.21.10"} {
		v := v
		mux.HandleFunc("/projects/paper/versions/"+v, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"version": map[string]any{"java": map[string]any{
				"version": map[string]int{"minimum": 21}, "flags": map[string][]string{"recommended": {"-XX:+UseG1GC"}}}}})
		})
		mux.HandleFunc("/projects/paper/versions/"+v+"/builds", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 7, "channel": "STABLE", "downloads": map[string]any{"server:default": map[string]any{
				"name": "paper-" + v + "-7.jar", "url": srv.URL + "/paper.jar", "checksums": map[string]string{"sha256": hex.EncodeToString(jarSum[:])}}}}})
		})
	}
	mux.HandleFunc("/paper.jar", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(jar) })
	mux.HandleFunc("/info/available_releases", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"available_lts_releases": []int{17, 21, 25}})
	})
	mux.HandleFunc("/assets/latest/21/hotspot", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"release_name": "jdk-21", "binary": map[string]any{"package": map[string]any{
			"link": srv.URL + "/jre.tar.gz", "checksum": hex.EncodeToString(jreSum[:]), "size": len(jre)}}}})
	})
	mux.HandleFunc("/jre.tar.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(jre) })
	srv = httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	oldD, oldC := providers.DownloadClient, providers.Client
	providers.DownloadClient, providers.Client = srv.Client(), srv.Client()
	t.Cleanup(func() { providers.DownloadClient, providers.Client = oldD, oldC })
	e.paper.BaseURL = srv.URL
	e.adoptium.BaseURL = srv.URL
}

func (e *testEnv) waitInstall(id string) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, body := e.do(http.MethodGet, "/api/v1/servers/"+id+"/install", nil)
		if body["state"] == "done" || body["state"] == "failed" {
			return body
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("install never finished: %v", body)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTemplatesAndInstallFlow(t *testing.T) {
	e := newEnv(t)
	withFakeUpstream(t, e)
	e.completeSetup("owner", "password123")

	resp, body := e.do(http.MethodGet, "/api/v1/templates", nil)
	items := body["items"].([]any)
	if resp.StatusCode != 200 || len(items) == 0 {
		t.Fatalf("templates: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodGet, "/api/v1/templates/minecraft-paper/sources/paper-versions", nil)
	opts := body["options"].([]any)
	if resp.StatusCode != 200 || opts[0].(map[string]any)["value"] != "latest" || len(opts) != 4 {
		t.Fatalf("source: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(http.MethodGet, "/api/v1/templates/minecraft-paper/sources/nope", nil)
	if resp.StatusCode != 404 {
		t.Errorf("unknown source: %d", resp.StatusCode)
	}

	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "MC", "templateId": "minecraft-paper", "variables": map[string]string{"MC_VERSION": "latest"}})
	if resp.StatusCode != 422 || body["error"].(map[string]any)["field"] != "variables.EULA" {
		t.Fatalf("no consent: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "MC", "templateId": "minecraft-paper", "variables": map[string]string{"MC_VERSION": "9.9", "EULA": "true"}})
	if resp.StatusCode != 422 || body["error"].(map[string]any)["field"] != "variables.MC_VERSION" {
		t.Fatalf("bogus version: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "MC", "templateId": "nope", "variables": map[string]string{}})
	if resp.StatusCode != 422 {
		t.Fatalf("unknown template: %d %v", resp.StatusCode, body)
	}

	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "MC", "templateId": "minecraft-paper",
		"variables": map[string]string{"MC_VERSION": "latest", "EULA": "true", "PORT": "25570"}})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	if body["installState"] == "ready" || body["category"] != "minecraft" || body["templateId"] != "minecraft-paper" {
		t.Errorf("created = %v", body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "start"})
	if resp.StatusCode != 409 || errCode(body) != CodeNotInstalled {
		t.Errorf("start before install: %d %v", resp.StatusCode, body)
	}
	job := e.waitInstall(id)
	if job["state"] != "done" {
		t.Fatalf("install failed: %v", job)
	}
	if lines := job["lines"].([]any); len(lines) == 0 {
		t.Error("no install log lines")
	}
	_, body = e.do(http.MethodGet, "/api/v1/servers/"+id, nil)
	if body["installState"] != "ready" {
		t.Fatalf("after install = %v", body)
	}
	run := body["run"].(map[string]any)
	if !strings.HasSuffix(run["command"].(string), "/java-21/bin/java") || run["stop"].(map[string]any)["command"] != "stop" {
		t.Errorf("run = %v", run)
	}
	ports := body["ports"].([]any)
	if len(ports) != 1 || ports[0].(map[string]any)["port"].(float64) != 25570 {
		t.Errorf("ports = %v", ports)
	}
	vars := body["variables"].(map[string]any)
	if vars["PORT"] != "25570" || vars["JAVA"] != nil {
		t.Errorf("variables exposed = %v", vars)
	}
	props, _ := os.ReadFile(filepath.Join(e.cfg.ServersDir(), id, "server.properties"))
	if !strings.Contains(string(props), "server-port=25570") {
		t.Errorf("server.properties = %s", props)
	}

	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "start"})
	if resp.StatusCode != 200 {
		t.Fatalf("start: %d %v", resp.StatusCode, body)
	}
	e.waitServerState(id, "running")
	if out := e.consoleSession(id, "list\n", true); !strings.Contains(out, "> list") {
		t.Errorf("console = %q", out)
	}
	resp, body = e.do(http.MethodPatch, "/api/v1/servers/"+id, map[string]any{"variables": map[string]string{"PORT": "25580", "MOTD": "Hi"}})
	if resp.StatusCode != 200 || body["variables"].(map[string]any)["PORT"] != "25580" {
		t.Fatalf("patch variables: %d %v", resp.StatusCode, body)
	}
	props, _ = os.ReadFile(filepath.Join(e.cfg.ServersDir(), id, "server.properties"))
	if !strings.Contains(string(props), "server-port=25580") || !strings.Contains(string(props), "motd=Hi") {
		t.Errorf("server.properties after patch = %s", props)
	}
	resp, body = e.do(http.MethodPatch, "/api/v1/servers/"+id, map[string]any{"variables": map[string]string{"RAM_MB": "1"}})
	if resp.StatusCode != 422 || body["error"].(map[string]any)["field"] != "variables.RAM_MB" {
		t.Errorf("bad variable: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(http.MethodPost, "/api/v1/servers/"+id+"/install", nil)
	if resp.StatusCode != 409 {
		t.Errorf("reinstall while running: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "stop"})
	if resp.StatusCode != 200 {
		t.Fatalf("stop: %d", resp.StatusCode)
	}
	e.waitServerState(id, "stopped")
	resp, _ = e.do(http.MethodPost, "/api/v1/servers/"+id+"/install", nil)
	if resp.StatusCode != 202 {
		t.Errorf("reinstall: %d", resp.StatusCode)
	}
	if job := e.waitInstall(id); job["state"] != "done" {
		t.Errorf("reinstall job = %v", job)
	}
	resp, _ = e.do(http.MethodDelete, "/api/v1/servers/"+id, nil)
	if resp.StatusCode != 204 {
		t.Errorf("delete: %d", resp.StatusCode)
	}
}

func TestCustomServerStillWorksWithTemplatesWired(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	resp, body := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Custom"))
	if resp.StatusCode != 201 || body["installState"] != "ready" || body["templateId"] != "custom" {
		t.Fatalf("custom create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	resp, body = e.do(http.MethodPatch, "/api/v1/servers/"+id, map[string]any{"variables": map[string]string{"X": "1"}})
	if resp.StatusCode != 422 {
		t.Errorf("variables on custom server: %d %v", resp.StatusCode, body)
	}
}

func TestConsoleHints(t *testing.T) {
	e := newEnv(t)
	withFakeUpstream(t, e)
	e.completeSetup("owner", "password123")
	resp, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "MC", "templateId": "minecraft-paper",
		"variables": map[string]string{"MC_VERSION": "latest", "EULA": "true"}})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	e.waitInstall(id)
	resp, body = e.do(http.MethodGet, "/api/v1/servers/"+id+"/console/hints", nil)
	cmds := body["commands"].([]any)
	if resp.StatusCode != 200 || len(cmds) < 20 {
		t.Fatalf("hints: %d %v", resp.StatusCode, body)
	}
	found := false
	for _, c := range cmds {
		m := c.(map[string]any)
		if m["name"] == "gamemode" && strings.Contains(m["args"].(string), "[player]") && m["help"].(map[string]any)["pl"] != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("gamemode hint missing: %v", cmds)
	}
	if _, ok := body["players"]; ok {
		t.Error("players present while stopped")
	}
	_, body = e.do(http.MethodGet, "/api/v1/servers/"+id, nil)
	if body["variables"].(map[string]any)["QUERY_PORT"] != nil {
		t.Error("query builtins leaked into variables")
	}

	resp, body = e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Custom"))
	cid := body["id"].(string)
	_, body = e.do(http.MethodGet, "/api/v1/servers/"+cid+"/console/hints", nil)
	if len(body["commands"].([]any)) != 0 {
		t.Errorf("custom server hints = %v", body)
	}
}

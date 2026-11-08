package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/cypek019/cypek.dev---panel/internal/auth"
	"github.com/cypek019/cypek.dev---panel/internal/shim"
)

// The test binary is reused as the shim (`<bin> shim ...`) and, with
// HOMEPANEL_FAKE_GAME=1, as the game itself.
func TestMain(m *testing.M) {
	if os.Getenv("HOMEPANEL_FAKE_GAME") == "1" {
		fmt.Println("Done (0.01s)!")
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "stop" {
				fmt.Println("bye")
				os.Exit(0)
			}
			fmt.Printf("> %s\n", line)
		}
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "shim" {
		os.Exit(shim.Main(os.Args[2:], "", slog.New(slog.NewTextHandler(os.Stderr, nil))))
	}
	os.Exit(m.Run())
}

func fakeServerBody(name string) map[string]any {
	return map[string]any{
		"name": name,
		"run": map[string]any{
			"command":    os.Args[0],
			"args":       []string{},
			"env":        map[string]string{"HOMEPANEL_FAKE_GAME": "1"},
			"stop":       map[string]any{"command": "stop", "timeoutSec": 5},
			"readyRegex": "Done",
		},
	}
}

func (e *testEnv) waitServerState(id, want string) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, body := e.do(http.MethodGet, "/api/v1/servers/"+id, nil)
		st, _ := body["status"].(map[string]any)
		if st != nil && st["state"] == want {
			return body
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("server %s never reached %s: %v", id, want, body)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (e *testEnv) loginAs(username string) {
	e.t.Helper()
	jar, _ := cookiejar.New(nil)
	e.client = &http.Client{Jar: jar}
	e.csrf = ""
	resp, body := e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": username, "password": "password123"})
	if resp.StatusCode != 200 {
		e.t.Fatalf("login %s: %d %v", username, resp.StatusCode, body)
	}
	e.csrf = body["csrfToken"].(string)
}

func TestServerLifecycleAPI(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")

	// Validation.
	resp, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "", "run": map[string]any{"command": "x"}})
	if resp.StatusCode != 422 || body["error"].(map[string]any)["field"] != "name" {
		t.Fatalf("empty name: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "x", "run": map[string]any{"command": ""}})
	if resp.StatusCode != 422 || body["error"].(map[string]any)["field"] != "run.command" {
		t.Fatalf("empty command: %d %v", resp.StatusCode, body)
	}
	bad := fakeServerBody("bad")
	bad["limits"] = map[string]any{"memoryMb": -1, "cpuPercent": 0}
	resp, body = e.do(http.MethodPost, "/api/v1/servers", bad)
	if resp.StatusCode != 422 || body["error"].(map[string]any)["field"] != "limits.memoryMb" {
		t.Fatalf("bad limits: %d %v", resp.StatusCode, body)
	}

	// Create.
	resp, body = e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Test server"))
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	if body["status"].(map[string]any)["state"] != "stopped" || body["name"] != "Test server" {
		t.Errorf("created = %v", body)
	}
	if _, err := os.Stat(filepath.Join(e.cfg.ServersDir(), id, shim.MetaDir, shim.RunFile)); err != nil {
		t.Errorf("run.json missing: %v", err)
	}
	resp, body = e.do(http.MethodGet, "/api/v1/servers", nil)
	if items := body["items"].([]any); resp.StatusCode != 200 || len(items) != 1 {
		t.Fatalf("list: %d %v", resp.StatusCode, body)
	}

	// Power.
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "stop"})
	if resp.StatusCode != 409 || errCode(body) != CodeConflict {
		t.Fatalf("stop while stopped: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "start"})
	if resp.StatusCode != 200 {
		t.Fatalf("start: %d %v", resp.StatusCode, body)
	}
	e.waitServerState(id, "running")
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "start"})
	if resp.StatusCode != 409 {
		t.Fatalf("double start: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodDelete, "/api/v1/servers/"+id, nil)
	if resp.StatusCode != 409 {
		t.Fatalf("delete while running: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "bogus"})
	if resp.StatusCode != 422 {
		t.Fatalf("bogus action: %d %v", resp.StatusCode, body)
	}

	// Console over WebSocket.
	out := e.consoleSession(id, "hello from test\n", true)
	if !strings.Contains(out, "Done") || !strings.Contains(out, "> hello from test") {
		t.Errorf("console output = %q", out)
	}

	// Patch.
	resp, body = e.do(http.MethodPatch, "/api/v1/servers/"+id, map[string]any{"name": "Renamed", "autostart": true, "limits": map[string]any{"memoryMb": 512, "cpuPercent": 50}})
	if resp.StatusCode != 200 || body["name"] != "Renamed" || body["autostart"] != true {
		t.Fatalf("patch: %d %v", resp.StatusCode, body)
	}

	// Stop + delete.
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "stop"})
	if resp.StatusCode != 200 {
		t.Fatalf("stop: %d %v", resp.StatusCode, body)
	}
	e.waitServerState(id, "stopped")
	resp, _ = e.do(http.MethodDelete, "/api/v1/servers/"+id, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(e.cfg.ServersDir(), id)); !os.IsNotExist(err) {
		t.Error("server dir not removed")
	}
	resp, _ = e.do(http.MethodGet, "/api/v1/servers/"+id, nil)
	if resp.StatusCode != 404 {
		t.Errorf("get after delete: %d", resp.StatusCode)
	}

	// Audit captured the lifecycle.
	_, body = e.do(http.MethodGet, "/api/v1/audit", nil)
	var actions []string
	for _, it := range body["items"].([]any) {
		actions = append(actions, it.(map[string]any)["action"].(string))
	}
	joined := strings.Join(actions, ",")
	for _, want := range []string{"server.create", "server.start", "console.input", "server.update", "server.stop", "server.delete"} {
		if !strings.Contains(joined, want) {
			t.Errorf("audit missing %s: %v", want, actions)
		}
	}
}

// consoleSession connects to the console WS, optionally sends a command, and
// returns the output collected within a short window.
func (e *testEnv) consoleSession(id, command string, expectEcho bool) string {
	e.t.Helper()
	u, _ := url.Parse(e.srv.URL)
	var cookieHeader []string
	for _, c := range e.client.Jar.Cookies(u) {
		cookieHeader = append(cookieHeader, c.Name+"="+c.Value)
	}
	wsURL := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/api/v1/servers/" + id + "/console"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {strings.Join(cookieHeader, "; ")}}})
	if err != nil {
		e.t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	var out strings.Builder
	sent := false
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			e.t.Fatalf("ws read: %v (output so far %q)", err, out.String())
		}
		var m consoleMsg
		_ = json.Unmarshal(data, &m)
		switch m.Type {
		case "hello":
			if command != "" && !sent {
				b, _ := json.Marshal(consoleMsg{Type: "input", Data: command})
				_ = conn.Write(ctx, websocket.MessageText, b)
				sent = true
			}
		case "output":
			out.WriteString(m.Data)
		case "error":
			if !expectEcho {
				return "error:" + m.Message
			}
			e.t.Fatalf("console error: %s", m.Message)
		}
		if expectEcho && command != "" && strings.Contains(out.String(), "> "+strings.TrimSpace(command)) {
			return out.String()
		}
		if command == "" && strings.Contains(out.String(), "Done") {
			return out.String()
		}
	}
}

func TestServerPermissionMatrix(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	ctx := context.Background()
	plain, err := e.store.CreateUser(ctx, "plain", "password123", auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	resp, body := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Shared"))
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)

	e.loginAs("plain")
	resp, body = e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Nope"))
	if resp.StatusCode != 403 {
		t.Errorf("user create: %d", resp.StatusCode)
	}
	_, body = e.do(http.MethodGet, "/api/v1/servers", nil)
	if len(body["items"].([]any)) != 0 {
		t.Errorf("user sees servers without grants: %v", body)
	}
	resp, _ = e.do(http.MethodGet, "/api/v1/servers/"+id, nil)
	if resp.StatusCode != 403 {
		t.Errorf("user get without grant: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodGet, "/api/v1/servers/zzzzzzzz", nil)
	if resp.StatusCode != 403 && resp.StatusCode != 404 {
		t.Errorf("unknown server as user: %d", resp.StatusCode)
	}

	if err := e.store.GrantPermissions(ctx, id, plain.ID, []auth.Permission{auth.PermConsoleRead}); err != nil {
		t.Fatal(err)
	}
	resp, _ = e.do(http.MethodGet, "/api/v1/servers/"+id, nil)
	if resp.StatusCode != 200 {
		t.Errorf("user get with console.read: %d", resp.StatusCode)
	}
	_, body = e.do(http.MethodGet, "/api/v1/servers", nil)
	if len(body["items"].([]any)) != 1 {
		t.Errorf("user list with grant: %v", body)
	}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/servers/" + id + "/power", map[string]string{"action": "start"}},
		{http.MethodPatch, "/api/v1/servers/" + id, map[string]any{"name": "x"}},
		{http.MethodDelete, "/api/v1/servers/" + id, nil},
	} {
		resp, _ = e.do(c.method, c.path, c.body)
		if resp.StatusCode != 403 {
			t.Errorf("%s %s with console.read only: %d", c.method, c.path, resp.StatusCode)
		}
	}

	// Grant power: the user can start it and read the console, but not type.
	if err := e.store.GrantPermissions(ctx, id, plain.ID, []auth.Permission{auth.PermConsoleRead, auth.PermPower}); err != nil {
		t.Fatal(err)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "start"})
	if resp.StatusCode != 200 {
		t.Fatalf("user start with power: %d %v", resp.StatusCode, body)
	}
	e.waitServerState(id, "running")
	if out := e.consoleSession(id, "nope\n", false); !strings.HasPrefix(out, "error:") {
		t.Errorf("console.write not enforced: %q", out)
	}
	if out := e.consoleSession(id, "", false); !strings.Contains(out, "Done") {
		t.Errorf("console.read backlog = %q", out)
	}

	// Owner cleans up.
	e.loginAs("owner")
	resp, _ = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "kill"})
	if resp.StatusCode != 200 {
		t.Fatalf("kill: %d", resp.StatusCode)
	}
	e.waitServerState(id, "stopped")
	resp, _ = e.do(http.MethodDelete, "/api/v1/servers/"+id, nil)
	if resp.StatusCode != 204 {
		t.Errorf("delete: %d", resp.StatusCode)
	}
}

func TestConsoleWaitsForStoppedServer(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	resp, body := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Idle"))
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	u, _ := url.Parse(e.srv.URL)
	var cookies []string
	for _, c := range e.client.Jar.Cookies(u) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/api/v1/servers/"+id+"/console",
		&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {strings.Join(cookies, "; ")}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m consoleMsg
	_ = json.Unmarshal(data, &m)
	if m.Type != "state" || m.State != "stopped" {
		t.Fatalf("first frame = %s", data)
	}
	// Start the server; the open socket should attach by itself.
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "start"})
	if resp.StatusCode != 200 {
		t.Fatalf("start: %d %v", resp.StatusCode, body)
	}
	var out strings.Builder
	for !strings.Contains(out.String(), "Done") {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (out %q)", err, out.String())
		}
		_ = json.Unmarshal(data, &m)
		if m.Type == "output" {
			out.WriteString(m.Data)
		}
	}
	_, _ = e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "kill"})
	e.waitServerState(id, "stopped")
}

func TestStartRejectsMissingCommand(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	body := fakeServerBody("Typo")
	body["run"].(map[string]any)["command"] = "bin/cat"
	resp, created := e.do(http.MethodPost, "/api/v1/servers", body)
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, created)
	}
	id := created["id"].(string)
	resp, out := e.do(http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "start"})
	if resp.StatusCode != 422 || errCode(out) != CodeCommandNotFound || out["error"].(map[string]any)["field"] != "run.command" {
		t.Fatalf("start with bad command: %d %v", resp.StatusCode, out)
	}
	if st := e.waitServerState(id, "stopped"); st["status"].(map[string]any)["state"] != "stopped" {
		t.Errorf("server should still be stopped: %v", st)
	}
}

func TestEventsWebSocket(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	resp, body := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Ev"))
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	_, stats := e.do(http.MethodGet, "/api/v1/system/stats", nil)
	if stats["memTotal"] == nil || stats["cpuCount"] == nil {
		t.Errorf("system stats = %v", stats)
	}
	u, _ := url.Parse(e.srv.URL)
	var cookies []string
	for _, c := range e.client.Jar.Cookies(u) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/api/v1/events",
		&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {strings.Join(cookies, "; ")}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m["type"] != "snapshot" || m["host"].(map[string]any)["memTotal"] == nil {
		t.Fatalf("first message = %s", data)
	}
	found := false
	for _, s := range m["servers"].([]any) {
		if s.(map[string]any)["id"] == id {
			found = true
		}
	}
	if !found {
		t.Errorf("snapshot missing server: %s", data)
	}
	// Unauthenticated sockets are refused before the upgrade.
	_, resp2, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(e.srv.URL, "http")+"/api/v1/events", nil)
	if err == nil || resp2 == nil || resp2.StatusCode != 401 {
		t.Errorf("anonymous events: err=%v status=%v", err, resp2)
	}
}

func TestSchedulesAPI(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	_, body := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Sched"))
	id := body["id"].(string)
	base := "/api/v1/servers/" + id + "/schedules"

	resp, body := e.do(http.MethodGet, "/api/v1/system/cron/preview?expr=0+4+*+*+*", nil)
	if resp.StatusCode != 200 || len(body["next"].([]any)) != 5 || body["timezone"] == "" {
		t.Fatalf("preview: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodGet, "/api/v1/system/cron/preview?expr=nope", nil)
	if resp.StatusCode != 422 || errCode(body) != CodeInvalidCron {
		t.Errorf("bad preview: %d %v", resp.StatusCode, body)
	}

	resp, body = e.do(http.MethodPost, base+"/", map[string]any{"name": "x", "cron": "bad", "actions": []map[string]any{{"type": "restart"}}})
	if resp.StatusCode != 422 || body["error"].(map[string]any)["field"] != "cron" {
		t.Errorf("bad cron: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, base+"/", map[string]any{"name": "Nightly", "cron": "0 4 * * *",
		"actions": []map[string]any{{"type": "command", "text": "say hi"}, {"type": "wait", "seconds": 1}, {"type": "stop"}}, "onlyIfRunning": false})
	if resp.StatusCode != 201 || body["nextRunAt"] == "" || body["enabled"] != true {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	sid := strconv.Itoa(int(body["id"].(float64)))
	resp, body = e.do(http.MethodGet, base+"/", nil)
	if len(body["items"].([]any)) != 1 || body["timezone"] == nil {
		t.Errorf("list: %v", body)
	}
	resp, body = e.do(http.MethodPatch, base+"/"+sid, map[string]any{"enabled": false, "name": "Paused"})
	if resp.StatusCode != 200 || body["name"] != "Paused" || body["enabled"] != false {
		t.Errorf("patch: %d %v", resp.StatusCode, body)
	}

	// Run now against the stopped fake server: command fails (not running) -> failed run recorded.
	resp, _ = e.do(http.MethodPost, base+"/"+sid+"/run", nil)
	if resp.StatusCode != 202 {
		t.Fatalf("run: %d", resp.StatusCode)
	}
	deadline := time.Now().Add(10 * time.Second)
	var runs []any
	for time.Now().Before(deadline) {
		_, rb := e.do(http.MethodGet, base+"/"+sid+"/runs", nil)
		runs = rb["items"].([]any)
		if len(runs) == 1 && runs[0].(map[string]any)["result"] != "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(runs) != 1 || runs[0].(map[string]any)["result"] != "failed" || runs[0].(map[string]any)["trigger"] != "manual" {
		t.Errorf("runs = %v", runs)
	}

	// Permission: plain user without `schedules` gets 403; with it, 200.
	plain, _ := e.store.CreateUser(t.Context(), "plain", "password123", auth.RoleUser)
	_ = e.store.GrantPermissions(t.Context(), id, plain.ID, []auth.Permission{auth.PermConsoleRead})
	e.loginAs("plain")
	if resp, _ := e.do(http.MethodGet, base+"/", nil); resp.StatusCode != 403 {
		t.Errorf("schedules without perm: %d", resp.StatusCode)
	}
	_ = e.store.GrantPermissions(t.Context(), id, plain.ID, []auth.Permission{auth.PermConsoleRead, auth.PermSchedules})
	if resp, _ := e.do(http.MethodGet, base+"/", nil); resp.StatusCode != 200 {
		t.Errorf("schedules with perm: %d", resp.StatusCode)
	}
	e.loginAs("owner")
	if resp, _ := e.do(http.MethodDelete, base+"/"+sid, nil); resp.StatusCode != 204 {
		t.Errorf("delete: %d", resp.StatusCode)
	}
	if resp, _ := e.do(http.MethodGet, base+"/"+sid+"/runs", nil); resp.StatusCode != 404 {
		t.Errorf("runs after delete: %d", resp.StatusCode)
	}
}

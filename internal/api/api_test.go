package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cypek019/cypek.dev---panel/internal/audit"
	"github.com/cypek019/cypek.dev---panel/internal/auth"
	"github.com/cypek019/cypek.dev---panel/internal/backups"
	"github.com/cypek019/cypek.dev---panel/internal/config"
	"github.com/cypek019/cypek.dev---panel/internal/database"
	"github.com/cypek019/cypek.dev---panel/internal/db"
	"github.com/cypek019/cypek.dev---panel/internal/installer"
	"github.com/cypek019/cypek.dev---panel/internal/providers"
	"github.com/cypek019/cypek.dev---panel/internal/runtimes"
	"github.com/cypek019/cypek.dev---panel/internal/scheduler"
	"github.com/cypek019/cypek.dev---panel/internal/secrets"
	"github.com/cypek019/cypek.dev---panel/internal/servers"
	"github.com/cypek019/cypek.dev---panel/internal/supervisor"
	"github.com/cypek019/cypek.dev---panel/internal/templates"
	builtinTemplates "github.com/cypek019/cypek.dev---panel/templates"
)

type testEnv struct {
	t        *testing.T
	srv      *httptest.Server
	client   *http.Client
	store    *auth.Store
	cfg      *config.Config
	csrf     string
	paper    *providers.Paper
	adoptium *providers.Adoptium
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.Listen = "127.0.0.1:0"
	sqlDB, err := db.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := db.Migrate(context.Background(), sqlDB, "", log); err != nil {
		t.Fatal(err)
	}
	store := auth.NewStore(sqlDB)
	// Short run dir: unix socket paths are limited to ~108 bytes.
	runDir, err := os.MkdirTemp("", "hp-run")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runDir) })
	layout := supervisor.Layout{ServersDir: cfg.ServersDir(), RunDir: runDir}
	sup := supervisor.NewLocal(layout, os.Args[0], log)
	serverStore := servers.NewStore(sqlDB, cfg.ServersDir())
	dist := fstest.MapFS{
		"index.html":    {Data: []byte("<html>spa</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	registry, err := templates.Load(builtinTemplates.Builtin, filepath.Join(dir, "templates"))
	if err != nil {
		t.Fatal(err)
	}
	paper := &providers.Paper{BaseURL: "https://127.0.0.1:1", Project: "paper"}
	adoptium := &providers.Adoptium{BaseURL: "https://127.0.0.1:1"}
	inst := installer.New(installer.Deps{Paper: paper, Java: &runtimes.Java{Dir: filepath.Join(dir, "runtimes"), Adoptium: adoptium}, Log: log})
	box, _ := secrets.New(bytes.Repeat([]byte{1}, 32))
	h, err := New(Deps{Config: cfg, Store: store, Audit: audit.New(sqlDB, log), Log: log, Version: "test", Dist: dist, Servers: serverStore, Sup: sup,
		Templates: registry, Installer: inst, Paper: paper, Secrets: box, Schedules: scheduler.NewStore(sqlDB), Backups: backups.NewStore(sqlDB), DBRecords: database.NewStore(sqlDB), Databases: database.NewManager(filepath.Join(dir, "database.json"))})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &testEnv{t: t, srv: srv, client: &http.Client{Jar: jar}, store: store, cfg: cfg, paper: paper, adoptium: adoptium}
}

// do sends a JSON request, attaching the CSRF header when known.
func (e *testEnv) do(method, path string, body any, headers ...string) (*http.Response, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.csrf != "" {
		req.Header.Set(csrfHeader, e.csrf)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(raw, &out); err != nil {
			e.t.Fatalf("bad JSON from %s %s: %v: %s", method, path, err, raw)
		}
	}
	return resp, out
}

func errCode(body map[string]any) string {
	if e, ok := body["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func (e *testEnv) setupToken() string {
	tok, _, err := e.store.EnsureSetupToken(context.Background(), e.cfg.DataDir)
	if err != nil {
		e.t.Fatal(err)
	}
	return tok
}

func (e *testEnv) completeSetup(username, password string) map[string]any {
	e.t.Helper()
	resp, body := e.do(http.MethodPost, "/api/v1/setup", map[string]string{"token": e.setupToken(), "username": username, "password": password})
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("setup: %d %v", resp.StatusCode, body)
	}
	e.csrf = body["csrfToken"].(string)
	return body
}

func TestSetupFlow(t *testing.T) {
	e := newEnv(t)

	resp, body := e.do(http.MethodGet, "/api/v1/setup/status", nil)
	if resp.StatusCode != 200 || body["setupRequired"] != true {
		t.Fatalf("status: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodGet, "/api/v1/auth/me", nil)
	if resp.StatusCode != 401 || errCode(body) != CodeUnauthorized {
		t.Fatalf("me before login: %d %v", resp.StatusCode, body)
	}

	resp, body = e.do(http.MethodPost, "/api/v1/setup", map[string]string{"token": "wrong", "username": "owner", "password": "password123"})
	if resp.StatusCode != 403 || errCode(body) != CodeInvalidSetupToken {
		t.Fatalf("wrong token: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/setup", map[string]string{"token": e.setupToken(), "username": "owner", "password": "short"})
	if resp.StatusCode != 422 || errCode(body) != CodeWeakPassword {
		t.Fatalf("weak password: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/setup", map[string]string{"token": e.setupToken(), "username": "owner", "password": "password123", "extra": "x"})
	if resp.StatusCode != 400 || errCode(body) != CodeBadRequest {
		t.Fatalf("unknown field: %d %v", resp.StatusCode, body)
	}

	body = e.completeSetup("owner", "password123")
	user := body["user"].(map[string]any)
	if user["role"] != "owner" || user["username"] != "owner" {
		t.Errorf("user = %v", user)
	}
	if _, leaked := user["passwordHash"]; leaked {
		t.Error("password hash leaked")
	}
	if _, leaked := user["PasswordHash"]; leaked {
		t.Error("password hash leaked")
	}

	resp, body = e.do(http.MethodGet, "/api/v1/setup/status", nil)
	if body["setupRequired"] != false {
		t.Fatalf("status after setup: %v", body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/setup", map[string]string{"token": "x", "username": "other", "password": "password123"})
	if resp.StatusCode != 409 || errCode(body) != CodeSetupCompleted {
		t.Fatalf("second setup: %d %v", resp.StatusCode, body)
	}

	resp, body = e.do(http.MethodGet, "/api/v1/auth/me", nil)
	if resp.StatusCode != 200 || body["user"].(map[string]any)["username"] != "owner" {
		t.Fatalf("me after setup: %d %v", resp.StatusCode, body)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Error("api response cacheable")
	}
}

func TestLoginLogoutAndCSRF(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")

	// Logout without CSRF header must fail; with it, must succeed.
	saved := e.csrf
	e.csrf = ""
	resp, body := e.do(http.MethodPost, "/api/v1/auth/logout", nil)
	if resp.StatusCode != 403 || errCode(body) != CodeCSRF {
		t.Fatalf("logout w/o csrf: %d %v", resp.StatusCode, body)
	}
	e.csrf = "bogus"
	resp, _ = e.do(http.MethodPost, "/api/v1/auth/logout", nil)
	if resp.StatusCode != 403 {
		t.Fatalf("logout bad csrf: %d", resp.StatusCode)
	}
	e.csrf = saved
	resp, _ = e.do(http.MethodPost, "/api/v1/auth/logout", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodGet, "/api/v1/auth/me", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("me after logout: %d", resp.StatusCode)
	}

	// Login.
	e.csrf = ""
	resp, body = e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "owner", "password": "nope"})
	if resp.StatusCode != 401 || errCode(body) != CodeInvalidCredentials {
		t.Fatalf("bad login: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "ghost", "password": "password123"})
	if resp.StatusCode != 401 || errCode(body) != CodeInvalidCredentials {
		t.Fatalf("unknown user login: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "OWNER", "password": "password123"})
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d %v", resp.StatusCode, body)
	}
	e.csrf = body["csrfToken"].(string)
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie attrs wrong: %+v", cookie)
	}

	// Cross-site marked requests and non-JSON bodies are rejected.
	resp, body = e.do(http.MethodPost, "/api/v1/auth/logout", nil, "Sec-Fetch-Site", "cross-site")
	if resp.StatusCode != 403 || errCode(body) != CodeCSRF {
		t.Fatalf("cross-site: %d %v", resp.StatusCode, body)
	}
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v1/auth/login", strings.NewReader("username=owner&password=password123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r2, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("form login: %d", r2.StatusCode)
	}

	// Audit log is admin+ only, and recorded logins.
	resp, body = e.do(http.MethodGet, "/api/v1/audit", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("audit: %d %v", resp.StatusCode, body)
	}
	items := body["items"].([]any)
	var actions []string
	for _, it := range items {
		actions = append(actions, it.(map[string]any)["action"].(string))
	}
	joined := strings.Join(actions, ",")
	for _, want := range []string{"auth.setup", "auth.logout", "auth.login_failed", "auth.login"} {
		if !strings.Contains(joined, want) {
			t.Errorf("audit missing %s in %v", want, actions)
		}
	}
}

func TestRoleMatrix(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	ctx := context.Background()
	if _, err := e.store.CreateUser(ctx, "plain", "password123", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.CreateUser(ctx, "adm", "password123", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		user string
		path string
		want int
	}{
		{"plain", "/api/v1/auth/me", 200},
		{"plain", "/api/v1/system/info", 200},
		{"plain", "/api/v1/audit", 403},
		{"adm", "/api/v1/audit", 200},
		{"owner", "/api/v1/audit", 200},
	}
	for _, c := range cases {
		jar, _ := cookiejar.New(nil)
		e.client = &http.Client{Jar: jar}
		e.csrf = ""
		resp, body := e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": c.user, "password": "password123"})
		if resp.StatusCode != 200 {
			t.Fatalf("login %s: %d %v", c.user, resp.StatusCode, body)
		}
		e.csrf = body["csrfToken"].(string)
		resp, body = e.do(http.MethodGet, c.path, nil)
		if resp.StatusCode != c.want {
			t.Errorf("%s GET %s = %d (%v), want %d", c.user, c.path, resp.StatusCode, body, c.want)
		}
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	e.csrf = ""
	var last int
	for i := 0; i < 10; i++ {
		resp, _ := e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "owner", "password": "wrong"})
		last = resp.StatusCode
		if last == 429 {
			if resp.Header.Get("Retry-After") == "" {
				t.Error("missing Retry-After")
			}
			break
		}
	}
	if last != 429 {
		t.Fatalf("never rate limited, last status %d", last)
	}
	// The right password is also blocked while limited (per-account lock).
	resp, _ := e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "owner", "password": "password123"})
	if resp.StatusCode != 429 {
		t.Errorf("locked account accepted login: %d", resp.StatusCode)
	}
}

func TestStaticAndErrors(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/login", "/servers/abc/console"} {
		resp, err := e.client.Get(e.srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(b), "spa") {
			t.Errorf("GET %s = %d %q", p, resp.StatusCode, b)
		}
	}
	resp, err := e.client.Get(e.srv.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("assets cache-control = %q", resp.Header.Get("Cache-Control"))
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("security headers missing")
	}

	resp, body := e.do(http.MethodGet, "/api/v1/nope", nil)
	if resp.StatusCode != 404 || errCode(body) != CodeNotFound {
		t.Errorf("unknown api: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodGet, "/api/v1/version", nil)
	if resp.StatusCode != 200 || body["version"] != "test" {
		t.Errorf("version: %d %v", resp.StatusCode, body)
	}

	// Oversize body.
	big := strings.Repeat("a", maxJSONBody+10)
	resp, body = e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": big, "password": "x"})
	if resp.StatusCode != http.StatusRequestEntityTooLarge || errCode(body) != CodePayloadTooLarge {
		t.Errorf("oversize: %d %v", resp.StatusCode, body)
	}
}

func TestFrontendMissing(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	sqlDB, _ := db.OpenMemory()
	defer sqlDB.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_ = db.Migrate(context.Background(), sqlDB, "", log)
	h, err := New(Deps{Config: cfg, Store: auth.NewStore(sqlDB), Audit: audit.New(sqlDB, log), Log: log, Dist: fstest.MapFS{".gitkeep": {}}})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), CodeFrontendMissing) {
		t.Errorf("missing frontend: %d %s", rec.Code, rec.Body.String())
	}
}

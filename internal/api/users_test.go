package api

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

func TestUsersAndGrantsAPI(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")

	resp, body := e.do(http.MethodPost, "/api/v1/users/", map[string]string{"username": "adm", "password": "password123", "role": "admin"})
	if resp.StatusCode != 201 || body["role"] != "admin" {
		t.Fatalf("create admin: %d %v", resp.StatusCode, body)
	}
	admID := strconv.Itoa(int(body["id"].(float64)))
	resp, body = e.do(http.MethodPost, "/api/v1/users/", map[string]string{"username": "plain", "password": "password123"})
	if resp.StatusCode != 201 || body["role"] != "user" {
		t.Fatalf("create user: %d %v", resp.StatusCode, body)
	}
	plainID := strconv.Itoa(int(body["id"].(float64)))
	if _, leaked := body["passwordHash"]; leaked {
		t.Error("hash leaked")
	}
	resp, body = e.do(http.MethodPost, "/api/v1/users/", map[string]string{"username": "plain", "password": "password123"})
	if resp.StatusCode != 422 || errCode(body) != CodeUsernameTaken {
		t.Errorf("duplicate: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/users/", map[string]string{"username": "boss", "password": "password123", "role": "owner"})
	if resp.StatusCode != 422 {
		t.Errorf("owner role: %d %v", resp.StatusCode, body)
	}
	_, body = e.do(http.MethodGet, "/api/v1/users/", nil)
	if len(body["items"].([]any)) != 3 {
		t.Errorf("list = %v", body)
	}

	_, me := e.do(http.MethodGet, "/api/v1/auth/me", nil)
	ownerID := strconv.Itoa(int(me["user"].(map[string]any)["id"].(float64)))
	resp, body = e.do(http.MethodPatch, "/api/v1/users/"+ownerID, map[string]any{"disabled": true})
	if resp.StatusCode != 403 || errCode(body) != CodeOwnerImmutable {
		t.Errorf("owner patch: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(http.MethodDelete, "/api/v1/users/"+ownerID, nil)
	if resp.StatusCode != 403 {
		t.Errorf("owner delete: %d", resp.StatusCode)
	}

	_, srvBody := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("G"))
	sid := srvBody["id"].(string)
	resp, body = e.do(http.MethodPut, "/api/v1/servers/"+sid+"/users/"+plainID, map[string]any{"permissions": []string{"console.read", "power", "bogus"}})
	if resp.StatusCode != 422 {
		t.Errorf("bogus perm: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPut, "/api/v1/servers/"+sid+"/users/"+plainID, map[string]any{"permissions": []string{"console.read", "power"}})
	if resp.StatusCode != 200 {
		t.Fatalf("grant: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPut, "/api/v1/servers/"+sid+"/users/"+admID, map[string]any{"permissions": []string{"power"}})
	if resp.StatusCode != 409 {
		t.Errorf("grant to admin: %d %v", resp.StatusCode, body)
	}
	_, body = e.do(http.MethodGet, "/api/v1/servers/"+sid+"/users/", nil)
	items := body["items"].([]any)
	if len(items) != 1 || len(items[0].(map[string]any)["permissions"].([]any)) != 2 {
		t.Errorf("grants = %v", body)
	}

	e.loginAs("adm")
	resp, _ = e.do(http.MethodPost, "/api/v1/users/", map[string]string{"username": "adm2", "password": "password123", "role": "admin"})
	if resp.StatusCode != 403 {
		t.Errorf("admin creates admin: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodPatch, "/api/v1/users/"+plainID, map[string]any{"disabled": true})
	if resp.StatusCode != 200 {
		t.Errorf("admin disables user: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodPatch, "/api/v1/users/"+admID, map[string]any{"disabled": true})
	if resp.StatusCode != 403 {
		t.Errorf("admin disables self: %d", resp.StatusCode)
	}

	resp, _ = e.do(http.MethodPatch, "/api/v1/users/"+plainID, map[string]any{"disabled": false})
	if resp.StatusCode != 200 {
		t.Fatalf("re-enable: %d", resp.StatusCode)
	}

	e.loginAs("plain")
	resp, _ = e.do(http.MethodGet, "/api/v1/users/", nil)
	if resp.StatusCode != 403 {
		t.Errorf("user lists users: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodGet, "/api/v1/servers/"+sid+"/users/", nil)
	if resp.StatusCode != 403 {
		t.Errorf("user lists grants: %d", resp.StatusCode)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/auth/password", map[string]string{"current": "wrong", "new": "newpassword123"})
	if resp.StatusCode != 422 || errCode(body) != CodeWrongPassword {
		t.Errorf("wrong current: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/auth/password", map[string]string{"current": "password123", "new": "newpassword123"})
	if resp.StatusCode != 200 {
		t.Fatalf("change password: %d %v", resp.StatusCode, body)
	}
	e.csrf = body["csrfToken"].(string)
	if resp, _ := e.do(http.MethodGet, "/api/v1/auth/me", nil); resp.StatusCode != 200 {
		t.Error("session not re-issued after password change")
	}
	if _, err := e.store.Authenticate(t.Context(), "plain", "newpassword123"); err != nil {
		t.Errorf("new password: %v", err)
	}

	e.loginAs("owner")
	resp, _ = e.do(http.MethodPut, "/api/v1/servers/"+sid+"/users/"+plainID, map[string]any{"permissions": []string{}})
	if resp.StatusCode != 204 {
		t.Errorf("revoke: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodDelete, "/api/v1/users/"+plainID, nil)
	if resp.StatusCode != 204 {
		t.Errorf("delete user: %d", resp.StatusCode)
	}
	_ = auth.RoleUser
}

func TestTOTPFlow(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")

	_, st := e.do(http.MethodGet, "/api/v1/auth/totp", nil)
	if st["enabled"] != false || st["policy"] != "none" {
		t.Fatalf("status = %v", st)
	}

	resp, body := e.do(http.MethodPut, "/api/v1/settings/security", map[string]string{"requireTotp": "admins"})
	if resp.StatusCode != 409 {
		t.Errorf("policy before enrol: %d %v", resp.StatusCode, body)
	}
	resp, setup := e.do(http.MethodPost, "/api/v1/auth/totp/setup", nil)
	if resp.StatusCode != 200 || setup["secret"] == "" || setup["qrPng"] == "" || !strings.HasPrefix(setup["otpauthUrl"].(string), "otpauth://") {
		t.Fatalf("setup: %d %v", resp.StatusCode, setup)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/auth/totp/enable", map[string]string{"code": "000000"})
	if resp.StatusCode != 422 || errCode(body) != CodeTOTPInvalid {
		t.Errorf("bad code: %d %v", resp.StatusCode, body)
	}
	code, _ := totp.GenerateCode(setup["secret"].(string), time.Now())
	resp, body = e.do(http.MethodPost, "/api/v1/auth/totp/enable", map[string]string{"code": code})
	if resp.StatusCode != 200 || len(body["backupCodes"].([]any)) != 8 {
		t.Fatalf("enable: %d %v", resp.StatusCode, body)
	}
	backup := body["backupCodes"].([]any)[0].(string)

	jar, _ := cookiejar.New(nil)
	e.client = &http.Client{Jar: jar}
	e.csrf = ""
	resp, body = e.do(http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "owner", "password": "password123"})
	if resp.StatusCode != 200 || body["totpRequired"] != true || body["token"] == "" {
		t.Fatalf("login step 1: %d %v", resp.StatusCode, body)
	}
	token := body["token"].(string)
	if resp, _ := e.do(http.MethodGet, "/api/v1/auth/me", nil); resp.StatusCode != 401 {
		t.Error("session issued before the code")
	}
	resp, body = e.do(http.MethodPost, "/api/v1/auth/login/totp", map[string]string{"token": token, "code": "999999"})
	if resp.StatusCode != 422 {
		t.Errorf("wrong code: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/auth/login/totp", map[string]string{"token": token, "code": backup})
	if resp.StatusCode != 200 || body["csrfToken"] == nil {
		t.Fatalf("login with backup code: %d %v", resp.StatusCode, body)
	}
	e.csrf = body["csrfToken"].(string)
	resp, body = e.do(http.MethodPost, "/api/v1/auth/login/totp", map[string]string{"token": token, "code": backup})
	if resp.StatusCode != 401 {
		t.Errorf("token reuse: %d %v", resp.StatusCode, body)
	}
	_, st = e.do(http.MethodGet, "/api/v1/auth/totp", nil)
	if st["enabled"] != true || st["backupCodesLeft"].(float64) != 7 {
		t.Errorf("status after = %v", st)
	}

	resp, body = e.do(http.MethodPut, "/api/v1/settings/security", map[string]string{"requireTotp": "admins"})
	if resp.StatusCode != 200 {
		t.Fatalf("set policy: %d %v", resp.StatusCode, body)
	}
	_, _ = e.do(http.MethodPost, "/api/v1/users/", map[string]string{"username": "adm", "password": "password123", "role": "admin"})
	e.loginAs("adm")
	resp, body = e.do(http.MethodGet, "/api/v1/servers", nil)
	if resp.StatusCode != 403 || errCode(body) != CodeTOTPRequired {
		t.Errorf("admin without 2fa: %d %v", resp.StatusCode, body)
	}
	if resp, _ := e.do(http.MethodGet, "/api/v1/auth/totp", nil); resp.StatusCode != 200 {
		t.Errorf("enrolment endpoint blocked: %d", resp.StatusCode)
	}
	if resp, _ := e.do(http.MethodPost, "/api/v1/auth/totp/setup", nil); resp.StatusCode != 200 {
		t.Errorf("setup blocked: %d", resp.StatusCode)
	}
}

func TestAPIKeys(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	_, srvBody := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("K"))
	sid := srvBody["id"].(string)

	resp, body := e.do(http.MethodPost, "/api/v1/auth/api-keys", map[string]any{"name": "ci", "scope": []string{"console.read", "power"}})
	if resp.StatusCode != 201 || !strings.HasPrefix(body["token"].(string), "hp_") {
		t.Fatalf("create key: %d %v", resp.StatusCode, body)
	}
	token := body["token"].(string)
	kid := strconv.Itoa(int(body["key"].(map[string]any)["id"].(float64)))
	resp, body = e.do(http.MethodPost, "/api/v1/auth/api-keys", map[string]any{"name": "bad", "scope": []string{"nope"}})
	if resp.StatusCode != 422 {
		t.Errorf("bad scope: %d %v", resp.StatusCode, body)
	}

	bearer := func(method, path string, payload any) (*http.Response, map[string]any) {
		t.Helper()
		var rd *strings.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			rd = strings.NewReader(string(b))
		} else {
			rd = strings.NewReader("")
		}
		req, _ := http.NewRequest(method, e.srv.URL+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}
	resp, body = bearer(http.MethodGet, "/api/v1/auth/me", nil)
	if resp.StatusCode != 200 || body["user"].(map[string]any)["username"] != "owner" {
		t.Fatalf("bearer me: %d %v", resp.StatusCode, body)
	}
	resp, body = bearer(http.MethodGet, "/api/v1/servers/"+sid, nil)
	if resp.StatusCode != 200 {
		t.Errorf("bearer get server: %d %v", resp.StatusCode, body)
	}
	resp, body = bearer(http.MethodPatch, "/api/v1/servers/"+sid, map[string]any{"name": "renamed"})
	if resp.StatusCode != 403 {
		t.Errorf("out-of-scope settings: %d %v", resp.StatusCode, body)
	}
	resp, body = bearer(http.MethodPost, "/api/v1/servers/"+sid+"/power", map[string]string{"action": "stop"})
	if resp.StatusCode != 409 {
		t.Errorf("in-scope power: %d %v", resp.StatusCode, body)
	}
	resp, _ = bearer(http.MethodPost, "/api/v1/auth/api-keys", map[string]any{"name": "nested"})
	if resp.StatusCode != 403 {
		t.Errorf("key creates key: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer hp_garbage")
	if resp, _ := (&http.Client{}).Do(req); resp.StatusCode != 401 {
		t.Errorf("garbage key: %d", resp.StatusCode)
	}
	_, body = e.do(http.MethodGet, "/api/v1/auth/api-keys", nil)
	items := body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["lastUsedAt"] == "" {
		t.Errorf("keys = %v", items)
	}
	resp, _ = e.do(http.MethodDelete, "/api/v1/auth/api-keys/"+kid, nil)
	if resp.StatusCode != 204 {
		t.Errorf("delete key: %d", resp.StatusCode)
	}
	resp, _ = bearer(http.MethodGet, "/api/v1/auth/me", nil)
	if resp.StatusCode != 401 {
		t.Errorf("deleted key still works: %d", resp.StatusCode)
	}
}

func TestDatabasesNotInstalled(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	_, body := e.do(http.MethodGet, "/api/v1/system/database", nil)
	if body["status"].(map[string]any)["installed"] != false {
		t.Errorf("status = %v", body)
	}
	_, srv := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("D"))
	id := srv["id"].(string)
	resp, body := e.do(http.MethodPost, "/api/v1/servers/"+id+"/databases/", map[string]string{"name": "esx"})
	if resp.StatusCode != 409 || errCode(body) != CodeDBNotInstalled {
		t.Errorf("create without mariadb: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/servers/"+id+"/databases/", map[string]string{"name": "bad name!"})
	if resp.StatusCode != 422 {
		t.Errorf("bad name: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPost, "/api/v1/system/database/install", nil)
	if resp.StatusCode != 409 || errCode(body) != CodeNoSystemd {
		t.Errorf("install with local supervisor: %d %v", resp.StatusCode, body)
	}
	_, body = e.do(http.MethodGet, "/api/v1/servers/"+id+"/databases/", nil)
	if len(body["items"].([]any)) != 0 {
		t.Errorf("list = %v", body)
	}
}

func TestRootLoginEndpoints(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	_, body := e.do(http.MethodGet, "/api/v1/system/database/root/", nil)
	if body["enabled"] != false {
		t.Errorf("initial = %v", body)
	}
	resp, body := e.do(http.MethodPut, "/api/v1/system/database/root/", map[string]string{"password": "bad pass"})
	if resp.StatusCode != 422 {
		t.Errorf("bad password: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPut, "/api/v1/system/database/root/", map[string]string{"password": "GoodPassword123"})
	if resp.StatusCode != 409 || errCode(body) != CodeDBNotInstalled {
		t.Errorf("without mariadb: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(http.MethodPost, "/api/v1/system/database/root/reveal", nil)
	if resp.StatusCode != 404 {
		t.Errorf("reveal when disabled: %d", resp.StatusCode)
	}
	_, _ = e.do(http.MethodPost, "/api/v1/users/", map[string]string{"username": "adm", "password": "password123", "role": "admin"})
	e.loginAs("adm")
	if resp, _ := e.do(http.MethodGet, "/api/v1/system/database/root/", nil); resp.StatusCode != 403 {
		t.Errorf("admin access: %d", resp.StatusCode)
	}
}

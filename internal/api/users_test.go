package api

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/cypek019/cypek.dev---panel/internal/auth"
)

func TestUsersAndGrantsAPI(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")

	// Owner creates an admin and a user.
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
	// Owner is immutable; self-role change refused.
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

	// Server grants.
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

	// Admin: may manage users but not other admins or the owner; cannot create admins.
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
	// Disabled user cannot log in; re-enable.
	resp, _ = e.do(http.MethodPatch, "/api/v1/users/"+plainID, map[string]any{"disabled": false})
	if resp.StatusCode != 200 {
		t.Fatalf("re-enable: %d", resp.StatusCode)
	}

	// Plain user: no access to user management, can change own password.
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

	// Owner removes the grant and deletes the user.
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

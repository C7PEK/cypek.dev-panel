package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cypek019/cypek.dev---panel/internal/auth"
)

func (e *testEnv) raw(method, path, contentType string, body io.Reader) *http.Response {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if e.csrf != "" {
		req.Header.Set(csrfHeader, e.csrf)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	return resp
}

func TestFilesAPI(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	_, body := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Files"))
	id := body["id"].(string)
	base := "/api/v1/servers/" + id + "/files"
	dir := filepath.Join(e.cfg.ServersDir(), id)

	// Write + read + list.
	resp := e.raw(http.MethodPut, base+"/content?path=plugins/config.yml", "text/plain", strings.NewReader("a: 1\n"))
	if resp.StatusCode != 200 {
		t.Fatalf("write: %d", resp.StatusCode)
	}
	resp = e.raw(http.MethodGet, base+"/content?path=plugins/config.yml", "", nil)
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(got) != "a: 1\n" {
		t.Fatalf("read: %d %q", resp.StatusCode, got)
	}
	_, body = e.do(http.MethodGet, base+"/?path=", nil)
	names := []string{}
	for _, it := range body["entries"].([]any) {
		names = append(names, it.(map[string]any)["name"].(string))
	}
	if strings.Contains(strings.Join(names, ","), ".homepanel") || !strings.Contains(strings.Join(names, ","), "plugins") {
		t.Errorf("list = %v", names)
	}

	// Traversal and protected paths. A leading "/" means the server root, so
	// "/etc/passwd" is just a file inside the server directory.
	for _, p := range []string{"../../etc/passwd", ".homepanel/run.json", "plugins/../../x"} {
		resp = e.raw(http.MethodGet, base+"/content?path="+p, "", nil)
		resp.Body.Close()
		if resp.StatusCode != 400 && resp.StatusCode != 403 && resp.StatusCode != 404 {
			t.Errorf("read %q: %d", p, resp.StatusCode)
		}
		resp = e.raw(http.MethodPut, base+"/content?path="+p, "text/plain", strings.NewReader("x"))
		resp.Body.Close()
		if resp.StatusCode != 400 && resp.StatusCode != 403 {
			t.Errorf("write %q: %d", p, resp.StatusCode)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".homepanel", "run.json")); string(b) == "x" {
		t.Error("run.json overwritten")
	}
	resp = e.raw(http.MethodPut, base+"/content?path=/etc/passwd", "text/plain", strings.NewReader("inside"))
	resp.Body.Close()
	if b, _ := os.ReadFile(filepath.Join(dir, "etc", "passwd")); resp.StatusCode != 200 || string(b) != "inside" {
		t.Errorf("root-relative absolute path: %d %q", resp.StatusCode, b)
	}
	if b, _ := os.ReadFile("/etc/passwd"); string(b) == "inside" {
		t.Fatal("host /etc/passwd overwritten")
	}

	// mkdir, rename, upload, download, extract, delete.
	resp, _ = e.do(http.MethodPost, base+"/mkdir", map[string]string{"path": "worlds/overworld"})
	if resp.StatusCode != 204 {
		t.Errorf("mkdir: %d", resp.StatusCode)
	}
	resp, _ = e.do(http.MethodPost, base+"/rename", map[string]string{"from": "worlds/overworld", "to": "worlds/main"})
	if resp.StatusCode != 204 {
		t.Errorf("rename: %d", resp.StatusCode)
	}
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	fw, _ := mw.CreateFormFile("files", "../evil.txt")
	_, _ = fw.Write([]byte("hello"))
	fw, _ = mw.CreateFormFile("files", "second.txt")
	_, _ = fw.Write([]byte("world"))
	mw.Close()
	resp = e.raw(http.MethodPost, base+"/upload?path=plugins", mw.FormDataContentType(), &mp)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: %d %s", resp.StatusCode, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "evil.txt")); err != nil {
		t.Error("upload with ../ in the name should land in the target dir under its base name")
	}
	if _, err := os.Stat(filepath.Join(e.cfg.ServersDir(), "evil.txt")); err == nil {
		t.Error("upload escaped")
	}
	resp = e.raw(http.MethodGet, base+"/download?path=plugins/second.txt", "", nil)
	got, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(got) != "world" || !strings.Contains(resp.Header.Get("Content-Disposition"), "second.txt") {
		t.Errorf("download: %d %q %s", resp.StatusCode, got, resp.Header.Get("Content-Disposition"))
	}
	resp, body = e.do(http.MethodPost, base+"/delete", map[string]any{"paths": []string{"plugins/evil.txt", "worlds"}})
	if resp.StatusCode != 204 {
		t.Errorf("delete: %d %v", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "worlds")); err == nil {
		t.Error("worlds not deleted")
	}

	// Permission matrix: files.read vs files.write.
	plain, _ := e.store.CreateUser(t.Context(), "plain", "password123", auth.RoleUser)
	_ = e.store.GrantPermissions(t.Context(), id, plain.ID, []auth.Permission{auth.PermFilesRead})
	e.loginAs("plain")
	resp, _ = e.do(http.MethodGet, base+"/?path=", nil)
	if resp.StatusCode != 200 {
		t.Errorf("files.read list: %d", resp.StatusCode)
	}
	resp = e.raw(http.MethodPut, base+"/content?path=x.txt", "text/plain", strings.NewReader("x"))
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("write without files.write: %d", resp.StatusCode)
	}
}

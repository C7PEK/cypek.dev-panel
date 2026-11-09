package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/auth"
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

func TestBackupsAPI(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	_, body := e.do(http.MethodPost, "/api/v1/servers", fakeServerBody("Bk"))
	id := body["id"].(string)
	base := "/api/v1/servers/" + id + "/backups"
	dir := filepath.Join(e.cfg.ServersDir(), id)
	_ = os.MkdirAll(filepath.Join(dir, "logs"), 0o750)
	_ = os.WriteFile(filepath.Join(dir, "world.dat"), []byte("world"), 0o640)
	_ = os.WriteFile(filepath.Join(dir, "logs", "a.log"), []byte("log"), 0o640)

	resp, body := e.do(http.MethodGet, base+"/", nil)
	if resp.StatusCode != 200 || body["writable"] != true || len(body["items"].([]any)) != 0 {
		t.Fatalf("list: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPut, base+"/settings", map[string]any{"excludes": []string{"logs/"}, "keepLast": 2, "keepDays": 0, "flushMinecraft": false})
	if resp.StatusCode != 200 || body["keepLast"].(float64) != 2 {
		t.Fatalf("settings: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(http.MethodPut, base+"/settings", map[string]any{"excludes": []string{"../x"}})
	if resp.StatusCode != 422 {
		t.Errorf("bad settings: %d %v", resp.StatusCode, body)
	}

	waitJob := func() map[string]any {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			_, b := e.do(http.MethodGet, base+"/", nil)
			if j, ok := b["job"].(map[string]any); ok && j["done"] == true {
				return b
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("job did not finish")
		return nil
	}
	for i := 0; i < 3; i++ {
		resp, body = e.do(http.MethodPost, base+"/", map[string]string{"note": "n"})
		if resp.StatusCode != 202 {
			t.Fatalf("create %d: %d %v", i, resp.StatusCode, body)
		}
		b := waitJob()
		if j := b["job"].(map[string]any); j["error"] != nil {
			t.Fatalf("backup error: %v", j["error"])
		}
		time.Sleep(1100 * time.Millisecond) // distinct file names (second resolution)
	}
	_, body = e.do(http.MethodGet, base+"/", nil)
	items := body["items"].([]any)
	if len(items) != 2 { // keepLast = 2
		t.Fatalf("retention: %d items", len(items))
	}
	bid := strconv.Itoa(int(items[0].(map[string]any)["id"].(float64)))
	files, _ := filepath.Glob(filepath.Join(e.cfg.BackupsDir(), id, "*.tar.zst"))
	if len(files) != 2 {
		t.Errorf("files on disk = %v", files)
	}

	// Download.
	resp = e.raw(http.MethodGet, base+"/"+bid+"/download", "", nil)
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || len(data) == 0 || !strings.Contains(resp.Header.Get("Content-Disposition"), ".tar.zst") {
		t.Errorf("download: %d %d bytes", resp.StatusCode, len(data))
	}

	// Restore: change a file, restore, verify; logs were excluded so they vanish.
	_ = os.WriteFile(filepath.Join(dir, "world.dat"), []byte("broken"), 0o640)
	resp, body = e.do(http.MethodPost, base+"/"+bid+"/restore", nil)
	if resp.StatusCode != 202 {
		t.Fatalf("restore: %d %v", resp.StatusCode, body)
	}
	b := waitJob()
	if j := b["job"].(map[string]any); j["kind"] != "restore" || j["error"] != nil {
		t.Fatalf("restore job = %v", j)
	}
	if w, _ := os.ReadFile(filepath.Join(dir, "world.dat")); string(w) != "world" {
		t.Errorf("world.dat after restore = %q", w)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs")); err == nil {
		t.Error("excluded logs dir reappeared")
	}
	if _, err := os.Stat(filepath.Join(dir, ".homepanel", "run.json")); err != nil {
		t.Error("run.json lost on restore")
	}
	resp, _ = e.do(http.MethodDelete, base+"/"+bid, nil)
	if resp.StatusCode != 204 {
		t.Errorf("delete: %d", resp.StatusCode)
	}
	files, _ = filepath.Glob(filepath.Join(e.cfg.BackupsDir(), id, "*.tar.zst"))
	if len(files) != 1 {
		t.Errorf("after delete files = %v", files)
	}

	// Permission matrix.
	plain, _ := e.store.CreateUser(t.Context(), "plain", "password123", auth.RoleUser)
	_ = e.store.GrantPermissions(t.Context(), id, plain.ID, []auth.Permission{auth.PermConsoleRead})
	e.loginAs("plain")
	if resp, _ := e.do(http.MethodGet, base+"/", nil); resp.StatusCode != 403 {
		t.Errorf("backups without perm: %d", resp.StatusCode)
	}
}

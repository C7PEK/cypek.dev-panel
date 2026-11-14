package staticsite

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func get(t *testing.T, h http.Handler, method, p string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://site"+p, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(b)
}

func TestServesFilesSafely(t *testing.T) {
	base := t.TempDir()
	pub := filepath.Join(base, "public")
	_ = os.MkdirAll(filepath.Join(pub, "docs"), 0o755)
	_ = os.MkdirAll(filepath.Join(pub, ".homepanel"), 0o755)
	_ = os.MkdirAll(filepath.Join(pub, "assets"), 0o755)
	_ = os.WriteFile(filepath.Join(pub, "index.html"), []byte("home"), 0o644)
	_ = os.WriteFile(filepath.Join(pub, "assets", "app.js"), []byte("js"), 0o644)
	_ = os.WriteFile(filepath.Join(pub, "docs", "readme.txt"), []byte("docs"), 0o644)
	_ = os.WriteFile(filepath.Join(pub, ".env"), []byte("SECRET=1"), 0o644)
	_ = os.WriteFile(filepath.Join(pub, ".homepanel", "run.json"), []byte("{}"), 0o644)
	_ = os.WriteFile(filepath.Join(base, "secret.txt"), []byte("outside"), 0o644)
	_ = os.Symlink(filepath.Join(base, "secret.txt"), filepath.Join(pub, "leak.txt"))
	_ = os.Symlink(base, filepath.Join(pub, "up"))

	root, err := os.OpenRoot(pub)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	h := Handler(root.FS(), false, false)

	if code, body := get(t, h, "GET", "/"); code != 200 || body != "home" {
		t.Errorf("/ = %d %q", code, body)
	}
	if code, body := get(t, h, "GET", "/assets/app.js"); code != 200 || body != "js" {
		t.Errorf("app.js = %d %q", code, body)
	}
	for _, p := range []string{"/.env", "/.homepanel/run.json", "/leak.txt", "/up/secret.txt", "/../secret.txt", "/%2e%2e/secret.txt", "/docs/", "/missing"} {
		code, body := get(t, h, "GET", p)
		if code == 200 || strings.Contains(body, "outside") || strings.Contains(body, "SECRET") || strings.Contains(body, "readme.txt") {
			t.Errorf("%s = %d %q", p, code, body)
		}
	}
	if code, _ := get(t, h, "POST", "/"); code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", code)
	}

	spa := Handler(root.FS(), true, false)
	if code, body := get(t, spa, "GET", "/dashboard/settings"); code != 200 || body != "home" {
		t.Errorf("SPA route = %d %q", code, body)
	}
	if code, _ := get(t, spa, "GET", "/assets/missing.js"); code != 404 {
		t.Errorf("SPA missing asset = %d, want 404", code)
	}
}

func TestPlaceholderWithoutIndex(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	code, body := get(t, Handler(root.FS(), false, false), "GET", "/")
	if code != 200 || !strings.Contains(body, "public") {
		t.Errorf("placeholder = %d %q", code, body)
	}
}

package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		cur, cand string
		want      bool
	}{
		{"0.1.0", "v0.2.0", true}, {"v1.2.3", "v1.2.3", false}, {"1.10.0", "v1.9.9", false},
		{"abc1234-dirty", "v0.1.0", true}, {"dev", "nope", false}, {"v0.9.0", "v1.0.0", true},
	}
	for _, c := range cases {
		if got := Newer(c.cur, c.cand); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.cur, c.cand, got)
		}
	}
}

func TestLatestAndApply(t *testing.T) {
	bin := []byte("#!/bin/sh\necho new\n")
	sum := sha256.Sum256(bin)
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v9.9.9", "body": "notes", "assets": []map[string]string{
			{"name": AssetName(), "browser_download_url": srv.URL + "/bin"},
			{"name": "SHA256SUMS", "browser_download_url": srv.URL + "/sums"},
		}})
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(bin) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + AssetName() + "\nffff  other\n"))
	})
	srv = httptest.NewTLSServer(mux)
	defer srv.Close()
	old := providers.DownloadClient
	providers.DownloadClient = srv.Client()
	defer func() { providers.DownloadClient = old }()

	c := &Client{APIBase: srv.URL, HTTP: srv.Client()}
	rel, err := c.Latest(context.Background())
	if err != nil || rel.Version != "9.9.9" || rel.BinaryURL == "" {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "homepanel")
	_ = os.WriteFile(target, []byte("old"), 0o755)
	if err := c.Apply(context.Background(), rel, target, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	bak, _ := os.ReadFile(target + ".bak")
	if string(got) != string(bin) || string(bak) != "old" {
		t.Errorf("after apply: %q bak %q", got, bak)
	}
	st, _ := os.Stat(target)
	if st.Mode()&0o111 == 0 {
		t.Error("not executable")
	}
}

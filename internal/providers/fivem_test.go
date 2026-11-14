package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestFiveMResolveAndOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"recommended": "38435", "latest": "38440",
			"recommended_download": "https://runtime.fivem.net/a/38435-x/fx.tar.xz", "latest_download": "https://runtime.fivem.net/a/38440-y/fx.tar.xz",
			"recommended_txadmin": "8.1.2", "latest_txadmin": "8.1.3",
			"support_policy": map[string]string{"38435": "9999-12-31", "38400": "2026-11-20", "38440": "9999-12-31"},
		})
	}))
	defer srv.Close()
	f := &FiveM{BaseURL: srv.URL}
	ctx := context.Background()
	opts, err := f.Options(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 5 || opts[0].Value != "recommended" || opts[1].Value != "latest" || opts[2].Value != "38440" {
		t.Errorf("options = %+v", opts)
	}
	if runtime.GOARCH != "amd64" {
		t.Skip("FiveM is amd64 only")
	}
	a, err := f.Resolve(ctx, "recommended")
	if err != nil || a.Build != 38435 || a.TxAdmin != "8.1.2" || a.URL == "" {
		t.Errorf("recommended = %+v, %v", a, err)
	}
	a, err = f.Resolve(ctx, "38440")
	if err != nil || a.Channel != "pinned" || a.Build != 38440 {
		t.Errorf("pinned = %+v, %v", a, err)
	}
	if _, err := f.Resolve(ctx, "38400"); err == nil {
		t.Error("unavailable pinned build resolved")
	}
	if _, err := f.Resolve(ctx, "garbage"); err == nil {
		t.Error("garbage selector resolved")
	}
}

func TestParseEnhancedDownloads(t *testing.T) {
	page := `<script>var data = {"other":{"x":1},"enhanced":{"linux":[{"displayName":"cfx-server_linux_x64.tar.xz","subtitle":"build 177","downloadURL":"https://cfx-downloads.akamaized.net/prod/abc/cfx-server_linux_x64.tar.xz"}],"windows":[]},"legacy":{}}</script>`
	a, err := parseEnhancedDownloads(page)
	if err != nil || a.Build != 177 || a.URL != "https://cfx-downloads.akamaized.net/prod/abc/cfx-server_linux_x64.tar.xz" {
		t.Fatalf("got %+v, %v", a, err)
	}
	for _, bad := range []string{
		`{"legacy":{}}`,
		`{"enhanced":{"linux":[{"subtitle":"build 1","downloadURL":"http://cfx-downloads.akamaized.net/a.tar.xz"}]}}`,
		`{"enhanced":{"linux":[{"subtitle":"build 1","downloadURL":"https://evil.example.com/a.tar.xz"}]}}`,
	} {
		if _, err := parseEnhancedDownloads(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestReleaseNoteSum(t *testing.T) {
	page := `<li><p>cloudflared-linux-amd64.deb: 5305dbaf62984e92e7daa29cdd6a35a4797edc23385d4c5b8422ff97eb96a19b<br>
cloudflared-linux-amd64: 51f7c68b57ed7269164075d7e789efce5b59355982f06392deeb037d982a51fd<br>
cloudflared-linux-arm64: 89cf5f39b64ccecc6f2a53b2cf8ae7b7635a0c5763c53159973caf5add4d6ce0</p></li>`
	if got := releaseNoteSum(page, "cloudflared-linux-amd64"); got != "51f7c68b57ed7269164075d7e789efce5b59355982f06392deeb037d982a51fd" {
		t.Errorf("amd64 = %q", got)
	}
	if got := releaseNoteSum(page, "cloudflared-linux-arm64"); got != "89cf5f39b64ccecc6f2a53b2cf8ae7b7635a0c5763c53159973caf5add4d6ce0" {
		t.Errorf("arm64 = %q", got)
	}
	if got := releaseNoteSum(page, "cloudflared-linux-386"); got != "" {
		t.Errorf("missing asset = %q", got)
	}
}

package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func TestCompareVersionsAndFilters(t *testing.T) {
	in := []string{"1.21.10", "1.21.11-rc3", "26.3", "1.21.11", "26.1.2", "1.20.6", "26.3-rc-3"}
	want := []string{"26.3", "26.3-rc-3", "26.1.2", "1.21.11", "1.21.11-rc3", "1.21.10", "1.20.6"}
	sorted := append([]string(nil), in...)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if compareVersions(sorted[j], sorted[i]) > 0 {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	if !reflect.DeepEqual(sorted, want) {
		t.Errorf("sorted = %v, want %v", sorted, want)
	}
	if got := StableVersions(want); !reflect.DeepEqual(got, []string{"26.3", "26.1.2", "1.21.11", "1.21.10", "1.20.6"}) {
		t.Errorf("stable = %v", got)
	}
	if PickFeature(21, []int{8, 11, 17, 21, 25}) != 21 || PickFeature(25, []int{8, 11, 17, 21, 25}) != 25 ||
		PickFeature(16, []int{8, 11, 17, 21}) != 17 || PickFeature(8, []int{8, 11}) != 8 || PickFeature(27, []int{21, 25}) != 27 {
		t.Error("PickFeature wrong")
	}
	if fallbackJavaFor("1.16.5") != 8 || fallbackJavaFor("1.20.4") != 17 || fallbackJavaFor("1.21.1") != 21 || fallbackJavaFor("26.3") != 25 {
		t.Error("fallbackJavaFor wrong")
	}
}

func fakePaper(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/paper", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"versions": map[string][]string{
			"1.21": {"1.21.11", "1.21.11-rc3", "1.21.10"}, "26.3": {"26.3-rc-3"},
		}})
	})
	mux.HandleFunc("/projects/paper/versions/1.21.11", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": map[string]any{"java": map[string]any{
			"version": map[string]int{"minimum": 21}, "flags": map[string][]string{"recommended": {"-XX:+UseG1GC"}}}}})
	})
	mux.HandleFunc("/projects/paper/versions/1.21.11/builds", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 133, "channel": "ALPHA", "downloads": map[string]any{"server:default": map[string]any{"name": "a.jar", "url": "https://x/a.jar", "checksums": map[string]string{"sha256": "aa"}}}},
			{"id": 132, "channel": "STABLE", "downloads": map[string]any{"server:default": map[string]any{"name": "paper-1.21.11-132.jar", "url": "https://x/p.jar", "checksums": map[string]string{"sha256": "bb"}}}},
		})
	})
	return httptest.NewServer(mux)
}

func TestPaperResolve(t *testing.T) {
	srv := fakePaper(t)
	defer srv.Close()
	p := &Paper{BaseURL: srv.URL, Project: "paper"}
	ctx := context.Background()
	vs, err := p.Versions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(vs, []string{"26.3-rc-3", "1.21.11", "1.21.11-rc3", "1.21.10"}) {
		t.Errorf("versions = %v", vs)
	}
	info, err := p.Resolve(ctx, "latest")
	if err != nil {
		t.Fatal(err)
	}

	if info.Version != "1.21.11" || info.LatestBuild != 132 || info.SHA256 != "bb" || info.JavaMinimum != 21 || info.JVMFlags[0] != "-XX:+UseG1GC" {
		t.Errorf("info = %+v", info)
	}
	if _, err := p.Resolve(ctx, "9.9.9"); err == nil {
		t.Error("unknown version resolved")
	}
}

func TestDownloadVerifiesAndRefusesHTTP(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()
	old := DownloadClient
	DownloadClient = srv.Client()
	defer func() { DownloadClient = old }()

	dest := t.TempDir() + "/f.bin"
	if err := Download(context.Background(), "http://example.com/x", dest, "", nil); err == nil {
		t.Error("http URL accepted")
	}
	if err := Download(context.Background(), srv.URL+"/x", dest, "00", nil); !errors.Is(err, ErrChecksum) {
		t.Errorf("bad checksum: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("file left behind after checksum failure")
	}

	var got []int64
	err := Download(context.Background(), srv.URL+"/x", dest, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", func(done, total int64) { got = append(got, done) })
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "hello" || len(got) == 0 || got[len(got)-1] != 5 {
		t.Errorf("content %q, progress %v", b, got)
	}
}

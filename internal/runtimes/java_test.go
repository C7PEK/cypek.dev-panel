package runtimes

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
)

type entry struct {
	name, content, link string
	typ                 byte
	mode                int64
}

func makeTarGz(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: e.mode, Typeflag: e.typ, Linkname: e.link, Size: int64(len(e.content))}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestEnsureDownloadsExtractsAndCaches(t *testing.T) {
	archive := makeTarGz(t, []entry{
		{name: "jdk-21.0.13+9-jre/", typ: tar.TypeDir, mode: 0o755},
		{name: "jdk-21.0.13+9-jre/bin/", typ: tar.TypeDir, mode: 0o755},
		{name: "jdk-21.0.13+9-jre/bin/java", typ: tar.TypeReg, mode: 0o755, content: "#!/bin/sh\necho java\n"},
		{name: "jdk-21.0.13+9-jre/lib/libjvm.so", typ: tar.TypeReg, content: "x"},
		{name: "jdk-21.0.13+9-jre/lib/link", typ: tar.TypeSymlink, link: "libjvm.so"},
	})
	sum := sha256.Sum256(archive)
	var downloads int
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/assets/latest/21/hotspot", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"release_name": "jdk-21.0.13+9", "binary": map[string]any{"package": map[string]any{
			"link": srv.URL + "/dl.tar.gz", "checksum": hex.EncodeToString(sum[:]), "size": len(archive)}}}})
	})
	mux.HandleFunc("/dl.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		downloads++
		_, _ = w.Write(archive)
	})
	srv = httptest.NewTLSServer(mux)
	defer srv.Close()
	old := providers.DownloadClient
	providers.DownloadClient = srv.Client()
	providers.Client = srv.Client()
	defer func() { providers.DownloadClient = old }()

	j := &Java{Dir: filepath.Join(t.TempDir(), "runtimes"), Adoptium: &providers.Adoptium{BaseURL: srv.URL}}
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	p, err := j.Ensure(context.Background(), 21, logf, nil)
	if err != nil {
		t.Fatalf("Ensure: %v (logs %v)", err, logs)
	}
	if !strings.HasSuffix(p, "/java-21/bin/java") {
		t.Errorf("path = %s", p)
	}
	if st, err := os.Stat(p); err != nil || st.Mode()&0o111 == 0 {
		t.Errorf("java not executable: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(j.Dir, "java-21", "lib", "link")); err != nil || target != "libjvm.so" {
		t.Errorf("symlink = %q, %v", target, err)
	}
	if _, err := os.Stat(filepath.Join(j.Dir, "java-21.tmp")); !os.IsNotExist(err) {
		t.Error("tmp dir left behind")
	}
	p2, err := j.Ensure(context.Background(), 21, logf, nil)
	if err != nil || p2 != p || downloads != 1 {
		t.Errorf("second Ensure: %s, %v, downloads=%d", p2, err, downloads)
	}
}

package fetch

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := map[string]string{
		"https://drive.google.com/file/d/1AbCdEfGhIjKlMnOp/view?usp=sharing": "https://drive.usercontent.google.com/download?id=1AbCdEfGhIjKlMnOp&export=download&confirm=t",
		"https://drive.google.com/open?id=1AbCdEfGhIjKlMnOp":                 "https://drive.usercontent.google.com/download?id=1AbCdEfGhIjKlMnOp&export=download&confirm=t",
		"https://www.dropbox.com/scl/fi/xyz/world.zip?rlkey=abc&dl=0":         "https://www.dropbox.com/scl/fi/xyz/world.zip?dl=1&rlkey=abc",
		"https://github.com/o/r/blob/main/pack.zip":                           "https://raw.githubusercontent.com/o/r/main/pack.zip",
		"https://cdn.example.com/files/server.zip":                            "https://cdn.example.com/files/server.zip",
	}
	for in, want := range cases {
		got, err := Resolve(in)
		if err != nil || got.URL != want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", in, got.URL, err, want)
		}
	}
	if s, err := Resolve("https://1drv.ms/u/s!AbCdEf"); err != nil || !strings.HasPrefix(s.URL, "https://api.onedrive.com/v1.0/shares/u!") {
		t.Errorf("onedrive: %v %v", s, err)
	}
	for _, bad := range []string{"https://mega.nz/file/abc#key", "https://drive.google.com/drive/folders/1Abc", "https://we.tl/t-abc", "ftp://x/y.zip", "file:///etc/passwd", "https://user:pw@host/x.zip", "not a link"} {
		if _, err := Resolve(bad); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

type memSink struct {
	buf       bytes.Buffer
	committed string
	aborted   bool
}

func (m *memSink) Create() (io.Writer, error)          { return &m.buf, nil }
func (m *memSink) Commit(name string) (string, error) { m.committed = name; return name, nil }
func (m *memSink) Abort()                             { m.aborted = true }

func TestDownload(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/named":
			w.Header().Set("Content-Disposition", `attachment; filename="../../etc/World Save.zip"`)
			w.Write([]byte("zipdata"))
		case "/files/plain.tar.gz":
			w.Write([]byte("tar"))
		case "/login":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<html>sign in</html>"))
		case "/big":
			w.Header().Set("Content-Length", "1000")
			w.Write(make([]byte, 1000))
		case "/short":
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("only a bit"))
		case "/private":
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	c := srv.Client()
	ctx := context.Background()

	s := &memSink{}
	name, err := Download(ctx, c, Source{URL: srv.URL + "/named", Kind: "direct"}, s, 0, nil)
	if err != nil || name != "World Save.zip" || s.buf.String() != "zipdata" {
		t.Fatalf("named: %q %v %q", name, err, s.buf.String())
	}
	s = &memSink{}
	if name, _ = Download(ctx, c, Source{URL: srv.URL + "/files/plain.tar.gz"}, s, 0, nil); name != "plain.tar.gz" {
		t.Errorf("path name: %q", name)
	}
	s = &memSink{}
	if _, err = Download(ctx, c, Source{URL: srv.URL + "/login", Kind: "gdrive"}, s, 0, nil); !errors.Is(err, ErrNotAFile) || !strings.Contains(err.Error(), "Anyone with the link") {
		t.Errorf("html: %v", err)
	}
	s = &memSink{}
	if _, err = Download(ctx, c, Source{URL: srv.URL + "/big"}, s, 500, nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
	s = &memSink{}
	if _, err = Download(ctx, c, Source{URL: srv.URL + "/short"}, s, 0, nil); err == nil || !s.aborted {
		t.Errorf("short body: %v aborted=%v", err, s.aborted)
	}
	if _, err = Download(ctx, c, Source{URL: srv.URL + "/private"}, &memSink{}, 0, nil); err == nil || !strings.Contains(err.Error(), "public") {
		t.Errorf("403: %v", err)
	}
}

package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/ulikunitz/xz"
)

type entry struct {
	name, content, link string
	typ                 byte
	mode                int64
}

func tarBytes(t *testing.T, entries []entry, compress string) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: e.mode, Typeflag: e.typ, Linkname: e.link, Size: int64(len(e.content))}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			_, _ = tw.Write([]byte(e.content))
		}
	}
	tw.Close()
	var out bytes.Buffer
	switch compress {
	case "gz":
		w := gzip.NewWriter(&out)
		_, _ = w.Write(raw.Bytes())
		w.Close()
	case "xz":
		w, _ := xz.NewWriter(&out)
		_, _ = w.Write(raw.Bytes())
		w.Close()
	default:
		return raw.Bytes()
	}
	return out.Bytes()
}

func TestExtractFormatsAndStrip(t *testing.T) {
	entries := []entry{
		{name: "top/", typ: tar.TypeDir, mode: 0o755},
		{name: "top/run.sh", typ: tar.TypeReg, mode: 0o755, content: "#!/bin/bash\n"},
		{name: "top/lib/a.so", typ: tar.TypeReg, content: "a"},
		{name: "top/lib/b.so", typ: tar.TypeSymlink, link: "a.so"},
	}
	for _, format := range []string{"gz", "xz"} {
		dir := t.TempDir()
		name := filepath.Join(dir, "a.tar."+format)
		if err := os.WriteFile(name, tarBytes(t, entries, format), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Extract(name, filepath.Join(dir, "out"), Options{Strip: 1}); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		st, err := os.Stat(filepath.Join(dir, "out", "run.sh"))
		if err != nil || st.Mode()&0o111 == 0 {
			t.Errorf("%s: run.sh = %v, %v", format, st, err)
		}
		if l, err := os.Readlink(filepath.Join(dir, "out", "lib", "b.so")); err != nil || l != "a.so" {
			t.Errorf("%s: symlink = %q, %v", format, l, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "out", "top")); err == nil {
			t.Errorf("%s: strip not applied", format)
		}
	}
	// zip
	dir := t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("repo-master/resources/chat/fxmanifest.lua")
	_, _ = w.Write([]byte("fx_version 'cerulean'"))
	w, _ = zw.Create("repo-master/README.md")
	_, _ = w.Write([]byte("hi"))
	zw.Close()
	name := filepath.Join(dir, "sd.zip")
	_ = os.WriteFile(name, buf.Bytes(), 0o600)
	if err := Extract(name, filepath.Join(dir, "out"), Options{Strip: 1}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "out", "resources", "chat", "fxmanifest.lua")); string(b) != "fx_version 'cerulean'" {
		t.Errorf("zip content = %q", b)
	}
	if err := Extract(filepath.Join(dir, "x.rar"), dir, Options{}); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestExtractRejectsEscapes(t *testing.T) {
	// Entries that must abort extraction.
	cases := map[string][]entry{
		"dotdot": {{name: "top/../../evil", typ: tar.TypeReg, content: "x"}},
		"abs":    {{name: "/etc/evil", typ: tar.TypeReg, content: "x"}},
		"viasymlink": { // a symlinked directory must never redirect a write
			{name: "top/d", typ: tar.TypeSymlink, link: "../.."},
			{name: "top/d/evil", typ: tar.TypeReg, content: "x"},
		},
		"viaabssymlink": {
			{name: "top/d", typ: tar.TypeSymlink, link: os.TempDir()},
			{name: "top/d/evil-hp-test", typ: tar.TypeReg, content: "x"},
		},
	}
	for name, entries := range cases {
		dir := t.TempDir()
		a := filepath.Join(dir, "a.tar.gz")
		_ = os.WriteFile(a, tarBytes(t, entries, "gz"), 0o600)
		out := filepath.Join(dir, "out")
		err := Extract(a, out, Options{})
		if _, statErr := os.Stat(filepath.Join(dir, "evil")); statErr == nil {
			t.Errorf("%s: file escaped the destination", name)
		}
		if _, statErr := os.Stat(filepath.Join(os.TempDir(), "evil-hp-test")); statErr == nil {
			os.Remove(filepath.Join(os.TempDir(), "evil-hp-test"))
			t.Errorf("%s: file escaped through an absolute symlink", name)
		}
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// Dangling or outward symlinks themselves are allowed (chroot-style
	// archives need them) and are recorded verbatim.
	dir := t.TempDir()
	a := filepath.Join(dir, "l.tar.gz")
	_ = os.WriteFile(a, tarBytes(t, []entry{
		{name: "top/abs", typ: tar.TypeSymlink, link: "/usr/glibc-compat/lib/ld.so"},
		{name: "top/rel", typ: tar.TypeSymlink, link: "../../etc/passwd"},
	}, "gz"), 0o600)
	if err := Extract(a, filepath.Join(dir, "out"), Options{Strip: 1}); err != nil {
		t.Fatalf("symlink archive: %v", err)
	}
	if l, _ := os.Readlink(filepath.Join(dir, "out", "abs")); l != "/usr/glibc-compat/lib/ld.so" {
		t.Errorf("abs link = %q", l)
	}
	// zip slip
	dir = t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../evil.txt")
	_, _ = w.Write([]byte("x"))
	zw.Close()
	name := filepath.Join(dir, "z.zip")
	_ = os.WriteFile(name, buf.Bytes(), 0o600)
	if err := Extract(name, filepath.Join(dir, "out"), Options{}); err == nil {
		t.Error("zip slip accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Error("zip slip wrote outside")
	}
}

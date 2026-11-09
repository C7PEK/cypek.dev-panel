package files

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"

	"github.com/C7PEK/cypek.dev-panel/internal/archive"
)

func TestDeleteAndRenameActOnTheLinkItself(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "world"), 0o755)
	os.WriteFile(filepath.Join(root, "world", "level.dat"), []byte("x"), 0o644)
	os.Symlink(".", filepath.Join(root, "l"))
	os.Symlink("world", filepath.Join(root, "shortcut"))
	m := New(root)
	if err := m.Delete("l"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete("shortcut"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "world", "level.dat")); err != nil {
		t.Fatal("deleting a link removed its target")
	}
	os.Symlink("world", filepath.Join(root, "ln"))
	if err := m.Rename("ln", "ln2"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(root, "ln2")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("rename moved the target instead of the link")
	}
	if err := m.Delete(".homepanel"); err == nil {
		t.Error(".homepanel deletable")
	}
}

func writeTar(t *testing.T, path string, entries []tar.Header, bodies map[string]string) {
	f, _ := os.Create(path)
	tw := tar.NewWriter(f)
	for _, h := range entries {
		h := h
		body := bodies[h.Name]
		h.Size = int64(len(body))
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		h.Mode = 0o644
		tw.WriteHeader(&h)
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(body))
		}
	}
	tw.Close()
	f.Close()
}

func TestExtractRefusesLinksAndMeta(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "srv")
	os.MkdirAll(filepath.Join(root, ".homepanel"), 0o755)
	m := New(root)
	writeTar(t, filepath.Join(root, "links.tar"), []tar.Header{{Name: "l", Typeflag: tar.TypeSymlink, Linkname: "/"}}, nil)
	if err := m.Extract("links.tar", ""); err == nil {
		t.Error("symlink accepted from an uploaded archive")
	}
	writeTar(t, filepath.Join(root, "meta.tar"), []tar.Header{{Name: ".homepanel/run.json", Typeflag: tar.TypeReg}}, map[string]string{".homepanel/run.json": `{"command":"/bin/evil"}`})
	if err := m.Extract("meta.tar", ""); err == nil {
		t.Error("archive wrote into .homepanel")
	}
}

func TestHardlinkThroughSymlinkCannotEscape(t *testing.T) {
	base := t.TempDir()
	dest, db := filepath.Join(base, "srv"), filepath.Join(base, "panel.db")
	os.MkdirAll(dest, 0o755)
	os.WriteFile(db, []byte("DB"), 0o600)
	tarPath := filepath.Join(base, "x.tar")
	writeTar(t, tarPath, []tar.Header{
		{Name: "l", Typeflag: tar.TypeSymlink, Linkname: base},
		{Name: "x", Typeflag: tar.TypeLink, Linkname: "l/panel.db"},
		{Name: "x", Typeflag: tar.TypeReg},
	}, map[string]string{"x": "PWNED"})
	_ = archive.Extract(tarPath, dest, archive.Options{Resolve: ResolveSafe})
	if b, _ := os.ReadFile(db); string(b) != "DB" {
		t.Fatalf("panel.db changed through a hardlink: %q", b)
	}
}

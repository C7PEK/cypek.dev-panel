package files

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (*Manager, string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "servers", "abcd1234")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside, filepath.Join(root, ".homepanel"), filepath.Join(root, "plugins"), filepath.Join(root, "zażółć gęślą")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, "server.properties"), []byte("motd=hi\n"), 0o640)
	_ = os.WriteFile(filepath.Join(root, ".homepanel", "run.json"), []byte("{}"), 0o600)
	_ = os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0o640)

	_ = os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "leak.txt"))
	_ = os.Symlink(outside, filepath.Join(root, "leakdir"))
	_ = os.Symlink("plugins", filepath.Join(root, "plugins-link"))
	return New(root), root, outside
}

func TestResolveSafeMalicious(t *testing.T) {
	m, root, _ := setup(t)
	bad := []string{
		"../", "..", "../../etc/passwd", "plugins/../../x", "/etc/passwd", "....//....//etc",
		"leak.txt", "leakdir", "leakdir/secret.txt", "leakdir/../x", "plugins/../leakdir/secret.txt",
		"a\x00b", "..\\..\\windows", "%2e%2e/x",
	}
	for _, p := range bad {
		abs, err := ResolveSafe(root, p)
		if err == nil && (abs == root || strings.HasPrefix(abs, root+string(filepath.Separator))) {
			if real, rerr := filepath.EvalSymlinks(abs); rerr == nil && !strings.HasPrefix(real, root) {
				t.Errorf("%q resolved outside via symlink: %s", p, real)
			}
			continue
		}
		if err == nil {
			t.Errorf("%q resolved to %s without error", p, abs)
		}
	}

	good := map[string]string{"": root, "plugins": filepath.Join(root, "plugins"), "/plugins/": filepath.Join(root, "plugins"),
		"zażółć gęślą": filepath.Join(root, "zażółć gęślą"),
		"plugins-link": filepath.Join(root, "plugins"), "new/deep/file.txt": filepath.Join(root, "new", "deep", "file.txt")}
	for p, want := range good {
		got, err := ResolveSafe(root, p)
		if err != nil || got != want {
			t.Errorf("ResolveSafe(%q) = %q, %v; want %q", p, got, err, want)
		}
	}
	_ = m
}

func TestProtectedMetaDir(t *testing.T) {
	m, root, _ := setup(t)
	blocked := func(err error) bool { return errors.Is(err, ErrProtected) || errors.Is(err, ErrOutsideRoot) }
	for _, p := range []string{".homepanel", ".homepanel/run.json", "plugins/../.homepanel/run.json", "/.homepanel/run.json"} {
		if _, err := m.ReadText(p); !blocked(err) {
			t.Errorf("read %q: %v", p, err)
		}
		if err := m.WriteText(p, []byte("x")); !blocked(err) {
			t.Errorf("write %q: %v", p, err)
		}
		if err := m.Delete(p); !blocked(err) {
			t.Errorf("delete %q: %v", p, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".homepanel", "run.json")); string(b) != "{}" {
		t.Error("run.json was modified")
	}
	_ = os.Symlink(filepath.Join(root, ".homepanel"), filepath.Join(root, "meta-link"))
	if _, err := m.ReadText("meta-link/run.json"); !errors.Is(err, ErrProtected) {
		t.Errorf("read via symlink into .homepanel: %v", err)
	}
	entries, err := m.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == ".homepanel" {
			t.Error(".homepanel listed")
		}
	}
}

func TestCRUD(t *testing.T) {
	m, root, outside := setup(t)
	entries, err := m.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || entries[0].Type != "dir" {
		t.Errorf("entries = %+v", entries)
	}
	if _, err := m.List("server.properties"); !errors.Is(err, ErrNotDir) {
		t.Errorf("List file: %v", err)
	}
	if _, err := m.List("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("List missing: %v", err)
	}

	if err := m.WriteText("plugins/config.yml", []byte("a: 1\n")); err != nil {
		t.Fatal(err)
	}
	b, err := m.ReadText("plugins/config.yml")
	if err != nil || string(b) != "a: 1\n" {
		t.Errorf("ReadText = %q, %v", b, err)
	}
	if err := m.WriteText("new/dir/file.txt", []byte("x")); err != nil {
		t.Errorf("write with new parents: %v", err)
	}
	if err := m.WriteText("plugins", []byte("x")); !errors.Is(err, ErrIsDir) {
		t.Errorf("write over dir: %v", err)
	}
	if _, err := m.ReadText("leak.txt"); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("read through outward symlink: %v", err)
	}
	if err := m.WriteText("leakdir/pwned.txt", []byte("x")); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("write through outward dir symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); err == nil {
		t.Error("wrote outside the root")
	}

	if err := m.Mkdir("worlds/nether"); err != nil {
		t.Fatal(err)
	}
	if err := m.Mkdir("worlds"); !errors.Is(err, ErrExists) {
		t.Errorf("mkdir existing: %v", err)
	}
	if err := m.Rename("worlds/nether", "worlds/end"); err != nil {
		t.Fatal(err)
	}
	if err := m.Rename("worlds/end", "server.properties"); !errors.Is(err, ErrExists) {
		t.Errorf("rename over existing: %v", err)
	}
	if err := m.Rename("worlds", "../escaped"); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("rename outside: %v", err)
	}
	if err := m.Rename("worlds", "worlds/end/inner"); err == nil {
		t.Error("moved dir into itself")
	}
	if err := m.Rename("", "x"); !errors.Is(err, ErrProtected) {
		t.Errorf("rename root: %v", err)
	}

	n, err := m.Create("plugins", "up.jar", strings.NewReader("jarjar"), 10)
	if err != nil || n != 6 {
		t.Errorf("Create = %d, %v", n, err)
	}
	if _, err := m.Create("plugins", "big.bin", strings.NewReader("0123456789abc"), 10); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversize upload: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "plugins", "big.bin")); err == nil {
		t.Error("partial upload left behind")
	}
	for _, name := range []string{"../x", "a/b", "", ".."} {
		if _, err := m.Create("plugins", name, strings.NewReader("x"), 10); !errors.Is(err, ErrBadName) {
			t.Errorf("Create name %q: %v", name, err)
		}
	}
	if _, err := m.Create("leakdir", "x", strings.NewReader("x"), 10); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("upload into outward symlink: %v", err)
	}

	if err := m.Delete("leakdir"); err != nil && !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("delete symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Error("symlink target was deleted")
	}
	if err := m.Delete("worlds"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(""); !errors.Is(err, ErrProtected) {
		t.Errorf("delete root: %v", err)
	}
	if err := m.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing: %v", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("ok/a.txt")
	_, _ = w.Write([]byte("a"))
	w, _ = zw.Create("../../evil.txt")
	_, _ = w.Write([]byte("evil"))
	zw.Close()
	_ = os.WriteFile(filepath.Join(root, "pack.zip"), buf.Bytes(), 0o640)
	if err := m.Extract("pack.zip", "unpacked"); err == nil {
		t.Error("zip slip archive extracted without error")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "evil.txt")); err == nil {
		t.Error("zip slip escaped")
	}
	if _, err := m.ReadText("unpacked/ok/a.txt"); err != nil {
		t.Logf("partial extraction before the bad entry is acceptable: %v", err)
	}
	if err := m.Extract("../x.zip", "."); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("extract outside: %v", err)
	}
	usage, err := m.DiskUsage()
	if err != nil || usage == 0 {
		t.Errorf("DiskUsage = %d, %v", usage, err)
	}
}

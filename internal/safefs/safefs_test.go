package safefs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestWriteNeverFollowsLinks(t *testing.T) {
	base := t.TempDir()
	root, outside := filepath.Join(base, "srv"), filepath.Join(base, "panel.db")
	os.MkdirAll(root, 0o755)
	os.WriteFile(outside, []byte("SECRET"), 0o600)

	// A symlink at the target is replaced, not followed.
	os.Symlink(outside, filepath.Join(root, "server.properties"))
	if err := WriteFile(root, "server.properties", []byte("motd=x"), 0o640); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(outside); string(b) != "SECRET" {
		t.Fatalf("wrote through symlink: %q", b)
	}
	// A hardlinked target is replaced by rename, the other name keeps its data.
	os.WriteFile(filepath.Join(root, "a"), []byte("one"), 0o640)
	os.Link(filepath.Join(root, "a"), filepath.Join(root, "b"))
	WriteFile(root, "a", []byte("two"), 0o640)
	if b, _ := os.ReadFile(filepath.Join(root, "b")); string(b) != "one" {
		t.Fatalf("modified hardlink: %q", b)
	}
	// A symlinked folder pointing outside is refused.
	os.Symlink(base, filepath.Join(root, ".homepanel"))
	if err := WriteFile(root, ".homepanel/run.json", []byte("{}"), 0o600); err == nil {
		t.Fatal("wrote through a folder symlink to outside")
	}
	if _, err := os.Stat(filepath.Join(base, "run.json")); err == nil {
		t.Fatal("file created outside")
	}
}

func TestReadRefusesSpecialFilesAndHugeOnes(t *testing.T) {
	root := t.TempDir()
	os.Symlink("/dev/zero", filepath.Join(root, "last-exit.json"))
	if _, err := ReadFile(root, "last-exit.json", 1<<20); err == nil {
		t.Fatal("read /dev/zero through symlink")
	}
	syscall.Mkfifo(filepath.Join(root, "fifo"), 0o644)
	done := make(chan error, 1)
	go func() { _, err := ReadFile(root, "fifo", 1<<20); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegular) {
			t.Fatalf("fifo: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked on a FIFO")
	}
	os.WriteFile(filepath.Join(root, "big"), make([]byte, 2048), 0o644)
	if _, err := ReadFile(root, "big", 1024); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big: %v", err)
	}
	os.WriteFile(filepath.Join(root, "ok"), []byte("hi"), 0o644)
	if b, err := ReadFile(root, "ok", 1024); err != nil || string(b) != "hi" {
		t.Fatalf("ok: %q %v", b, err)
	}
}

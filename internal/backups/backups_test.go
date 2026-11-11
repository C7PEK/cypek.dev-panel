package backups

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExcluded(t *testing.T) {
	pats := []string{"logs/", "cache/", "*.log", "world_nether/region"}
	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"logs", true, true}, {"logs/latest.log", false, true}, {"server.jar", false, false},
		{"plugins/x.log", false, true}, {"cache", true, true}, {"mycache", true, false},
		{"world_nether/region", true, true}, {"world/region", true, false}, {"logsx/a", false, false},
	}
	for _, c := range cases {
		if got := Excluded(pats, c.rel, c.isDir); got != c.want {
			t.Errorf("Excluded(%q, dir=%v) = %v, want %v", c.rel, c.isDir, got, c.want)
		}
	}
	s := Settings{Excludes: []string{" logs/ ", "./cache/", "/plugins/*.jar", "", "../x"}}
	if err := s.Validate(); err == nil {
		t.Error("exclude with .. accepted")
	}
	s = Settings{Excludes: []string{" logs/ ", "./cache/", "/plugins/*.jar", ""}, KeepLast: 3}
	if err := s.Validate(); err != nil || len(s.Excludes) != 3 || s.Excludes[2] != "plugins/*.jar" {
		t.Errorf("Validate = %v, %v", s.Excludes, err)
	}
}

func TestCreateRestoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	srv := filepath.Join(root, "servers", "abcd1234")
	for _, d := range []string{"world/region", "logs", "cache", ".homepanel", "plugins"} {
		_ = os.MkdirAll(filepath.Join(srv, d), 0o750)
	}
	_ = os.WriteFile(filepath.Join(srv, "server.properties"), []byte("motd=hi"), 0o640)
	_ = os.WriteFile(filepath.Join(srv, "world", "level.dat"), []byte("level"), 0o640)
	_ = os.WriteFile(filepath.Join(srv, "world", "region", "r.0.0.mca"), make([]byte, 200000), 0o640)
	_ = os.WriteFile(filepath.Join(srv, "logs", "latest.log"), []byte("log"), 0o640)
	_ = os.WriteFile(filepath.Join(srv, "cache", "x.bin"), []byte("cache"), 0o640)
	_ = os.WriteFile(filepath.Join(srv, ".homepanel", "run.json"), []byte("{}"), 0o600)
	_ = os.WriteFile(filepath.Join(srv, "console.sock"), []byte("not a socket"), 0o640)
	_ = os.Symlink("server.properties", filepath.Join(srv, "props-link"))

	dest := filepath.Join(root, "backups", "abcd1234", FileName("abcd1234", time.Now()))
	var lastFiles int
	size, err := Create(context.Background(), srv, dest, Settings{Excludes: []string{"logs/", "cache/"}}, func(f int, b int64) { lastFiles = f })
	if err != nil {
		t.Fatal(err)
	}
	if size == 0 || size > 100000 {
		t.Errorf("archive size = %d", size)
	}
	if lastFiles != 3 {
		t.Errorf("files backed up = %d", lastFiles)
	}

	_ = os.WriteFile(filepath.Join(srv, "server.properties"), []byte("changed"), 0o640)
	_ = os.WriteFile(filepath.Join(srv, "plugins", "new.jar"), []byte("new"), 0o640)
	_ = os.Remove(filepath.Join(srv, "world", "level.dat"))
	if err := Restore(context.Background(), dest, srv); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(srv, "server.properties")); string(b) != "motd=hi" {
		t.Errorf("server.properties = %q", b)
	}
	if _, err := os.Stat(filepath.Join(srv, "world", "level.dat")); err != nil {
		t.Error("level.dat not restored")
	}
	if _, err := os.Stat(filepath.Join(srv, "plugins", "new.jar")); err == nil {
		t.Error("file created after the backup survived the restore")
	}
	if _, err := os.Stat(filepath.Join(srv, "logs", "latest.log")); err == nil {
		t.Error("excluded logs dir came back")
	}
	if _, err := os.Stat(filepath.Join(srv, ".homepanel", "run.json")); err != nil {
		t.Error("run.json missing after restore")
	}
	if l, err := os.Readlink(filepath.Join(srv, "props-link")); err != nil || l != "server.properties" {
		t.Errorf("symlink = %q, %v", l, err)
	}
	for _, leftover := range []string{srv + ".restore-tmp", srv + ".restore-old"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s left behind", leftover)
		}
	}
	if err := Restore(context.Background(), filepath.Join(root, "missing.tar.zst"), srv); err == nil {
		t.Error("restore from missing archive succeeded")
	}
	if _, err := os.Stat(filepath.Join(srv, "server.properties")); err != nil {
		t.Error("failed restore damaged the server dir")
	}
}

func TestPrune(t *testing.T) {
	now := time.Now()
	items := []Item{}
	for i := 0; i < 10; i++ {
		items = append(items, Item{ID: int64(i), CreatedAt: now.Add(-time.Duration(i) * 24 * time.Hour)})
	}
	del := Prune(Settings{KeepLast: 3}, items, now)
	if len(del) != 7 || del[0].ID != 3 {
		t.Errorf("keepLast prune = %v", del)
	}
	del = Prune(Settings{KeepDays: 5}, items, now)
	if len(del) != 4 {
		t.Errorf("keepDays prune = %v", del)
	}
	if del := Prune(Settings{}, items, now); len(del) != 0 {
		t.Errorf("unlimited prune = %v", del)
	}
	if err := CheckWritable(t.TempDir()); err != nil {
		t.Error(err)
	}
	if err := CheckWritable("/proc/nope"); err == nil {
		t.Error("unwritable dir accepted")
	}
}

// Package backups creates and restores tar.zst snapshots of server
// directories. Archives are written atomically, excluded paths are globs
// relative to the server root, and restores go through a staging directory
// so a failed restore never leaves a half-written server behind.
package backups

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/C7PEK/cypek.dev-panel/internal/archive"
	"github.com/C7PEK/cypek.dev-panel/internal/files"
)

// Settings are per-server backup preferences.
type Settings struct {
	// Excludes are glob patterns relative to the server dir; a trailing "/"
	// marks a directory. Examples: "cache/", "logs/", "*.log".
	Excludes []string `json:"excludes"`
	// KeepLast keeps the newest N backups (0 = unlimited).
	KeepLast int `json:"keepLast"`
	// KeepDays deletes backups older than N days (0 = unlimited).
	KeepDays int `json:"keepDays"`
	// FlushMinecraft sends save-off/save-all before and save-on after.
	FlushMinecraft bool `json:"flushMinecraft"`
}

// DefaultSettings for a new server.
func DefaultSettings(category string) Settings {
	s := Settings{Excludes: []string{"logs/", "cache/", "crash-reports/"}, KeepLast: 7, KeepDays: 0}
	switch category {
	case "minecraft":
		s.FlushMinecraft = true
	case "fivem":
		s.Excludes = append(s.Excludes, "artifact/", "server-files/", "txData/logs/")
	}
	return s
}

// Validate normalises settings.
func (s *Settings) Validate() error {
	if s.KeepLast < 0 || s.KeepLast > 1000 || s.KeepDays < 0 || s.KeepDays > 3650 {
		return errors.New("retention values out of range")
	}
	clean := make([]string, 0, len(s.Excludes))
	for _, e := range s.Excludes {
		e = strings.TrimSpace(strings.ReplaceAll(e, "\\", "/"))
		e = strings.TrimPrefix(e, "./")
		e = strings.TrimPrefix(e, "/")
		if e == "" {
			continue
		}
		if strings.Contains(e, "..") {
			return fmt.Errorf("exclude %q must be relative", e)
		}
		if _, err := path.Match(strings.TrimSuffix(e, "/"), ""); err != nil {
			return fmt.Errorf("exclude %q: %w", e, err)
		}
		clean = append(clean, e)
	}
	if len(clean) > 100 {
		return errors.New("too many exclude patterns")
	}
	s.Excludes = clean
	return nil
}

// Excluded reports whether rel (slash-separated, dirs without trailing /)
// matches any pattern. Directory patterns ("cache/") exclude the whole tree.
func Excluded(patterns []string, rel string, isDir bool) bool {
	for _, p := range patterns {
		dirOnly := strings.HasSuffix(p, "/")
		p = strings.TrimSuffix(p, "/")
		if dirOnly {
			if isDir && (p == rel || matchPath(p, rel)) {
				return true
			}
			if strings.HasPrefix(rel, p+"/") {
				return true
			}
			continue
		}
		if matchPath(p, rel) || matchPath(p, path.Base(rel)) {
			return true
		}
	}
	return false
}

func matchPath(pattern, rel string) bool {
	ok, _ := path.Match(pattern, rel)
	return ok
}

// Progress reports bytes written so far.
type Progress func(files int, bytes int64)

// Create writes a tar.zst of srcDir to destPath. Sockets, pid files and the
// panel's own temp files are always skipped.
func Create(ctx context.Context, srcDir, destPath string, settings Settings, progress Progress) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destPath), "."+filepath.Base(destPath)+".part-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	zw, err := zstd.NewWriter(tmp, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(2))
	if err != nil {
		tmp.Close()
		return 0, err
	}
	tw := tar.NewWriter(zw)
	srcDir = filepath.Clean(srcDir)
	var files int
	var bytes int64
	walkErr := filepath.WalkDir(srcDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p == srcDir {
			return nil
		}
		rel, _ := filepath.Rel(srcDir, p)
		rel = filepath.ToSlash(rel)
		if Excluded(settings.Excludes, rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			hdr, _ := tar.FileInfoHeader(info, "")
			hdr.Name = rel + "/"
			return tw.WriteHeader(hdr)
		case mode.IsRegular():
			if strings.HasSuffix(rel, ".sock") || strings.HasSuffix(rel, ".pid") || strings.Contains(path.Base(rel), ".part-") {
				return nil
			}
			hdr, _ := tar.FileInfoHeader(info, "")
			hdr.Name = rel
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			n, err := io.Copy(tw, f)
			f.Close()
			if err != nil {
				return err
			}
			files++
			bytes += n
			if progress != nil && files%50 == 0 {
				progress(files, bytes)
			}
			return nil
		case mode&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return nil
			}
			hdr, _ := tar.FileInfoHeader(info, link)
			hdr.Name = rel
			return tw.WriteHeader(hdr)
		default:
			return nil // sockets, devices
		}
	})
	if walkErr != nil {
		tw.Close()
		zw.Close()
		tmp.Close()
		return 0, walkErr
	}
	if err := tw.Close(); err != nil {
		zw.Close()
		tmp.Close()
		return 0, err
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if progress != nil {
		progress(files, bytes)
	}
	st, err := os.Stat(tmpName)
	if err != nil {
		return 0, err
	}
	_ = os.Chmod(tmpName, 0o640)
	if err := os.Rename(tmpName, destPath); err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// Restore replaces the contents of serverDir with the archive. It extracts
// into a sibling staging directory first, then swaps; the previous contents
// are removed only after the swap succeeded. The .homepanel directory from
// the current installation is kept when the archive has none.
func Restore(ctx context.Context, archivePath, serverDir string) error {
	serverDir = filepath.Clean(serverDir)
	parent := filepath.Dir(serverDir)
	staging := serverDir + ".restore-tmp"
	old := serverDir + ".restore-old"
	os.RemoveAll(staging)
	os.RemoveAll(old)
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return err
	}
	if err := archive.Extract(archivePath, staging, archive.Options{Resolve: files.ResolveSafe}); err != nil {
		os.RemoveAll(staging)
		return fmt.Errorf("extract backup: %w", err)
	}
	if ctx.Err() != nil {
		os.RemoveAll(staging)
		return ctx.Err()
	}
	// Keep panel metadata if the archive predates it.
	if _, err := os.Stat(filepath.Join(staging, ".homepanel")); err != nil {
		if _, err := os.Stat(filepath.Join(serverDir, ".homepanel")); err == nil {
			_ = copyDir(filepath.Join(serverDir, ".homepanel"), filepath.Join(staging, ".homepanel"))
		}
	}
	if err := os.Rename(serverDir, old); err != nil {
		os.RemoveAll(staging)
		return fmt.Errorf("move current files aside: %w", err)
	}
	if err := os.Rename(staging, serverDir); err != nil {
		_ = os.Rename(old, serverDir) // roll back
		os.RemoveAll(staging)
		return fmt.Errorf("install restored files: %w", err)
	}
	_ = os.RemoveAll(old)
	_ = parent
	return nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
}

// FileName builds the archive name for a server backup.
func FileName(serverID string, at time.Time) string {
	return fmt.Sprintf("%s-%s.tar.zst", serverID, at.UTC().Format("20060102-150405"))
}

// Prune returns the file names to delete to satisfy retention, given
// backups newest first as (name, createdAt).
func Prune(settings Settings, items []Item, now time.Time) []Item {
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	var out []Item
	for i, it := range items {
		if settings.KeepLast > 0 && i >= settings.KeepLast {
			out = append(out, it)
			continue
		}
		if settings.KeepDays > 0 && now.Sub(it.CreatedAt) > time.Duration(settings.KeepDays)*24*time.Hour {
			out = append(out, it)
		}
	}
	return out
}

// Item is the minimum Prune needs to know about a backup.
type Item struct {
	ID        int64
	FileName  string
	CreatedAt time.Time
}

// CheckWritable verifies the backup destination exists and accepts files.
func CheckWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return fmt.Errorf("%s is not writable: %w", dir, err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

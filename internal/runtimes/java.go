// Package runtimes downloads interpreters (Java now; Node and Python later)
// into <data>/runtimes so servers never depend on what the host has installed.
package runtimes

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cypek019/cypek.dev---panel/internal/providers"
)

// Java manages Temurin JREs under Dir (e.g. /var/lib/homepanel/runtimes).
// Layout: <Dir>/java-<feature>/bin/java.
type Java struct {
	Dir      string
	Adoptium *providers.Adoptium

	mu sync.Mutex // one install at a time keeps disk and bandwidth sane
}

// Logf receives human-readable progress lines for the install log.
type Logf func(format string, args ...any)

// Path returns the java binary for a feature version if it is installed.
func (j *Java) Path(feature int) (string, bool) {
	p := filepath.Join(j.Dir, fmt.Sprintf("java-%d", feature), "bin", "java")
	st, err := os.Stat(p)
	return p, err == nil && st.Mode()&0o111 != 0
}

// Ensure returns the path to a java binary of the given feature version,
// downloading and extracting a Temurin JRE when needed.
func (j *Java) Ensure(ctx context.Context, feature int, logf Logf, progress providers.Progress) (string, error) {
	if p, ok := j.Path(feature); ok {
		logf("Java %d already installed (%s)", feature, p)
		return p, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if p, ok := j.Path(feature); ok {
		return p, nil
	}
	build, err := j.Adoptium.Latest(ctx, feature)
	if err != nil {
		return "", err
	}
	logf("Downloading Java %d (%s, %d MB)", feature, build.ReleaseName, build.Size/1024/1024)
	if err := os.MkdirAll(j.Dir, 0o750); err != nil {
		return "", err
	}
	archive := filepath.Join(j.Dir, fmt.Sprintf(".java-%d.tar.gz", feature))
	defer os.Remove(archive)
	if err := providers.Download(ctx, build.URL, archive, build.SHA256, progress); err != nil {
		return "", err
	}
	logf("Checksum OK, extracting")
	target := filepath.Join(j.Dir, fmt.Sprintf("java-%d", feature))
	tmp := target + ".tmp"
	os.RemoveAll(tmp)
	if err := extractTarGz(archive, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("extract Java %d: %w", feature, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	p, ok := j.Path(feature)
	if !ok {
		return "", fmt.Errorf("Java %d archive did not contain bin/java", feature)
	}
	logf("Java %d ready", feature)
	return p, nil
}

// extractTarGz unpacks a Temurin archive, dropping its single top-level
// directory (jdk-21.0.13+9-jre/) so bin/java lands directly under dest.
// Every entry path is validated against dest (no "..", no absolute paths,
// symlinks only within the tree).
func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	destAbs, _ := filepath.Abs(dest)
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		rel := stripTop(h.Name)
		if rel == "" {
			continue
		}
		target, err := within(destAbs, rel)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(h.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Allow only links that stay inside the extracted tree.
			linkTarget := filepath.Join(filepath.Dir(target), h.Linkname)
			if _, err := within(destAbs, mustRel(destAbs, linkTarget)); err != nil {
				return fmt.Errorf("symlink %s -> %s escapes the archive root", rel, h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		default:
			// Hard links, devices etc. are not expected in a JRE archive.
			continue
		}
	}
}

func stripTop(name string) string {
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return ""
}

// within joins rel under root and ensures the result stays inside root.
func within(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path %q in archive", rel)
	}
	p := filepath.Join(root, rel)
	if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the archive root", rel)
	}
	return p, nil
}

func mustRel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return ".." // forces within() to fail
	}
	return r
}

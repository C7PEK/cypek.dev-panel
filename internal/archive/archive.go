// Package archive extracts tar.gz, tar.xz and zip files safely: every entry
// is confined to the destination (no "..", no absolute paths, symlinks only
// within the tree). Used for runtimes, game artifacts and the file manager.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

// Options tune extraction.
type Options struct {
	// Strip removes this many leading path components (like tar --strip-components).
	Strip int
}

// Extract unpacks archivePath into dest, picking the format from the file name.
func Extract(archivePath, dest string, opts Options) error {
	name := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(name, ".zip"):
		return extractZip(archivePath, dest, opts)
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return extractTar(archivePath, dest, opts, func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) })
	case strings.HasSuffix(name, ".tar.xz"), strings.HasSuffix(name, ".txz"):
		return extractTar(archivePath, dest, opts, func(r io.Reader) (io.Reader, error) { return xz.NewReader(r) })
	case strings.HasSuffix(name, ".tar"):
		return extractTar(archivePath, dest, opts, func(r io.Reader) (io.Reader, error) { return r, nil })
	default:
		return fmt.Errorf("unsupported archive type: %s", filepath.Base(archivePath))
	}
}

// Within joins rel under root and ensures the result stays inside root.
func Within(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path %q in archive", rel)
	}
	root = filepath.Clean(root)
	p := filepath.Join(root, rel)
	if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the destination", rel)
	}
	return p, nil
}

// stripped validates an entry name and removes strip leading components.
// Names with ".." segments or a leading "/" are rejected outright rather than
// normalised: an archive that tries is hostile and should not be installed.
func stripped(name string, strip int) (string, bool, error) {
	slash := filepath.ToSlash(name)
	if strings.HasPrefix(slash, "/") {
		return "", false, fmt.Errorf("absolute path %q in archive", name)
	}
	for _, seg := range strings.Split(slash, "/") {
		if seg == ".." {
			return "", false, fmt.Errorf("path %q in archive contains \"..\"", name)
		}
	}
	clean := strings.TrimPrefix(path.Clean("/"+slash), "/")
	if clean == "" || clean == "." {
		return "", false, nil
	}
	parts := strings.Split(clean, "/")
	if len(parts) <= strip {
		return "", false, nil
	}
	return filepath.FromSlash(strings.Join(parts[strip:], "/")), true, nil
}

func extractTar(archivePath, dest string, opts Options, wrap func(io.Reader) (io.Reader, error)) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := wrap(f)
	if err != nil {
		return err
	}
	if c, ok := r.(io.Closer); ok {
		defer c.Close()
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		rel, ok, err := stripped(h.Name, opts.Strip)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		target, err := Within(destAbs, rel)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, dirMode(h.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(target, tr, fileMode(h.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := symlink(destAbs, target, h.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			linkRel, _, err := stripped(h.Linkname, opts.Strip)
			if err != nil {
				return err
			}
			src, err := Within(destAbs, linkRel)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Link(src, target); err != nil {
				return err
			}
		default:
			// devices, fifos: never expected in game archives
			continue
		}
	}
}

func extractZip(archivePath, dest string, opts Options) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		return err
	}
	for _, zf := range zr.File {
		rel, ok, err := stripped(zf.Name, opts.Strip)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		target, err := Within(destAbs, rel)
		if err != nil {
			return err
		}
		mode := zf.Mode()
		switch {
		case zf.FileInfo().IsDir() || strings.HasSuffix(zf.Name, "/"):
			if err := os.MkdirAll(target, dirMode(int64(mode.Perm()))); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			link, err := io.ReadAll(io.LimitReader(rc, 4096))
			rc.Close()
			if err != nil {
				return err
			}
			if err := symlink(destAbs, target, string(link)); err != nil {
				return err
			}
		default:
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			err = writeFile(target, rc, fileMode(int64(mode.Perm())))
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func writeFile(target string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	// Replace, never follow, an existing symlink at the target.
	if st, err := os.Lstat(target); err == nil && st.Mode()&os.ModeSymlink != 0 {
		os.Remove(target)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// symlink creates target -> linkname only when the resolved link stays in root.
func symlink(root, target, linkname string) error {
	if filepath.IsAbs(linkname) {
		return fmt.Errorf("symlink %s -> %s: absolute link target", target, linkname)
	}
	resolved := filepath.Join(filepath.Dir(target), linkname)
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("symlink %s -> %s escapes the destination", target, linkname)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	os.Remove(target)
	return os.Symlink(linkname, target)
}

func fileMode(m int64) os.FileMode {
	mode := os.FileMode(m) & 0o777
	if mode == 0 {
		mode = 0o644
	}
	return mode | 0o600 // always readable/writable by the owner
}

func dirMode(m int64) os.FileMode {
	mode := os.FileMode(m) & 0o777
	if mode == 0 {
		mode = 0o755
	}
	return mode | 0o700
}

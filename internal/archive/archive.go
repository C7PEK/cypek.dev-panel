package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

type Options struct {
	Strip int

	Progress func(entries int)

	MaxBytes int64

	Cancel func() bool

	Written func(n int64)

	NoLinks bool

	Resolve func(root, userPath string) (string, error)

	Keep func(rel string) bool
}

func (o Options) kept(target, rel string) bool {
	if o.Keep == nil || !o.Keep(filepath.ToSlash(rel)) {
		return false
	}
	_, err := os.Lstat(target)
	return err == nil
}

func (o Options) check(root, rel string) error {
	if o.Resolve == nil {
		return nil
	}
	dir := filepath.Dir(rel)
	if dir == "." {
		return nil
	}
	if _, err := o.Resolve(root, filepath.ToSlash(dir)); err != nil {
		return fmt.Errorf("unsafe path %q in archive: %w", rel, err)
	}
	return nil
}

func (o Options) tick(n int) {
	if o.Progress != nil && n%200 == 0 {
		o.Progress(n)
	}
}

var ErrCancelled = errors.New("extraction cancelled")

var ErrTooLarge = errors.New("the archive unpacks to more than the allowed size")

func (o Options) cancelled() bool { return o.Cancel != nil && o.Cancel() }

type budget struct {
	opts Options
	n    int64
}

func (b *budget) reader(r io.Reader) io.Reader { return &budgetReader{b: b, r: r} }

type budgetReader struct {
	b *budget
	r io.Reader
}

func (br *budgetReader) Read(p []byte) (int, error) {
	n, err := br.r.Read(p)
	br.b.n += int64(n)
	if br.b.opts.MaxBytes > 0 && br.b.n > br.b.opts.MaxBytes {
		return n, ErrTooLarge
	}
	if br.b.opts.Written != nil && n > 0 {
		br.b.opts.Written(br.b.n)
	}
	return n, err
}

func Extract(archivePath, dest string, opts Options) error {
	name := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(name, ".zip"):
		return extractZip(archivePath, dest, opts)
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return extractTar(archivePath, dest, opts, func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) })
	case strings.HasSuffix(name, ".tar.xz"), strings.HasSuffix(name, ".txz"):
		return extractTar(archivePath, dest, opts, xzReader)
	case strings.HasSuffix(name, ".tar.zst"), strings.HasSuffix(name, ".tzst"):
		return extractTar(archivePath, dest, opts, func(r io.Reader) (io.Reader, error) {
			d, err := zstd.NewReader(r)
			if err != nil {
				return nil, err
			}
			return d.IOReadCloser(), nil
		})
	case strings.HasSuffix(name, ".tar"):
		return extractTar(archivePath, dest, opts, func(r io.Reader) (io.Reader, error) { return r, nil })
	default:
		return fmt.Errorf("unsupported archive type: %s", filepath.Base(archivePath))
	}
}

var UseSystemXz = true

func xzReader(r io.Reader) (io.Reader, error) {
	if UseSystemXz {
		if path, err := exec.LookPath("xz"); err == nil {
			cmd := exec.Command(path, "-d", "-c")
			cmd.Stdin = r
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.StdoutPipe()
			if err == nil {
				if err := cmd.Start(); err == nil {
					return &cmdReader{ReadCloser: out, cmd: cmd, stderr: &stderr}, nil
				}
			}
		}
	}
	return xz.NewReader(r)
}

type cmdReader struct {
	io.ReadCloser
	cmd    *exec.Cmd
	stderr *bytes.Buffer
}

func (c *cmdReader) Close() error {
	c.ReadCloser.Close()
	if err := c.cmd.Wait(); err != nil {
		return fmt.Errorf("xz: %v: %s", err, strings.TrimSpace(c.stderr.String()))
	}
	return nil
}

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
	bud := &budget{opts: opts}
	n := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		n++
		opts.tick(n)
		if opts.cancelled() {
			return ErrCancelled
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
		if err := opts.check(destAbs, rel); err != nil {
			return err
		}
		if opts.kept(target, rel) {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := safeDir(destAbs, target); err != nil {
				return err
			}
			_ = os.Chmod(target, dirMode(h.Mode))
		case tar.TypeReg:
			if err := writeFile(destAbs, target, bud.reader(tr), fileMode(h.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if opts.NoLinks {
				return fmt.Errorf("archive contains a link (%s); links are not allowed here", h.Name)
			}
			if err := symlink(destAbs, target, h.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			if opts.NoLinks {
				return fmt.Errorf("archive contains a link (%s); links are not allowed here", h.Name)
			}
			linkRel, _, err := stripped(h.Linkname, opts.Strip)
			if err != nil {
				return err
			}
			if _, err := Within(destAbs, linkRel); err != nil {
				return err
			}

			if err := copyWithin(destAbs, linkRel, target); err != nil {
				return fmt.Errorf("hardlink %s: %w", h.Name, err)
			}
		default:
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
	bud := &budget{opts: opts}
	for i, zf := range zr.File {
		opts.tick(i + 1)
		if opts.cancelled() {
			return ErrCancelled
		}
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
		if err := opts.check(destAbs, rel); err != nil {
			return err
		}
		if opts.kept(target, rel) {
			continue
		}
		mode := zf.Mode()
		switch {
		case zf.FileInfo().IsDir() || strings.HasSuffix(zf.Name, "/"):
			if err := safeDir(destAbs, target); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			if opts.NoLinks {
				return fmt.Errorf("archive contains a link (%s); links are not allowed here", zf.Name)
			}
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
			err = writeFile(destAbs, target, bud.reader(rc), fileMode(int64(mode.Perm())))
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func writeFile(root, target string, r io.Reader, mode os.FileMode) error {
	if err := safeDir(root, filepath.Dir(target)); err != nil {
		return err
	}

	if st, err := os.Lstat(target); err == nil && !st.IsDir() {
		os.Remove(target)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func symlink(root, target, linkname string) error {
	if err := safeDir(root, filepath.Dir(target)); err != nil {
		return err
	}
	os.Remove(target)
	return os.Symlink(linkname, target)
}

func safeDir(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	cur := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		st, err := os.Lstat(cur)
		switch {
		case err == nil && st.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("refusing to write through symlink %s", cur)
		case err == nil && !st.IsDir():
			return fmt.Errorf("%s exists and is not a directory", cur)
		case err != nil:
			if err := os.Mkdir(cur, 0o755); err != nil && !os.IsExist(err) {
				return err
			}
		}
	}
	return nil
}

func fileMode(m int64) os.FileMode {
	mode := os.FileMode(m) & 0o777
	if mode == 0 {
		mode = 0o644
	}
	return mode | 0o600
}

func dirMode(m int64) os.FileMode {
	mode := os.FileMode(m) & 0o777
	if mode == 0 {
		mode = 0o755
	}
	return mode | 0o700
}

func copyWithin(root, srcRel, target string) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	in, err := r.OpenFile(filepath.Clean(srcRel), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", srcRel)
	}
	return writeFile(root, target, in, st.Mode().Perm())
}

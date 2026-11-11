package files

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/C7PEK/cypek.dev-panel/internal/archive"
)

type Incoming struct {
	root   *os.Root
	dirRel string
	tmp    string
	f      *os.File
}

func (m *Manager) NewIncoming(dir string) (*Incoming, error) {
	abs, rel, err := m.resolve(dir)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, mapErr(err)
	}
	if !st.IsDir() {
		return nil, ErrNotDir
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	return &Incoming{root: r, dirRel: rel}, nil
}

func (in *Incoming) Create() (io.Writer, error) {
	for i := 0; i < 5; i++ {
		var b [8]byte
		_, _ = rand.Read(b[:])
		in.tmp = ".download-" + hex.EncodeToString(b[:]) + ".part"
		f, err := in.root.OpenFile(in.tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o640)
		if err == nil {
			in.f = f
			return f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	return nil, errors.New("could not create a temporary file")
}

func (in *Incoming) Commit(name string) (string, error) {
	defer in.root.Close()
	if err := in.f.Close(); err != nil {
		_ = in.root.Remove(in.tmp)
		return "", err
	}
	if checkName(name) != nil || strings.HasPrefix(name, ".") {
		name = "download"
	}
	ext := path.Ext(name)
	if strings.HasSuffix(strings.ToLower(name), ".tar.gz") || strings.HasSuffix(strings.ToLower(name), ".tar.xz") || strings.HasSuffix(strings.ToLower(name), ".tar.zst") {
		ext = name[len(name)-len(".tar.gz"):]
		if strings.HasSuffix(strings.ToLower(name), ".tar.zst") {
			ext = name[len(name)-len(".tar.zst"):]
		}
	}
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		cand := name
		if i > 1 {
			cand = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		if _, err := in.root.Lstat(cand); err == nil {
			continue
		}
		if err := in.root.Rename(in.tmp, cand); err != nil {
			_ = in.root.Remove(in.tmp)
			return "", err
		}
		return path.Join(in.dirRel, cand), nil
	}
	_ = in.root.Remove(in.tmp)
	return "", ErrExists
}

func (in *Incoming) Abort() {
	if in.f != nil {
		in.f.Close()
		_ = in.root.Remove(in.tmp)
	}
	in.root.Close()
}

type ExtractOptions struct {
	MaxBytes int64
	Cancel   func() bool
	Written  func(n int64)
}

func (m *Manager) ExtractWith(archivePath, dirPath string, o ExtractOptions) error {
	src, _, err := m.resolve(archivePath)
	if err != nil {
		return err
	}
	dst, _, err := m.resolve(dirPath)
	if err != nil {
		return err
	}
	if st, err := os.Stat(src); err != nil || st.IsDir() {
		return ErrNotFound
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	opts := m.extractOptions()
	opts.MaxBytes, opts.Cancel, opts.Written = o.MaxBytes, o.Cancel, o.Written
	return archive.Extract(src, dst, opts)
}

func IsArchive(name string) bool {
	n := strings.ToLower(filepath.Base(name))
	for _, ext := range []string{".zip", ".tar", ".tar.gz", ".tgz", ".tar.xz", ".txz", ".tar.zst", ".tzst"} {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	return false
}

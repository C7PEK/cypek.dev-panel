// Package safefs reads and writes files inside a directory the panel does
// not trust: game servers run as the same user and can plant symlinks,
// hardlinks, FIFOs or huge files in their folder. Every operation goes
// through os.Root, so the kernel resolves paths beneath the root (no
// symlink can lead outside it, and there is no check-then-use race).
package safefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// ErrNotRegular is returned for FIFOs, devices, sockets and directories.
var ErrNotRegular = errors.New("not a regular file")

// ErrTooLarge is returned when a file exceeds the read limit.
var ErrTooLarge = errors.New("file is too large")

// ReadFile reads rel beneath root: regular files only (a FIFO would block
// the panel, /dev/zero would exhaust its memory), at most max bytes.
func ReadFile(root, rel string, max int64) ([]byte, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.OpenFile(filepath.Clean(rel), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", rel, ErrNotRegular)
	}
	if st.Size() > max {
		return nil, fmt.Errorf("%s: %w (%d bytes)", rel, ErrTooLarge, st.Size())
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s: %w", rel, ErrTooLarge)
	}
	return b, nil
}

// WriteFile replaces rel beneath root atomically: the data goes to a new
// temp file with a random name (O_EXCL, so a planted name cannot be
// reused), which is then renamed over rel. Renaming replaces a symlink at
// rel instead of writing through it, and a hardlinked target is never
// modified in place. Parent folders are created when missing.
func WriteFile(root, rel string, data []byte, perm fs.FileMode) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	rel = filepath.Clean(rel)
	if dir := filepath.Dir(rel); dir != "." {
		if err := r.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	var tmp string
	var f *os.File
	for i := 0; i < 5; i++ {
		var rnd [8]byte
		_, _ = rand.Read(rnd[:])
		tmp = filepath.Join(filepath.Dir(rel), "."+filepath.Base(rel)+".tmp-"+hex.EncodeToString(rnd[:]))
		f, err = r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, perm)
		if err == nil || !errors.Is(err, fs.ErrExist) {
			break
		}
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = r.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = r.Remove(tmp)
		return err
	}
	if err := r.Rename(tmp, rel); err != nil {
		_ = r.Remove(tmp)
		return err
	}
	return nil
}

// Remove deletes rel beneath root (a symlink itself, never its target).
func Remove(root, rel string) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Remove(filepath.Clean(rel))
}

// OpenRegular opens rel beneath root for reading; regular files only.
// The caller closes the file.
func OpenRegular(root, rel string) (*os.File, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.OpenFile(filepath.Clean(rel), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s: %w", rel, ErrNotRegular)
	}
	return f, nil
}

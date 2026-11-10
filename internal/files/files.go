// Package files is the server file manager. Every user-supplied path goes
// through ResolveSafe, which confines it to the server directory even in the
// presence of symlinks. The .homepanel/ directory (run.json, shim state) is
// invisible and read-only from here on purpose: it controls what gets
// executed and belongs to the settings permission, not files.write.
package files

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/archive"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
)

var (
	ErrOutsideRoot = errors.New("path escapes the server directory")
	ErrProtected   = errors.New("this path is managed by the panel")
	ErrNotFound    = errors.New("not found")
	ErrExists      = errors.New("already exists")
	ErrIsDir       = errors.New("is a directory")
	ErrNotDir      = errors.New("not a directory")
	ErrTooLarge    = errors.New("file too large")
	ErrBadName     = errors.New("invalid name")
)

// MaxEditSize bounds files opened in the editor.
const MaxEditSize = 2 << 20

// Entry describes one directory entry.
type Entry struct {
	Name       string `json:"name"`
	Type       string `json:"type"` // file | dir | symlink | other
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
	Mode       string `json:"mode"`
	// Editable is true for files small enough for the editor.
	Editable bool `json:"editable,omitempty"`
}

// Manager operates on one server directory.
type Manager struct {
	Root string
}

// New returns a Manager rooted at the (existing) server directory.
func New(root string) *Manager { return &Manager{Root: filepath.Clean(root)} }

// Clean normalises a user path to a slash-separated relative path ("" = root).
// Any ".." segment is rejected outright rather than normalised away, so the
// path a user sees is the path that gets touched.
func Clean(userPath string) (string, error) {
	p := strings.ReplaceAll(userPath, "\\", "/")
	if strings.ContainsRune(p, 0) {
		return "", ErrBadName
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", ErrOutsideRoot
		}
	}
	p = path.Clean("/" + p)
	p = strings.TrimPrefix(p, "/")
	if p == "." {
		p = ""
	}
	return p, nil
}

// ResolveSafe maps userPath to an absolute path under root. It cleans the
// path, joins it, resolves symlinks for the longest existing prefix and
// checks the result stays inside root. The returned path is safe to open;
// nothing in it is a symlink pointing outside.
func ResolveSafe(root, userPath string) (string, error) {
	rel, err := Clean(userPath)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	full := filepath.Join(rootReal, filepath.FromSlash(rel))
	// Resolve the deepest existing ancestor so a symlinked directory in the
	// middle of the path cannot lead outside.
	existing := full
	var missing []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", ErrOutsideRoot
		}
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", rel, err)
	}
	resolved := filepath.Join(append([]string{real}, missing...)...)
	if resolved != rootReal && !strings.HasPrefix(resolved, rootReal+string(filepath.Separator)) {
		return "", ErrOutsideRoot
	}
	return resolved, nil
}

// resolve applies ResolveSafe and the .homepanel protection.
func (m *Manager) resolve(userPath string) (abs, rel string, err error) {
	rel, err = Clean(userPath)
	if err != nil {
		return "", "", err
	}
	if rel == shim.MetaDir || strings.HasPrefix(rel, shim.MetaDir+"/") {
		return "", "", ErrProtected
	}
	abs, err = ResolveSafe(m.Root, rel)
	if err != nil {
		return "", "", err
	}
	// The resolved path may still land in .homepanel through a symlink.
	rootReal, _ := filepath.EvalSymlinks(m.Root)
	if r, err := filepath.Rel(rootReal, abs); err == nil && (r == shim.MetaDir || strings.HasPrefix(r, shim.MetaDir+string(filepath.Separator))) {
		return "", "", ErrProtected
	}
	return abs, rel, nil
}

// entry resolves the parent folder safely but not the last element: a
// symlink is the thing itself (renaming or deleting a link must never act
// on its target, e.g. "l -> ." would otherwise delete the whole server).
func (m *Manager) entry(userPath string) (abs, rel string, err error) {
	rel, err = Clean(userPath)
	if err != nil {
		return "", "", err
	}
	if rel == "" {
		return "", "", ErrProtected
	}
	parentAbs, _, err := m.resolve(path.Dir(rel))
	if err != nil && path.Dir(rel) != "." {
		return "", "", err
	}
	if path.Dir(rel) == "." {
		parentAbs, err = filepath.EvalSymlinks(m.Root)
		if err != nil {
			return "", "", err
		}
	}
	base := path.Base(rel)
	if path.Dir(rel) == "." && base == shim.MetaDir {
		return "", "", ErrProtected
	}
	return filepath.Join(parentAbs, base), rel, nil
}

// List returns the entries of a directory, directories first.
func (m *Manager) List(userPath string) ([]Entry, error) {
	abs, _, err := m.resolve(userPath)
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
	dirents, err := os.ReadDir(abs)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Entry, 0, len(dirents))
	for _, d := range dirents {
		if d.Name() == shim.MetaDir && userPath == "" {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue
		}
		e := Entry{Name: d.Name(), Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339), Mode: info.Mode().Perm().String()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			e.Type = "symlink"
			if target, err := os.Stat(filepath.Join(abs, d.Name())); err == nil && target.IsDir() {
				e.Type = "dir"
			}
		case info.IsDir():
			e.Type = "dir"
		case info.Mode().IsRegular():
			e.Type = "file"
			e.Editable = info.Size() <= MaxEditSize
		default:
			e.Type = "other"
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := out[i].Type == "dir", out[j].Type == "dir"
		if di != dj {
			return di
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Stat returns info about one path.
func (m *Manager) Stat(userPath string) (Entry, error) {
	abs, rel, err := m.resolve(userPath)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Entry{}, mapErr(err)
	}
	e := Entry{Name: path.Base("/" + rel), Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339), Mode: info.Mode().Perm().String(), Type: "file"}
	if info.IsDir() {
		e.Type = "dir"
	} else {
		e.Editable = info.Mode().IsRegular() && info.Size() <= MaxEditSize
	}
	return e, nil
}

// Open returns a reader for a regular file (any size) plus its size.
func (m *Manager) Open(userPath string) (io.ReadCloser, int64, error) {
	abs, _, err := m.resolve(userPath)
	if err != nil {
		return nil, 0, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, 0, mapErr(err)
	}
	if st.IsDir() {
		return nil, 0, ErrIsDir
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, 0, mapErr(err)
	}
	return f, st.Size(), nil
}

// ReadText reads a file for the editor, refusing anything above MaxEditSize.
func (m *Manager) ReadText(userPath string) ([]byte, error) {
	f, size, err := m.Open(userPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if size > MaxEditSize {
		return nil, ErrTooLarge
	}
	return io.ReadAll(io.LimitReader(f, MaxEditSize+1))
}

// WriteText replaces a file's content atomically (temp file + rename).
func (m *Manager) WriteText(userPath string, data []byte) error {
	abs, rel, err := m.resolve(userPath)
	if err != nil {
		return err
	}
	if rel == "" {
		return ErrIsDir
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		return ErrIsDir
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), "."+filepath.Base(abs)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o640)
	if st, err := os.Stat(abs); err == nil {
		mode = st.Mode().Perm()
	}
	_ = os.Chmod(tmp.Name(), mode)
	return os.Rename(tmp.Name(), abs)
}

// Create writes an uploaded stream to dir/name; existing files are replaced.
// maxSize caps the upload; the partial file is removed on overflow.
func (m *Manager) Create(dirPath, name string, r io.Reader, maxSize int64) (int64, error) {
	if err := checkName(name); err != nil {
		return 0, err
	}
	dirAbs, _, err := m.resolve(dirPath)
	if err != nil {
		return 0, err
	}
	if st, err := os.Stat(dirAbs); err != nil || !st.IsDir() {
		return 0, ErrNotDir
	}
	abs, _, err := m.resolve(path.Join(dirPath, name))
	if err != nil {
		return 0, err
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		return 0, ErrIsDir
	}
	tmp, err := os.CreateTemp(dirAbs, "."+name+".upload-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, io.LimitReader(r, maxSize+1))
	if err != nil {
		tmp.Close()
		return 0, err
	}
	if n > maxSize {
		tmp.Close()
		return 0, ErrTooLarge
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	_ = os.Chmod(tmp.Name(), 0o640)
	return n, os.Rename(tmp.Name(), abs)
}

// Mkdir creates a directory (and parents) inside the root.
func (m *Manager) Mkdir(userPath string) error {
	abs, rel, err := m.resolve(userPath)
	if err != nil {
		return err
	}
	if rel == "" {
		return ErrExists
	}
	if _, err := os.Lstat(abs); err == nil {
		return ErrExists
	}
	return os.MkdirAll(abs, 0o750)
}

// Rename moves a file or directory within the root; refuses to overwrite.
func (m *Manager) Rename(from, to string) error {
	src, srcRel, err := m.entry(from)
	if err != nil {
		return err
	}
	dst, dstRel, err := m.entry(to)
	if err != nil {
		return err
	}
	if srcRel == "" || dstRel == "" {
		return ErrProtected
	}
	if err := checkName(path.Base(dstRel)); err != nil {
		return err
	}
	if _, err := os.Lstat(src); err != nil {
		return mapErr(err)
	}
	if _, err := os.Lstat(dst); err == nil {
		return ErrExists
	}
	if strings.HasPrefix(dst, src+string(filepath.Separator)) {
		return fmt.Errorf("cannot move a directory into itself")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// Delete removes a file, or a directory recursively. Symlinks are removed,
// never followed.
func (m *Manager) Delete(userPath string) error {
	abs, rel, err := m.entry(userPath)
	if err != nil {
		return err
	}
	if rel == "" {
		return ErrProtected
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return mapErr(err)
	}
	if st.IsDir() {
		return os.RemoveAll(abs)
	}
	return os.Remove(abs)
}

// Extract unpacks an archive inside the root into dirPath (zip, tar.gz, tar.xz).
func (m *Manager) Extract(archivePath, dirPath string) error {
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
	return archive.Extract(src, dst, m.extractOptions())
}

// extractOptions: uploaded archives may not create links (a link to "/" or
// to another server would turn later panel operations against the host)
// nor write into .homepanel (run.json decides what the server executes).
func (m *Manager) extractOptions() archive.Options {
	rootReal, _ := filepath.EvalSymlinks(m.Root)
	return archive.Options{NoLinks: true, Resolve: func(root, p string) (string, error) {
		abs, err := ResolveSafe(root, p)
		if err != nil {
			return "", err
		}
		if r, err := filepath.Rel(rootReal, abs); err == nil && (r == shim.MetaDir || strings.HasPrefix(r, shim.MetaDir+string(filepath.Separator))) {
			return "", ErrProtected
		}
		return abs, nil
	}}
}

// DiskUsage sums the sizes of regular files under the root (best effort).
func (m *Manager) DiskUsage() (int64, error) {
	var total int64
	err := filepath.WalkDir(m.Root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") || len(name) > 255 {
		return ErrBadName
	}
	return nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, os.ErrExist):
		return ErrExists
	}
	return err
}

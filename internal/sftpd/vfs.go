package sftpd

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"github.com/C7PEK/cypek.dev-panel/internal/files"
)

const metaDir = ".homepanel"

type vfs struct {
	ctx     context.Context
	backend Backend
	sess    *session
	stats   *counters

	mu       sync.Mutex
	cache    []ServerRef
	cachedAt time.Time
}

type target struct {
	root   bool
	server *ServerRef
	rel    string
	abs    string
}

func (v *vfs) servers() []ServerRef {
	v.mu.Lock()
	defer v.mu.Unlock()
	if time.Since(v.cachedAt) > 10*time.Second {
		list, err := v.backend.Servers(v.ctx, v.sess.userID)
		if err != nil {
			list = nil
		}
		v.cache, v.cachedAt = list, time.Now()
	}
	return v.cache
}

func FolderName(s ServerRef) string {
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, strings.TrimSpace(s.Name))
	if name == "" {
		return s.ID
	}
	return name + " (" + s.ID + ")"
}

func (v *vfs) resolve(p string) (*target, error) {
	clean := path.Clean("/" + p)
	list := v.servers()
	var srv *ServerRef
	rest := ""
	if v.sess.single != "" {
		for i := range list {
			if list[i].ID == v.sess.single {
				srv = &list[i]
			}
		}
		rest = strings.TrimPrefix(clean, "/")
	} else {
		if clean == "/" {
			return &target{root: true}, nil
		}
		first, after, _ := strings.Cut(strings.TrimPrefix(clean, "/"), "/")
		for i := range list {
			if FolderName(list[i]) == first || list[i].ID == first {
				srv = &list[i]
			}
		}
		rest = after
	}
	if srv == nil {
		return nil, sftp.ErrSSHFxNoSuchFile
	}
	if rest == metaDir || strings.HasPrefix(rest, metaDir+"/") {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	abs, err := files.ResolveSafe(srv.Dir, rest)
	if err != nil {
		return nil, sftp.ErrSSHFxPermissionDenied
	}

	if real, err := filepath.EvalSymlinks(srv.Dir); err == nil {
		meta := filepath.Join(real, metaDir)
		if abs == meta || strings.HasPrefix(abs, meta+string(filepath.Separator)) {
			return nil, sftp.ErrSSHFxPermissionDenied
		}
	}
	return &target{server: srv, rel: rest, abs: abs}, nil
}

func (v *vfs) resolveEntry(p string) (*target, error) {
	clean := path.Clean("/" + p)
	parent, err := v.resolve(path.Dir(clean))
	if err != nil {
		return nil, err
	}
	if parent.root {
		return v.resolve(clean)
	}
	base := path.Base(clean)
	if parent.rel == "" && base == metaDir {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	rel := base
	if parent.rel != "" {
		rel = parent.rel + "/" + base
	}
	return &target{server: parent.server, rel: rel, abs: filepath.Join(parent.abs, base)}, nil
}

func (v *vfs) writable(t *target) error {
	if t.root || t.server == nil || !v.backend.CanWrite(v.ctx, v.sess.userID, t.server.ID) {
		return sftp.ErrSSHFxPermissionDenied
	}
	return nil
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return sftp.ErrSSHFxNoSuchFile
	case errors.Is(err, os.ErrPermission), errors.Is(err, files.ErrOutsideRoot):
		return sftp.ErrSSHFxPermissionDenied
	}
	return err
}

func (v *vfs) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	t, err := v.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	if t.root {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	f, err := os.Open(t.abs)
	if err != nil {
		return nil, mapErr(err)
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		f.Close()
		return nil, sftp.ErrSSHFxFailure
	}
	v.stats.add(&v.stats.rd)
	return f, nil
}

func (v *vfs) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	t, err := v.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	if err := v.writable(t); err != nil {
		return nil, err
	}
	if t.rel == "" {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	pf := r.Pflags()
	flags := os.O_WRONLY
	if pf.Read {
		flags = os.O_RDWR
	}
	if pf.Creat || (!pf.Read && !pf.Write) {
		flags |= os.O_CREATE
	}
	if pf.Trunc {
		flags |= os.O_TRUNC
	}
	if pf.Excl {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(t.abs, flags, 0o640)
	if err != nil {
		return nil, mapErr(err)
	}
	v.stats.add(&v.stats.uploaded)
	return f, nil
}

func (v *vfs) Filecmd(r *sftp.Request) error {
	resolve := v.resolve
	switch r.Method {
	case "Rename", "PosixRename", "Rmdir", "Remove", "Mkdir":
		resolve = v.resolveEntry
	}
	t, err := resolve(r.Filepath)
	if err != nil {
		return err
	}
	if err := v.writable(t); err != nil {
		return err
	}
	switch r.Method {
	case "Setstat":
		if t.rel == "" {
			return sftp.ErrSSHFxPermissionDenied
		}
		attrs, flags := r.Attributes(), r.AttrFlags()
		if flags.Size {
			if err := os.Truncate(t.abs, int64(attrs.Size)); err != nil {
				return mapErr(err)
			}
		}
		if flags.Permissions {
			if err := os.Chmod(t.abs, os.FileMode(attrs.Mode)&0o777); err != nil {
				return mapErr(err)
			}
		}
		if flags.Acmodtime {
			if err := os.Chtimes(t.abs, time.Unix(int64(attrs.Atime), 0), time.Unix(int64(attrs.Mtime), 0)); err != nil {
				return mapErr(err)
			}
		}
		return nil
	case "Rename", "PosixRename":
		dst, err := v.resolveEntry(r.Target)
		if err != nil {
			return err
		}
		if err := v.writable(dst); err != nil {
			return err
		}
		if t.rel == "" || dst.rel == "" {
			return sftp.ErrSSHFxPermissionDenied
		}
		if r.Method == "Rename" {
			if _, err := os.Lstat(dst.abs); err == nil {
				return sftp.ErrSSHFxFailure
			}
		}
		v.stats.add(&v.stats.renamed)
		return mapErr(os.Rename(t.abs, dst.abs))
	case "Rmdir", "Remove":
		if t.rel == "" {
			return sftp.ErrSSHFxPermissionDenied
		}
		st, err := os.Lstat(t.abs)
		if err != nil {
			return mapErr(err)
		}
		if r.Method == "Remove" && st.IsDir() || r.Method == "Rmdir" && !st.IsDir() {
			return sftp.ErrSSHFxFailure
		}
		v.stats.add(&v.stats.removed)
		return mapErr(os.Remove(t.abs))
	case "Mkdir":
		if t.rel == "" {
			return sftp.ErrSSHFxFailure
		}
		v.stats.add(&v.stats.mkdir)
		return mapErr(os.Mkdir(t.abs, 0o750))
	case "Link", "Symlink":
		return sftp.ErrSSHFxPermissionDenied
	}
	return sftp.ErrSSHFxOpUnsupported
}

func (v *vfs) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	t, err := v.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	switch r.Method {
	case "List":
		if t.root {
			var out []os.FileInfo
			for _, s := range v.servers() {
				out = append(out, dirInfo{name: FolderName(s)})
			}
			sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name()) < strings.ToLower(out[j].Name()) })
			return lister(out), nil
		}
		entries, err := os.ReadDir(t.abs)
		if err != nil {
			return nil, mapErr(err)
		}
		var out []os.FileInfo
		for _, e := range entries {
			if t.rel == "" && e.Name() == metaDir {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, info)
		}
		return lister(out), nil
	case "Stat", "Lstat":
		if t.root {
			return lister{dirInfo{name: "/"}}, nil
		}
		info, err := os.Stat(t.abs)
		if err != nil {
			return nil, mapErr(err)
		}
		if t.rel == "" && v.sess.single == "" {
			return lister{dirInfo{name: FolderName(*t.server), mod: info.ModTime()}}, nil
		}
		return lister{info}, nil
	case "Readlink":
		return nil, sftp.ErrSSHFxOpUnsupported
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

type lister []os.FileInfo

func (l lister) ListAt(out []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[offset:])
	if n < len(out) {
		return n, io.EOF
	}
	return n, nil
}

type dirInfo struct {
	name string
	mod  time.Time
}

func (d dirInfo) Name() string { return d.name }
func (d dirInfo) Size() int64  { return 0 }
func (d dirInfo) Mode() os.FileMode {
	return os.ModeDir | 0o755
}
func (d dirInfo) ModTime() time.Time {
	if d.mod.IsZero() {
		return time.Now()
	}
	return d.mod
}
func (d dirInfo) IsDir() bool      { return true }
func (d dirInfo) Sys() interface{} { return nil }

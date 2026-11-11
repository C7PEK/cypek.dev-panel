package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/backups"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

const (
	CodeBackupRunning = "backup_running"
	CodeBackupsDir    = "backups_dir_unwritable"
)

type backupJob struct {
	Kind      string    `json:"kind"`
	StartedAt time.Time `json:"startedAt"`
	Files     int       `json:"files"`
	Bytes     int64     `json:"bytes"`
	Error     string    `json:"error,omitempty"`
	Done      bool      `json:"done"`
}

type backupJobs struct {
	mu   sync.Mutex
	jobs map[string]*backupJob
}

func (b *backupJobs) get(id string) *backupJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	if j, ok := b.jobs[id]; ok {
		cp := *j
		return &cp
	}
	return nil
}

func (b *backupJobs) start(id, kind string) (*backupJob, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if j, ok := b.jobs[id]; ok && !j.Done {
		return nil, false
	}
	j := &backupJob{Kind: kind, StartedAt: time.Now()}
	b.jobs[id] = j
	return j, true
}

func (b *backupJobs) update(id string, fn func(*backupJob)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if j, ok := b.jobs[id]; ok {
		fn(j)
	}
}

func (s *Server) backupDir(serverID string) string {
	return filepath.Join(s.cfg.BackupsDir(), serverID)
}

func (s *Server) createBackup(ctx context.Context, srv *servers.Server, trigger, note string, userID *int64, logf func(string, ...any)) (*backups.Record, error) {
	settings, err := s.backups.Settings(ctx, srv.ID, string(srv.Category))
	if err != nil {
		return nil, err
	}
	dir := s.backupDir(srv.ID)
	if err := backups.CheckWritable(dir); err != nil {
		return nil, err
	}
	st, _ := s.sup.Status(ctx, srv.ID)
	flushed := false
	if settings.FlushMinecraft && srv.Category == servers.CategoryMinecraft && st.State == supervisor.StateRunning {
		if c, err := s.sup.Attach(ctx, srv.ID); err == nil {
			logf("Pausing world saves (save-off, save-all)")
			_ = c.Input("save-off\n")
			_ = c.Input("save-all\n")
			c.Close()
			flushed = true
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
			}
		}
	}
	defer func() {
		if flushed {
			if c, err := s.sup.Attach(context.WithoutCancel(ctx), srv.ID); err == nil {
				_ = c.Input("save-on\n")
				c.Close()
				logf("World saves resumed (save-on)")
			}
		}
	}()
	name := backups.FileName(srv.ID, time.Now())
	logf("Writing %s", name)
	size, err := backups.Create(ctx, s.servers.Dir(srv.ID), filepath.Join(dir, name), settings, func(files int, bytes int64) {
		s.backupJobs.update(srv.ID, func(j *backupJob) { j.Files, j.Bytes = files, bytes })
	})
	if err != nil {
		return nil, err
	}
	rec, err := s.backups.Insert(ctx, &backups.Record{ServerID: srv.ID, FileName: name, Size: size, Note: note, Trigger: trigger, CreatedBy: userID})
	if err != nil {
		return nil, err
	}
	logf("Backup done: %s (%d MB)", name, size>>20)

	all, err := s.backups.List(ctx, srv.ID)
	if err == nil {
		for _, old := range backups.Prune(settings, backups.Items(all), time.Now()) {
			if old.ID == rec.ID {
				continue
			}
			_ = os.Remove(filepath.Join(dir, old.FileName))
			_ = s.backups.Delete(ctx, old.ID)
			logf("Removed old backup %s (retention)", old.FileName)
		}
	}
	return rec, nil
}

type backupsListResponse struct {
	Items    []*backups.Record `json:"items"`
	Settings backups.Settings  `json:"settings"`
	Dir      string            `json:"dir"`
	Writable bool              `json:"writable"`
	Job      *backupJob        `json:"job,omitempty"`
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	items, err := s.backups.List(r.Context(), srv.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	settings, _ := s.backups.Settings(r.Context(), srv.ID, string(srv.Category))
	writeJSON(w, http.StatusOK, backupsListResponse{
		Items: items, Settings: settings, Dir: s.backupDir(srv.ID),
		Writable: backups.CheckWritable(s.backupDir(srv.ID)) == nil, Job: s.backupJobs.get(srv.ID),
	})
}

type createBackupRequest struct {
	Note string `json:"note"`
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	var req createBackupRequest
	if r.ContentLength > 0 && !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Note) > 200 {
		writeFieldError(w, CodeValidation, "note", "Note is too long.")
		return
	}
	if err := backups.CheckWritable(s.backupDir(srv.ID)); err != nil {
		writeError(w, http.StatusConflict, CodeBackupsDir, "The backup folder is not writable: "+err.Error())
		return
	}
	if _, ok := s.backupJobs.start(srv.ID, "backup"); !ok {
		writeError(w, http.StatusConflict, CodeBackupRunning, "A backup or restore is already running for this server.")
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "backup.create", Target: srv.ID, IP: clientIP(r)})
	uid := user.ID
	go func() {
		ctx, cancel := context.WithTimeout(s.bgCtx, 6*time.Hour)
		defer cancel()
		_, err := s.createBackup(ctx, srv, "manual", req.Note, &uid, func(f string, a ...any) { s.log.Info("backup", "server", srv.ID, "msg", fmt.Sprintf(f, a...)) })
		s.backupJobs.update(srv.ID, func(j *backupJob) {
			j.Done = true
			if err != nil {
				j.Error = err.Error()
			}
		})
	}()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) backupFromRequest(w http.ResponseWriter, r *http.Request) (*backups.Record, bool) {
	srv := serverFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "Backup not found.")
		return nil, false
	}
	rec, err := s.backups.Get(r.Context(), id)
	if errors.Is(err, backups.ErrNotFound) || (err == nil && rec.ServerID != srv.ID) {
		writeError(w, http.StatusNotFound, CodeNotFound, "Backup not found.")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return rec, true
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	rec, ok := s.backupFromRequest(w, r)
	if !ok {
		return
	}
	if err := os.Remove(filepath.Join(s.backupDir(rec.ServerID), rec.FileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.internalError(w, r, err)
		return
	}
	if err := s.backups.Delete(r.Context(), rec.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "backup.delete", Target: rec.ServerID, IP: clientIP(r), Details: map[string]any{"file": rec.FileName}})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.backupFromRequest(w, r)
	if !ok {
		return
	}
	path := filepath.Join(s.backupDir(rec.ServerID), rec.FileName)
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "The backup file is missing on disk.")
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "application/zstd")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": rec.FileName}))
	http.ServeContent(w, r, rec.FileName, st.ModTime(), f)
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	rec, ok := s.backupFromRequest(w, r)
	if !ok {
		return
	}
	if st := s.statusOf(r.Context(), srv.ID); st.Active() {
		writeError(w, http.StatusConflict, CodeServerRunning, "Stop the server before restoring a backup.")
		return
	}
	path := filepath.Join(s.backupDir(rec.ServerID), rec.FileName)
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "The backup file is missing on disk.")
		return
	}
	if _, ok := s.backupJobs.start(srv.ID, "restore"); !ok {
		writeError(w, http.StatusConflict, CodeBackupRunning, "A backup or restore is already running for this server.")
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "backup.restore", Target: srv.ID, IP: clientIP(r), Details: map[string]any{"file": rec.FileName}})
	go func() {
		ctx, cancel := context.WithTimeout(s.bgCtx, 6*time.Hour)
		defer cancel()
		err := backups.Restore(ctx, path, s.servers.Dir(srv.ID))
		if err != nil {
			s.log.Error("restore failed", "server", srv.ID, "err", err)
		} else if cur, gerr := s.servers.Get(ctx, srv.ID); gerr == nil && cur.InstallState == servers.InstallReady {
			if werr := shim.SaveRunConfig(s.servers.Dir(srv.ID), &cur.Run); werr != nil {
				s.log.Warn("rewrite run config after restore", "server", srv.ID, "err", werr)
			}
		}
		s.backupJobs.update(srv.ID, func(j *backupJob) {
			j.Done = true
			if err != nil {
				j.Error = err.Error()
			}
		})
	}()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleBackupSettings(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	var st backups.Settings
	if !decodeJSON(w, r, &st) {
		return
	}
	if err := s.backups.SaveSettings(r.Context(), srv.ID, st); err != nil {
		writeFieldError(w, CodeValidation, "excludes", err.Error())
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "backup.settings", Target: srv.ID, IP: clientIP(r)})
	saved, _ := s.backups.Settings(r.Context(), srv.ID, string(srv.Category))
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) scheduledBackup(ctx context.Context, serverID string, logf func(string, ...any)) error {
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return err
	}
	if _, ok := s.backupJobs.start(serverID, "backup"); !ok {
		return errors.New("a backup or restore is already running")
	}
	_, err = s.createBackup(ctx, srv, "schedule", "scheduled", nil, logf)
	s.backupJobs.update(serverID, func(j *backupJob) {
		j.Done = true
		if err != nil {
			j.Error = err.Error()
		}
	})
	return err
}

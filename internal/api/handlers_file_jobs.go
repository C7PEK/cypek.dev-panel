package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"path"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/C7PEK/cypek.dev-panel/internal/archive"
	"github.com/C7PEK/cypek.dev-panel/internal/fetch"
	"github.com/C7PEK/cypek.dev-panel/internal/files"
	"github.com/C7PEK/cypek.dev-panel/internal/netguard"
)

const (
	CodeTooManyJobs  = "too_many_jobs"
	CodeBadLink      = "bad_link"
	maxJobsPerServer = 2
	maxJobsTotal     = 4
	keepFreeBytes    = 1 << 30
	jobTimeout       = 6 * time.Hour
	finishedJobTTL   = 2 * time.Hour
)

var downloadClient = netguard.Client(netguard.Options{})

type fileJob struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	State      string    `json:"state"`
	Phase      string    `json:"phase"`
	Name       string    `json:"name"`
	Source     string    `json:"source,omitempty"`
	Dir        string    `json:"dir"`
	File       string    `json:"file,omitempty"`
	Bytes      int64     `json:"bytes"`
	Total      int64     `json:"total"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`

	serverID string
	cancel   context.CancelFunc
}

type fileJobs struct {
	mu   sync.Mutex
	jobs map[string]*fileJob
}

func (j *fileJobs) add(job *fileJob) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.jobs == nil {
		j.jobs = map[string]*fileJob{}
	}
	perServer, total := 0, 0
	for id, x := range j.jobs {
		if x.State != "running" && time.Since(x.FinishedAt) > finishedJobTTL {
			delete(j.jobs, id)
			continue
		}
		if x.State == "running" {
			total++
			if x.serverID == job.serverID {
				perServer++
			}
		}
	}
	if perServer >= maxJobsPerServer || total >= maxJobsTotal {
		return errors.New("too many downloads or extractions are running; wait for one to finish")
	}
	j.jobs[job.ID] = job
	return nil
}

func (j *fileJobs) update(id string, f func(*fileJob)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if x, ok := j.jobs[id]; ok {
		f(x)
	}
}

func (j *fileJobs) list(serverID string) []fileJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := []fileJob{}
	for _, x := range j.jobs {
		if x.serverID == serverID {
			out = append(out, *x)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt.After(out[b].StartedAt) })
	return out
}

func (j *fileJobs) cancel(serverID, id string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	x, ok := j.jobs[id]
	if !ok || x.serverID != serverID || x.State != "running" {
		return false
	}
	x.cancel()
	return true
}

func newJobID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func freeBytes(dir string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0
	}
	free := int64(st.Bavail) * int64(st.Bsize)
	if free <= keepFreeBytes {
		return 0
	}
	return free - keepFreeBytes
}

func (s *Server) finishJob(id string, err error, ctx context.Context) {
	s.fileJobs.update(id, func(x *fileJob) {
		x.FinishedAt = time.Now()
		switch {
		case err == nil:
			x.State = "done"
		case ctx.Err() == context.Canceled || errors.Is(err, archive.ErrCancelled):
			x.State, x.Error = "cancelled", ""
		default:
			x.State, x.Error = "failed", err.Error()
		}
	})
}

type fetchInput struct {
	URL           string `json:"url"`
	Path          string `json:"path"`
	Extract       bool   `json:"extract"`
	DeleteArchive bool   `json:"deleteArchive"`
}

func (s *Server) handleFilesFetch(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	var in fetchInput
	if !decodeJSON(w, r, &in) {
		return
	}
	src, err := fetch.Resolve(in.URL)
	if err != nil {
		writeFieldError(w, CodeBadLink, "url", err.Error())
		return
	}
	m := s.filesFor(r)
	if _, err := m.Stat(in.Path); err != nil {
		s.writeFileError(w, r, err)
		return
	}
	u, _ := url.Parse(in.URL)
	ctx, cancel := context.WithTimeout(s.bgCtx, jobTimeout)
	job := &fileJob{ID: newJobID(), Kind: "download", State: "running", Phase: "downloading", Name: u.Host + u.EscapedPath(), Source: src.Kind,
		Dir: in.Path, Total: -1, StartedAt: time.Now(), serverID: srv.ID, cancel: cancel}
	if err := s.fileJobs.add(job); err != nil {
		cancel()
		writeError(w, http.StatusTooManyRequests, CodeTooManyJobs, err.Error())
		return
	}

	s.auditFile(r, "files.fetch", in.Path, map[string]any{"host": u.Host, "path": u.EscapedPath(), "source": src.Kind, "extract": in.Extract})
	serverDir := s.servers.Dir(srv.ID)
	snapshot := *job
	go func() {
		defer cancel()
		incoming, err := m.NewIncoming(in.Path)
		if err != nil {
			s.finishJob(job.ID, err, ctx)
			return
		}
		file, err := fetch.Download(ctx, downloadClient, src, incoming, freeBytes(serverDir), func(done, total int64) {
			s.fileJobs.update(job.ID, func(x *fileJob) { x.Bytes, x.Total = done, total })
		})
		if err != nil {
			s.finishJob(job.ID, err, ctx)
			return
		}
		s.fileJobs.update(job.ID, func(x *fileJob) { x.File = file; x.Name = path.Base(file) })
		if in.Extract && files.IsArchive(file) {
			s.fileJobs.update(job.ID, func(x *fileJob) { x.Phase, x.Bytes, x.Total = "extracting", 0, -1 })
			err = s.extractJob(ctx, m, job.ID, file, path.Dir(file), serverDir)
			if err == nil && in.DeleteArchive {
				_ = m.Delete(file)
			}
		}
		s.finishJob(job.ID, err, ctx)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"job": snapshot})
}

func (s *Server) extractJob(ctx context.Context, m *files.Manager, jobID, archivePath, to, serverDir string) error {
	return m.ExtractWith(archivePath, to, files.ExtractOptions{
		MaxBytes: freeBytes(serverDir),
		Cancel:   func() bool { return ctx.Err() != nil },
		Written: func(n int64) {
			s.fileJobs.update(jobID, func(x *fileJob) { x.Bytes = n })
		},
	})
}

func (s *Server) handleFilesExtract(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	var req extractRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.To == "" {
		req.To = path.Dir("/" + req.Path)
	}
	m := s.filesFor(r)
	if _, err := m.Stat(req.Path); err != nil {
		s.writeFileError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(s.bgCtx, jobTimeout)
	job := &fileJob{ID: newJobID(), Kind: "extract", State: "running", Phase: "extracting", Name: path.Base(req.Path), Dir: req.To,
		File: req.Path, Total: -1, StartedAt: time.Now(), serverID: srv.ID, cancel: cancel}
	if err := s.fileJobs.add(job); err != nil {
		cancel()
		writeError(w, http.StatusTooManyRequests, CodeTooManyJobs, err.Error())
		return
	}
	s.auditFile(r, "files.extract", req.Path, map[string]any{"to": req.To})
	serverDir := s.servers.Dir(srv.ID)
	snapshot := *job
	go func() {
		defer cancel()
		s.finishJob(job.ID, s.extractJob(ctx, m, job.ID, req.Path, req.To, serverDir), ctx)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"job": snapshot})
}

func (s *Server) handleFileJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.fileJobs.list(serverFromContext(r.Context()).ID)})
}

func (s *Server) handleCancelFileJob(w http.ResponseWriter, r *http.Request) {
	if !s.fileJobs.cancel(serverFromContext(r.Context()).ID, chi.URLParam(r, "jid")) {
		writeError(w, http.StatusNotFound, CodeNotFound, "No such running job.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

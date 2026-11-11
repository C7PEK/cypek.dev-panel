package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/selfupdate"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

const (
	updateUnit       = "homepanel-update.service"
	CodeNoSelfUpdate = "self_update_unavailable"
)

var latestCache struct {
	sync.Mutex
	at  time.Time
	rel *selfupdate.Release
	err error
}

func (s *Server) latestRelease(ctx context.Context, fresh bool) (*selfupdate.Release, error) {
	c := &latestCache
	c.Lock()
	defer c.Unlock()
	if !fresh && time.Since(c.at) < 15*time.Minute {
		return c.rel, c.err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c.rel, c.err = selfupdate.New().Latest(ctx)
	c.at = time.Now()
	return c.rel, c.err
}

func (s *Server) updateStatusPath() string {
	return filepath.Join(s.cfg.DataDir, "update-status.json")
}

func (s *Server) handleUpdateInfo(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"current": s.version, "repo": selfupdate.Repo}
	rel, err := s.latestRelease(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["latest"] = rel.Tag
		out["notes"] = rel.Notes
		out["publishedAt"] = rel.PublishedAt
		out["available"] = selfupdate.Newer(s.version, rel.Tag)
	}
	if st := selfupdate.ReadStatus(s.updateStatusPath()); st != nil {
		out["last"] = st
	}
	out["canUpdate"] = false
	if a, ok := s.addons(); ok {
		if active, _, err := a.AddonState(r.Context(), updateUnit); err == nil {
			out["canUpdate"] = true
			out["running"] = active == "active" || active == "activating"
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleStartUpdate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.addons()
	if !ok {
		writeError(w, http.StatusConflict, CodeNoSelfUpdate, "This panel runs without systemd (dev mode); update it from the terminal with: sudo homepanel update")
		return
	}
	if err := a.StartAddon(r.Context(), updateUnit); err != nil {
		if errors.Is(err, supervisor.ErrNoSystemd) {
			writeError(w, http.StatusConflict, CodeNoSelfUpdate, "Update it from the terminal with: sudo homepanel update")
			return
		}
		writeError(w, http.StatusBadGateway, CodeSupervisor, "Could not start the update: "+err.Error()+". Run 'sudo homepanel install-files' once so the update unit exists, or update from the terminal.")
		return
	}
	_, user := sessionFromContext(r.Context())
	s.audit.Log(context.WithoutCancel(r.Context()), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "panel.update", Target: "panel", IP: clientIP(r), Details: map[string]any{"from": s.version}})
	w.WriteHeader(http.StatusAccepted)
}

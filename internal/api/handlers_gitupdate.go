package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/installer"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

const (
	CodeNoGit    = "no_git_source"
	CodeNoUpdate = "no_update"
)

func (s *Server) handleGitUpdate(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	tpl, ok := s.templates.Get(srv.TemplateID)
	if !ok || !isTemplateServer(srv) || !tpl.UsesGit() {
		writeError(w, http.StatusConflict, CodeNoGit, "This server does not get its code from git.")
		return
	}
	s.runUpdate(w, r, srv, tpl, "server.git_update")
}

func (s *Server) handleGameUpdate(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	tpl, ok := s.templates.Get(srv.TemplateID)
	if !ok || !isTemplateServer(srv) || !tpl.HasUpdate() {
		writeError(w, http.StatusConflict, CodeNoUpdate, "This server has no automatic update. Reinstall it in Settings instead.")
		return
	}
	upd := *tpl
	upd.Install = tpl.Update
	s.runUpdate(w, r, srv, &upd, "server.game_update")
}

func (s *Server) runUpdate(w http.ResponseWriter, r *http.Request, srv *servers.Server, tpl *templates.Template, action string) {
	_, user := sessionFromContext(r.Context())
	var in struct {
		Restart *bool `json:"restart"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &in) {
		return
	}
	restart := in.Restart == nil || *in.Restart
	if srv.InstallState == servers.InstallInstalling || s.installer.Running(srv.ID) {
		writeError(w, http.StatusConflict, CodeInstalling, "An installation or update is already running.")
		return
	}
	userVars, _, err := s.splitVars(tpl, srv.Variables)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	wasRunning := s.statusOf(r.Context(), srv.ID).Active()
	stopTimeout := time.Duration(srv.Run.Stop.TimeoutSec+30) * time.Second
	hooks := installer.Hooks{
		Finished: "Update finished.",
		Before: func(ctx context.Context, logf func(string, ...any)) error {
			if !s.statusOf(ctx, srv.ID).Active() {
				return nil
			}
			logf("Stopping the server for the update")
			sctx, cancel := context.WithTimeout(ctx, stopTimeout)
			defer cancel()
			if err := s.sup.Stop(sctx, srv.ID); err != nil {
				return fmt.Errorf("could not stop the server: %w", err)
			}
			return nil
		},
	}
	then := func(ok bool, jobErr error) {
		if !ok {
			_ = s.servers.SetInstallState(s.bgCtx, srv.ID, servers.InstallReady, "")
		}
		if !restart && !wasRunning {
			return
		}
		s.startAfterInstall(srv.ID)
	}
	if err := s.startInstallWith(srv, tpl, userVars, hooks, then); err != nil {
		if errors.Is(err, installer.ErrInstallRunning) {
			writeError(w, http.StatusConflict, CodeInstalling, "An installation or update is already running.")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: action, Target: srv.ID, IP: clientIP(r),
		Details: map[string]any{"restart": restart, "wasRunning": wasRunning}})
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) startAfterInstall(id string) {
	ctx, cancel := context.WithTimeout(s.bgCtx, 60*time.Second)
	defer cancel()
	cur, err := s.servers.Get(ctx, id)
	if err != nil || s.servers.CheckStartable(ctx, id) != nil {
		return
	}
	s.applyConfigDefaults(cur)
	if err := s.sup.Start(ctx, id); err != nil {
		s.log.Warn("start after install", "server", id, "err", err)
	}
}

package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/installer"
	"github.com/C7PEK/cypek.dev-panel/internal/providers"
	"github.com/C7PEK/cypek.dev-panel/internal/query"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

const (
	CodeNotInstalled  = "not_installed"
	CodeInstalling    = "installing"
	CodeUpstream      = "upstream_error"
	CodeTemplateError = "template_error"
)

const customTemplateID = "custom"

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	list := s.templates.List()
	out := make([]templateItem, 0, len(list))
	for _, t := range list {
		out = append(out, s.templateItem(t))
	}
	resp := map[string]any{"items": out}
	if errs := s.templates.LoadErrors(); len(errs) > 0 {
		resp["loadErrors"] = errs
	}
	writeJSON(w, http.StatusOK, resp)
}

type sourceOption struct {
	Value      string `json:"value"`
	Label      string `json:"label"`
	Prerelease bool   `json:"prerelease,omitempty"`
}

func (s *Server) handleTemplateSource(w http.ResponseWriter, r *http.Request) {
	tpl, ok := s.templates.Get(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, CodeNotFound, "Unknown template.")
		return
	}
	source := chi.URLParam(r, "source")
	uses := false
	for _, v := range tpl.Variables {
		if v.Source == source {
			uses = true
		}
	}
	if !uses {
		writeError(w, http.StatusNotFound, CodeNotFound, "This template has no such option source.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	opts, err := s.sourceOptions(ctx, source)
	if err != nil {
		writeError(w, http.StatusBadGateway, CodeUpstream, "Could not fetch the version list: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": opts})
}

var (
	forgeClient    = providers.NewForge()
	neoForgeClient = providers.NewNeoForge()
)

func (s *Server) sourceOptions(ctx context.Context, source string) ([]sourceOption, error) {
	switch source {
	case "paper-versions":
		all, err := s.paper.Versions(ctx)
		if err != nil {
			return nil, err
		}
		stable := providers.StableVersions(all)
		opts := []sourceOption{{Value: "latest", Label: "latest"}}
		if len(stable) > 0 {
			opts[0].Label = "latest (" + stable[0] + ")"
		}
		for _, v := range all {
			opts = append(opts, sourceOption{Value: v, Label: v, Prerelease: providers.IsPrerelease(v)})
		}
		return opts, nil
	case "folia-versions", "velocity-versions":
		project := strings.TrimSuffix(source, "-versions")
		all, err := s.paper.ForProject(project).Versions(ctx)
		if err != nil {
			return nil, err
		}
		stable := providers.StableVersions(all)
		opts := []sourceOption{{Value: "latest", Label: "latest"}}
		if len(stable) > 0 {
			opts[0].Label = "latest (" + stable[0] + ")"
		}
		for _, v := range all {
			opts = append(opts, sourceOption{Value: v, Label: v, Prerelease: providers.IsPrerelease(v)})
		}
		return opts, nil
	case "minecraft-versions", "fabric-versions", "purpur-versions", "forge-versions", "neoforge-versions", "quilt-versions":
		var (
			list []string
			err  error
		)
		switch source {
		case "minecraft-versions":
			list, err = providers.NewMojang().Releases(ctx)
		case "fabric-versions":
			list, err = providers.NewFabric().GameVersions(ctx)
		case "forge-versions":
			list, err = forgeClient.GameVersions(ctx)
		case "neoforge-versions":
			list, err = neoForgeClient.GameVersions(ctx)
		case "quilt-versions":
			list, err = providers.NewQuilt().GameVersions(ctx)
		default:
			list, err = providers.NewPurpur().Versions(ctx)
		}
		if err != nil {
			return nil, err
		}
		opts := []sourceOption{{Value: "latest", Label: "latest"}}
		if len(list) > 0 {
			opts[0].Label = "latest (" + list[0] + ")"
		}
		for _, v := range list {
			opts = append(opts, sourceOption{Value: v, Label: v})
		}
		return opts, nil
	case "fivem-artifacts":
		items, err := s.fivem.Options(ctx)
		if err != nil {
			return nil, err
		}
		opts := make([]sourceOption, 0, len(items))
		for _, it := range items {
			opts = append(opts, sourceOption{Value: it.Value, Label: it.Label})
		}
		return opts, nil
	default:
		return nil, errors.New("unknown source " + source)
	}
}

func (s *Server) validateSourceValue(ctx context.Context, v *templates.Variable, value string) error {
	if v.Source == "" {
		return nil
	}
	opts, err := s.sourceOptions(ctx, v.Source)
	if err != nil {
		return err
	}
	for _, o := range opts {
		if o.Value == value {
			return nil
		}
	}
	return &templates.ValueError{Key: v.Key, Message: "is not an available option"}
}

func (s *Server) startInstall(srv *servers.Server, tpl *templates.Template, userVars map[string]string) error {
	return s.startInstallWith(srv, tpl, userVars, installer.Hooks{}, nil)
}

func (s *Server) startInstallWith(srv *servers.Server, tpl *templates.Template, userVars map[string]string, hooks installer.Hooks, then func(ok bool, err error)) error {
	dir := s.servers.Dir(srv.ID)
	ctx := s.bgCtx
	_ = s.servers.SetInstallState(ctx, srv.ID, servers.InstallInstalling, "")
	return s.installer.StartWith(ctx, srv.ID, dir, tpl, userVars, hooks, func(res *installer.Result, err error) {
		if then != nil {
			defer func() { then(err == nil, err) }()
		}
		if err != nil {
			s.log.Warn("install failed", "server", srv.ID, "err", err)
			_ = s.servers.SetInstallState(ctx, srv.ID, servers.InstallFailed, err.Error())
			return
		}
		cur, gerr := s.servers.Get(ctx, srv.ID)
		if gerr != nil {
			s.log.Warn("install finished but server is gone", "server", srv.ID, "err", gerr)
			return
		}
		cur.Run = res.Run
		cur.Ports = res.Ports
		if cur.Variables == nil {
			cur.Variables = map[string]string{}
		}
		for k, v := range res.Builtins {
			cur.Variables[k] = v
		}
		cur.InstallState = servers.InstallReady
		cur.InstallError = ""
		if err := s.servers.Update(ctx, cur); err != nil {
			s.log.Error("save installed server", "server", srv.ID, "err", err)
			_ = s.servers.SetInstallState(ctx, srv.ID, servers.InstallFailed, "could not save the server configuration: "+err.Error())
			return
		}
		s.log.Info("install finished", "server", srv.ID, "name", cur.Name)
	})
}

func (s *Server) splitVars(tpl *templates.Template, stored map[string]string) (user, builtins map[string]string, err error) {
	user, builtins = map[string]string{}, map[string]string{}
	for k, v := range stored {
		if tv := tpl.Variable(k); tv != nil {
			if tv.Type == templates.TypeSecret {
				if v, err = s.secrets.Open(v); err != nil {
					return nil, nil, err
				}
			}
			user[k] = v
		} else {
			builtins[k] = v
		}
	}
	return user, builtins, nil
}

func maskedVariables(tpl *templates.Template, stored map[string]string) map[string]string {
	out := map[string]string{}
	if tpl == nil {
		return out
	}
	for k, v := range stored {
		tv := tpl.Variable(k)
		if tv == nil {
			continue
		}
		if tv.Type == templates.TypeSecret && v != "" {
			v = "••••••••"
		}
		out[k] = v
	}
	return out
}

func (s *Server) handleInstallStatus(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	job, ok := s.installer.Get(srv.ID)
	if !ok {
		state := installer.JobDone
		if srv.InstallState == servers.InstallFailed {
			state = installer.JobFailed
		} else if srv.InstallState != servers.InstallReady {
			state = installer.JobRunning
		}
		job = &installer.Job{ServerID: srv.ID, State: state, Error: srv.InstallError, Lines: []string{}, Percent: -1}
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleReinstall(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	tpl, ok := s.templates.Get(srv.TemplateID)
	if !ok || srv.TemplateID == customTemplateID {
		writeError(w, http.StatusConflict, CodeTemplateError, "This server was not created from a template.")
		return
	}
	if st := s.statusOf(r.Context(), srv.ID); st.Active() {
		writeError(w, http.StatusConflict, CodeServerRunning, "Stop the server before reinstalling.")
		return
	}
	userVars, _, err := s.splitVars(tpl, srv.Variables)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.startInstall(srv, tpl, userVars); err != nil {
		if errors.Is(err, installer.ErrInstallRunning) {
			writeError(w, http.StatusConflict, CodeInstalling, "Installation is already running.")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "server.reinstall", Target: srv.ID, IP: clientIP(r)})
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) recoverInstalls(ctx context.Context) {
	list, err := s.servers.List(ctx, nil)
	if err != nil {
		return
	}
	for _, srv := range list {
		if srv.InstallState == servers.InstallInstalling || srv.InstallState == servers.InstallPending {
			_ = s.servers.SetInstallState(ctx, srv.ID, servers.InstallFailed, "The panel was restarted during installation. Run the installation again.")
		}
	}
}

type consoleHints struct {
	Commands []templates.Command `json:"commands"`
	Players  *query.Players      `json:"players,omitempty"`
}

func (s *Server) handleConsoleHints(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	hints := consoleHints{Commands: []templates.Command{}}
	if tpl, ok := s.templates.Get(srv.TemplateID); ok && isTemplateServer(srv) {
		hints.Commands = tpl.Console.Commands
		if hints.Commands == nil {
			hints.Commands = []templates.Command{}
		}
	}
	if st := s.statusOf(r.Context(), srv.ID); st.State == supervisor.StateRunning {
		hints.Players = s.playersOf(r.Context(), srv)
	}
	writeJSON(w, http.StatusOK, hints)
}

func (s *Server) warnCommandFor(ctx context.Context, serverID, message string) (string, bool, error) {
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return "", false, err
	}
	tpl, ok := s.templates.Get(srv.TemplateID)
	if !ok || !isTemplateServer(srv) || tpl.WarnCommand == "" {
		return "", false, nil
	}
	cmd, err := templates.Render(tpl.WarnCommand, map[string]string{templates.KeyMessage: message})
	if err != nil {
		return "", false, err
	}
	return cmd, true, nil
}

func (s *Server) applyConfigDefaults(srv *servers.Server) {
	if !isTemplateServer(srv) {
		return
	}
	tpl, ok := s.templates.Get(srv.TemplateID)
	if !ok {
		return
	}
	s.syncStopConfig(srv, tpl)
	changed, err := templates.ApplyConfigDefaults(s.servers.Dir(srv.ID), tpl)
	if err != nil {
		s.log.Warn("config defaults", "server", srv.ID, "err", err)
	}
	for _, f := range changed {
		s.log.Info("config default added", "server", srv.ID, "file", f)
	}
	if len(tpl.ConfigFiles) == 0 {
		return
	}

	user, builtins, err := s.splitVars(tpl, srv.Variables)
	if err != nil {
		s.log.Warn("config files: read variables", "server", srv.ID, "err", err)
		return
	}
	vars := builtins
	for k, v := range user {
		vars[k] = v
	}
	dir, _ := filepath.Abs(s.servers.Dir(srv.ID))
	vars[templates.KeyServerDir] = dir
	files, err := templates.ApplyConfigFiles(dir, tpl, vars)
	if err != nil {
		s.log.Warn("config files", "server", srv.ID, "err", err)
	}
	for _, f := range files {
		s.log.Info("config file updated", "server", srv.ID, "file", f)
	}
}

func (s *Server) beforeStart(ctx context.Context, id string) {
	if srv, err := s.servers.Get(ctx, id); err == nil {
		s.applyConfigDefaults(srv)
	}
}

func isTemplateServer(srv *servers.Server) bool {
	return srv.TemplateID != "" && srv.TemplateID != customTemplateID
}

func (s *Server) syncStopConfig(srv *servers.Server, tpl *templates.Template) {
	want := shim.StopConfig{Command: tpl.Stop.Command, TimeoutSec: tpl.Stop.TimeoutSec}
	if want.TimeoutSec <= 0 {
		want.TimeoutSec = srv.Run.Stop.TimeoutSec
	}
	if srv.InstallState != servers.InstallReady || srv.Run.Stop == want {
		return
	}
	srv.Run.Stop = want
	if err := s.servers.Update(s.bgCtx, srv); err != nil {
		s.log.Warn("sync stop command from template", "server", srv.ID, "err", err)
		return
	}
	s.log.Info("stop command updated from template", "server", srv.ID, "command", want.Command)
}

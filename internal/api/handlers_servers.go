package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cypek019/cypek.dev---panel/internal/audit"
	"github.com/cypek019/cypek.dev---panel/internal/auth"
	"github.com/cypek019/cypek.dev---panel/internal/query"
	"github.com/cypek019/cypek.dev---panel/internal/servers"
	"github.com/cypek019/cypek.dev---panel/internal/shim"
	"github.com/cypek019/cypek.dev---panel/internal/supervisor"
	"github.com/cypek019/cypek.dev---panel/internal/templates"
)

const (
	CodeServerRunning   = "server_running"
	CodeInvalidAction   = "invalid_action"
	CodeSupervisor      = "supervisor_error"
	CodeCommandNotFound = "command_not_found"
)

const ctxServer ctxKey = 10

type serverResponse struct {
	*servers.Server
	Status    supervisor.Status `json:"status"`
	Variables map[string]string `json:"variables"`
	Players   *query.Players    `json:"players,omitempty"`
}

func (s *Server) toResponse(ctx context.Context, srv *servers.Server) serverResponse {
	var tpl *templates.Template
	if isTemplateServer(srv) {
		tpl, _ = s.templates.Get(srv.TemplateID)
	}
	st := s.statusOf(ctx, srv.ID)
	resp := serverResponse{Server: srv, Status: st, Variables: maskedVariables(tpl, srv.Variables)}
	if st.State == supervisor.StateRunning {
		resp.Players = s.playersOf(ctx, srv)
	}
	return resp
}

// playersOf queries the game for its player count when the template told us
// how; nil when unsupported or the server does not answer (still starting).
func (s *Server) playersOf(ctx context.Context, srv *servers.Server) *query.Players {
	kind := srv.Variables[templates.KeyQueryKind]
	port, _ := strconv.Atoi(srv.Variables[templates.KeyQueryPort])
	if kind == "" || port == 0 {
		// Servers installed before the query builtins existed: derive the
		// spec from the template and the stored user variables.
		tpl, ok := s.templates.Get(srv.TemplateID)
		if !ok || !isTemplateServer(srv) {
			return nil
		}
		k, p, has, err := templates.RenderQuery(tpl, srv.Variables)
		if err != nil || !has {
			return nil
		}
		kind, port = k, p
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	p, err := s.players.Get(ctx, srv.ID, query.Spec{Kind: kind, Host: "127.0.0.1", Port: port})
	if err != nil {
		return nil
	}
	return p
}

// serverInput mirrors the Server JSON. On create the required fields must be
// present; on patch only the provided ones change. Pointers tell the two apart.
type serverInput struct {
	Name      *string         `json:"name"`
	Run       *runInput       `json:"run"`
	Limits    *limitsInput    `json:"limits"`
	Autostart *bool           `json:"autostart"`
	Ports     *[]servers.Port `json:"ports"`
	// TemplateID + Variables create a server from a template (create only);
	// Variables alone reconfigure a template server (patch).
	TemplateID *string            `json:"templateId"`
	Variables  *map[string]string `json:"variables"`
}

type runInput struct {
	Command    *string            `json:"command"`
	Args       *[]string          `json:"args"`
	Env        *map[string]string `json:"env"`
	Stop       *stopInput         `json:"stop"`
	ReadyRegex *string            `json:"readyRegex"`
}

type stopInput struct {
	Command    *string `json:"command"`
	TimeoutSec *int    `json:"timeoutSec"`
}

type limitsInput struct {
	MemoryMB   *int `json:"memoryMb"`
	CPUPercent *int `json:"cpuPercent"`
}

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// apply validates in and merges it into srv. It returns the offending field
// path and a message on validation failure.
func (in *serverInput) apply(srv *servers.Server, creating bool) (field, msg string) {
	if in.Name != nil {
		if err := servers.ValidateName(*in.Name); err != nil {
			return "name", "Name must be 1–64 characters."
		}
		srv.Name = strings.TrimSpace(*in.Name)
	} else if creating {
		return "name", "Name is required."
	}
	if in.Run == nil && creating && !in.fromTemplate() {
		return "run.command", "Command is required."
	}
	if in.Run != nil {
		if f, m := in.Run.apply(&srv.Run, creating); f != "" {
			return f, m
		}
	}
	if in.Limits != nil {
		if in.Limits.MemoryMB != nil {
			if *in.Limits.MemoryMB < 0 || *in.Limits.MemoryMB > 1<<20 {
				return "limits.memoryMb", "Memory limit must be 0 (unlimited) or a positive number of MB."
			}
			srv.Limits.MemoryMB = *in.Limits.MemoryMB
		}
		if in.Limits.CPUPercent != nil {
			if *in.Limits.CPUPercent < 0 || *in.Limits.CPUPercent > 100*256 {
				return "limits.cpuPercent", "CPU limit must be 0 (unlimited) or a percentage (100 = one core)."
			}
			srv.Limits.CPUPercent = *in.Limits.CPUPercent
		}
	}
	if in.Autostart != nil {
		srv.Autostart = *in.Autostart
	}
	if in.Ports != nil {
		if len(*in.Ports) > 64 {
			return "ports", "Too many ports."
		}
		for i, p := range *in.Ports {
			if p.Port < 1 || p.Port > 65535 || (p.Protocol != "tcp" && p.Protocol != "udp" && p.Protocol != "tcp+udp") {
				return "ports", "Port " + itoa(i) + " must be 1–65535 with protocol tcp, udp or tcp+udp."
			}
		}
		srv.Ports = *in.Ports
	}
	return "", ""
}

func (in *serverInput) fromTemplate() bool {
	return in.TemplateID != nil && *in.TemplateID != "" && *in.TemplateID != customTemplateID
}

func (in *runInput) apply(run *shim.RunConfig, creating bool) (field, msg string) {
	if in.Command != nil {
		c := strings.TrimSpace(*in.Command)
		if c == "" || len(c) > 1024 || strings.ContainsAny(c, "\n\r\x00") {
			return "run.command", "Command is required: a program name or an absolute path, without shell syntax."
		}
		run.Command = c
	} else if creating {
		return "run.command", "Command is required."
	}
	if in.Args != nil {
		args := make([]string, 0, len(*in.Args))
		for _, a := range *in.Args {
			if len(a) > 4096 || strings.ContainsRune(a, 0) {
				return "run.args", "An argument is too long or contains invalid characters."
			}
			args = append(args, a)
		}
		if len(args) > 256 {
			return "run.args", "Too many arguments."
		}
		run.Args = args
	}
	if run.Args == nil {
		run.Args = []string{}
	}
	if in.Env != nil {
		env := map[string]string{}
		for k, v := range *in.Env {
			if !envKeyRe.MatchString(k) || len(v) > 8192 || strings.ContainsRune(v, 0) {
				return "run.env", "Environment variable names must look like MY_VAR and values must be plain text."
			}
			env[k] = v
		}
		if len(env) > 128 {
			return "run.env", "Too many environment variables."
		}
		run.Env = env
	}
	if in.Stop != nil {
		if in.Stop.Command != nil {
			sc := strings.TrimSpace(*in.Stop.Command)
			if len(sc) > 256 || strings.ContainsAny(sc, "\n\r\x00") {
				return "run.stop.command", "Stop command must be a single line."
			}
			run.Stop.Command = sc
		}
		if in.Stop.TimeoutSec != nil {
			if *in.Stop.TimeoutSec < 1 || *in.Stop.TimeoutSec > 3600 {
				return "run.stop.timeoutSec", "Stop timeout must be between 1 and 3600 seconds."
			}
			run.Stop.TimeoutSec = *in.Stop.TimeoutSec
		}
	}
	if in.ReadyRegex != nil {
		re := strings.TrimSpace(*in.ReadyRegex)
		if len(re) > 512 {
			return "run.readyRegex", "Ready pattern is too long."
		}
		if re != "" {
			if _, err := regexp.Compile(re); err != nil {
				return "run.readyRegex", "Ready pattern is not a valid regular expression: " + err.Error()
			}
		}
		run.ReadyRegex = re
	}
	return "", ""
}

func itoa(i int) string { return strconv.Itoa(i) }

// serverFromContext returns the server loaded by withServer.
func serverFromContext(ctx context.Context) *servers.Server {
	srv, _ := ctx.Value(ctxServer).(*servers.Server)
	return srv
}

// withServer loads {id}, enforces the per-server permission and stores the
// server in the context. Every /servers/{id} route goes through it.
func (s *Server) withServer(perm auth.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, user := sessionFromContext(r.Context())
			id := chi.URLParam(r, "id")
			srv, err := s.servers.Get(r.Context(), id)
			if errors.Is(err, servers.ErrNotFound) {
				writeError(w, http.StatusNotFound, CodeNotFound, "Server not found.")
				return
			}
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			ok, err := s.store.Require(r.Context(), user, perm, srv.ID)
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			if key := apiKeyFromContext(r); ok && key != nil && !key.AllowsPermission(perm) {
				writeError(w, http.StatusForbidden, CodeForbidden, "This API key is not allowed to "+string(perm)+".")
				return
			}
			if !ok {
				writeError(w, http.StatusForbidden, CodeForbidden, "You do not have permission to do this on this server.")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxServer, srv)))
		})
	}
}

// statusOf fetches state plus live counters; failures degrade to "unknown"
// rather than failing the whole request.
func (s *Server) statusOf(ctx context.Context, id string) supervisor.Status {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	st, err := s.sup.Status(ctx, id)
	if err != nil {
		s.log.Debug("status", "server", id, "err", err)
		return supervisor.Status{State: supervisor.StateUnknown}
	}
	if st.Active() && st.MemoryBytes == 0 {
		if stats, err := s.sup.Stats(ctx, id); err == nil {
			st.MemoryBytes, st.CPUNanos = stats.MemoryBytes, stats.CPUNanos
		}
	}
	return st
}

func (s *Server) respondServer(w http.ResponseWriter, r *http.Request, status int, srv *servers.Server) {
	writeJSON(w, status, s.toResponse(r.Context(), srv))
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	ids, all, err := s.store.VisibleServerIDs(r.Context(), user)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !all && ids == nil {
		ids = []string{}
	}
	list, err := s.servers.List(r.Context(), ids)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]serverResponse, 0, len(list))
	for _, srv := range list {
		items = append(items, s.toResponse(r.Context(), srv))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	s.respondServer(w, r, http.StatusOK, serverFromContext(r.Context()))
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	var in serverInput
	if !decodeJSON(w, r, &in) {
		return
	}
	srv := &servers.Server{TemplateID: customTemplateID, Category: servers.CategoryOther, CreatedBy: &user.ID}
	if field, msg := in.apply(srv, true); field != "" {
		writeFieldError(w, CodeValidation, field, msg)
		return
	}
	var (
		tpl      *templates.Template
		userVars map[string]string
	)
	if in.fromTemplate() {
		var ok bool
		tpl, ok = s.templates.Get(*in.TemplateID)
		if !ok {
			writeFieldError(w, CodeValidation, "templateId", "Unknown template.")
			return
		}
		given := map[string]string{}
		if in.Variables != nil {
			given = *in.Variables
		}
		vals, err := tpl.ValidateValues(given)
		if err != nil {
			var ve *templates.ValueError
			if errors.As(err, &ve) {
				writeFieldError(w, CodeValidation, "variables."+ve.Key, ve.Message)
				return
			}
			s.internalError(w, r, err)
			return
		}
		for i := range tpl.Variables {
			v := &tpl.Variables[i]
			if err := s.validateSourceValue(r.Context(), v, vals[v.Key]); err != nil {
				var ve *templates.ValueError
				if errors.As(err, &ve) {
					writeFieldError(w, CodeValidation, "variables."+ve.Key, ve.Message)
					return
				}
				writeError(w, http.StatusBadGateway, CodeUpstream, "Could not verify "+v.Key+": "+err.Error())
				return
			}
		}
		userVars = vals
		srv.TemplateID = tpl.ID
		srv.Category = tpl.Category
		srv.InstallState = servers.InstallPending
		srv.Variables = map[string]string{}
		for k, v := range vals {
			if tpl.Variable(k).Type == templates.TypeSecret {
				if v, err = s.secrets.Seal(v); err != nil {
					s.internalError(w, r, err)
					return
				}
			}
			srv.Variables[k] = v
		}
	}
	created, err := s.servers.Create(r.Context(), srv)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if tpl != nil {
		if err := s.startInstall(created, tpl, userVars); err != nil {
			s.log.Error("start install", "server", created.ID, "err", err)
			_ = s.servers.SetInstallState(r.Context(), created.ID, servers.InstallFailed, err.Error())
		}
	}
	if err := s.applySupervisorSettings(r.Context(), created, in.Limits != nil, in.Autostart != nil); err != nil {
		s.log.Warn("apply supervisor settings after create", "server", created.ID, "err", err)
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "server.create", Target: created.ID, IP: clientIP(r),
		Details: map[string]any{"name": created.Name, "command": created.Run.Command}})
	s.respondServer(w, r, http.StatusCreated, created)
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	var in serverInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if field, msg := in.apply(srv, false); field != "" {
		writeFieldError(w, CodeValidation, field, msg)
		return
	}
	if in.Variables != nil {
		if !isTemplateServer(srv) {
			writeFieldError(w, CodeValidation, "variables", "This server has no template variables; edit the command instead.")
			return
		}
		if srv.InstallState != servers.InstallReady {
			writeError(w, http.StatusConflict, CodeNotInstalled, "Finish the installation before changing settings.")
			return
		}
		if code, field, msg, err := s.applyVariables(r.Context(), srv, *in.Variables); err != nil {
			s.internalError(w, r, err)
			return
		} else if msg != "" {
			if field != "" {
				writeFieldError(w, code, field, msg)
			} else {
				writeError(w, http.StatusBadGateway, code, msg)
			}
			return
		}
	}
	if err := s.servers.Update(r.Context(), srv); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "server.update", Target: srv.ID, IP: clientIP(r)})
	if err := s.applySupervisorSettings(r.Context(), srv, in.Limits != nil, in.Autostart != nil); err != nil {
		writeError(w, http.StatusBadGateway, CodeSupervisor, "Settings were saved, but applying limits or autostart failed: "+err.Error())
		return
	}
	s.respondServer(w, r, http.StatusOK, srv)
}

// applyVariables validates new template variables, re-applies the
// template's config steps and rebuilds the run config in place. It returns a
// user-facing (code, field, message) on validation problems.
func (s *Server) applyVariables(ctx context.Context, srv *servers.Server, given map[string]string) (code, field, msg string, err error) {
	tpl, ok := s.templates.Get(srv.TemplateID)
	if !ok {
		return CodeTemplateError, "", "The template this server was created from is no longer available.", nil
	}
	current, builtins, err := s.splitVars(tpl, srv.Variables)
	if err != nil {
		return "", "", "", err
	}
	// Secrets shown masked come back masked: keep the stored value then.
	merged := map[string]string{}
	for k, v := range current {
		merged[k] = v
	}
	for k, v := range given {
		if tv := tpl.Variable(k); tv != nil && tv.Type == templates.TypeSecret && v == "••••••••" {
			continue
		}
		merged[k] = v
	}
	vals, err := tpl.ValidateValues(merged)
	if err != nil {
		var ve *templates.ValueError
		if errors.As(err, &ve) {
			return CodeValidation, "variables." + ve.Key, ve.Message, nil
		}
		return "", "", "", err
	}
	for i := range tpl.Variables {
		v := &tpl.Variables[i]
		if v.Source == "" || vals[v.Key] == current[v.Key] {
			continue // unchanged dynamic values need no upstream round trip
		}
		if err := s.validateSourceValue(ctx, v, vals[v.Key]); err != nil {
			var ve *templates.ValueError
			if errors.As(err, &ve) {
				return CodeValidation, "variables." + ve.Key, ve.Message, nil
			}
			return CodeUpstream, "", "Could not verify " + v.Key + ": " + err.Error(), nil
		}
	}
	res, err := s.installer.Reconfigure(s.servers.Dir(srv.ID), tpl, vals, builtins)
	if err != nil {
		return CodeTemplateError, "", "Could not apply the new settings: " + err.Error(), nil
	}
	srv.Run = res.Run
	srv.Ports = res.Ports
	srv.Variables = map[string]string{}
	for k, v := range vals {
		if tpl.Variable(k).Type == templates.TypeSecret {
			if v, err = s.secrets.Seal(v); err != nil {
				return "", "", "", err
			}
		}
		srv.Variables[k] = v
	}
	for k, v := range res.Builtins {
		srv.Variables[k] = v
	}
	return "", "", "", nil
}

// applySupervisorSettings pushes limits and autostart to the supervisor when
// those fields were part of the request.
func (s *Server) applySupervisorSettings(ctx context.Context, srv *servers.Server, limits, autostart bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if limits {
		if err := s.sup.SetLimits(ctx, srv.ID, srv.Limits); err != nil {
			return err
		}
	}
	if autostart {
		if err := s.sup.SetAutostart(ctx, srv.ID, srv.Autostart); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	if st := s.statusOf(r.Context(), srv.ID); st.Active() {
		writeError(w, http.StatusConflict, CodeServerRunning, "Stop the server before deleting it.")
		return
	}
	if s.installer.Running(srv.ID) {
		writeError(w, http.StatusConflict, CodeInstalling, "Wait for the installation to finish before deleting the server.")
		return
	}
	defer s.installer.Forget(srv.ID)
	removeFiles := r.URL.Query().Get("files") != "0"
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.sup.Remove(ctx, srv.ID); err != nil {
		s.log.Warn("supervisor remove", "server", srv.ID, "err", err)
	}
	if removeFiles {
		s.dropServerDatabases(r.Context(), srv.ID)
	}
	if err := s.servers.Delete(r.Context(), srv.ID, removeFiles); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "server.delete", Target: srv.ID, IP: clientIP(r),
		Details: map[string]any{"name": srv.Name, "files": removeFiles}})
	w.WriteHeader(http.StatusNoContent)
}

type powerRequest struct {
	Action string `json:"action"`
}

func (s *Server) handlePower(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	var req powerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var timeout time.Duration
	switch req.Action {
	case "start":
		timeout = 30 * time.Second
	case "stop":
		timeout = time.Duration(srv.Run.Stop.TimeoutSec+30) * time.Second
	case "restart":
		timeout = time.Duration(srv.Run.Stop.TimeoutSec+60) * time.Second
	case "kill":
		timeout = 20 * time.Second
	default:
		writeFieldError(w, CodeInvalidAction, "action", "Action must be start, stop, restart or kill.")
		return
	}
	if req.Action == "start" || req.Action == "restart" {
		if srv.InstallState != servers.InstallReady {
			writeError(w, http.StatusConflict, CodeNotInstalled, "The server is not installed yet. Check the installation status.")
			return
		}
		s.applyConfigDefaults(srv)
		if msg := servers.CommandProblem(srv.Run.Command, s.servers.Dir(srv.ID)); msg != "" {
			writeFieldError(w, CodeCommandNotFound, "run.command", msg)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	var err error
	switch req.Action {
	case "start":
		err = s.sup.Start(ctx, srv.ID)
	case "stop":
		err = s.sup.Stop(ctx, srv.ID)
	case "restart":
		err = s.sup.Restart(ctx, srv.ID)
	case "kill":
		err = s.sup.Kill(ctx, srv.ID)
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "server." + req.Action, Target: srv.ID, IP: clientIP(r),
		Details: map[string]any{"ok": err == nil}})
	switch {
	case err == nil:
	case errors.Is(err, supervisor.ErrAlreadyRunning):
		writeError(w, http.StatusConflict, CodeConflict, "The server is already running.")
		return
	case errors.Is(err, supervisor.ErrNotRunning):
		writeError(w, http.StatusConflict, CodeConflict, "The server is not running.")
		return
	default:
		s.log.Error("power action failed", "server", srv.ID, "action", req.Action, "err", err)
		writeError(w, http.StatusBadGateway, CodeSupervisor, "Could not "+req.Action+" the server: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": s.statusOf(r.Context(), srv.ID)})
}

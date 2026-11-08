package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cypek019/cypek.dev---panel/internal/audit"
	"github.com/cypek019/cypek.dev---panel/internal/database"
	"github.com/cypek019/cypek.dev---panel/internal/supervisor"
)

const (
	CodeDBNotInstalled = "database_not_installed"
	CodeDBUnavailable  = "database_unavailable"
	CodeDBExists       = "database_exists"
	CodeDBInstalling   = "database_installing"
	CodeNoSystemd      = "addon_needs_systemd"
)

type dbInstallState struct {
	Running bool     `json:"running"`
	Done    bool     `json:"done"`
	Failed  bool     `json:"failed"`
	Lines   []string `json:"lines"`
}

func (s *Server) addons() (supervisor.AddonController, bool) {
	a, ok := s.sup.(supervisor.AddonController)
	return a, ok
}

func (s *Server) dbInstallState(ctx context.Context) dbInstallState {
	lines, done, failed := database.ReadInstallLog(database.InstallLog)
	st := dbInstallState{Done: done, Failed: failed, Lines: lines}
	if st.Lines == nil {
		st.Lines = []string{}
	}
	if a, ok := s.addons(); ok {
		if active, _, err := a.AddonState(ctx, database.InstallUnit); err == nil {
			st.Running = active == "activating"
		}
	}
	return st
}

// handleDatabaseStatus reports whether MariaDB/phpMyAdmin are installed and
// the installer's progress.
func (s *Server) handleDatabaseStatus(w http.ResponseWriter, r *http.Request) {
	st := s.databases.Status(r.Context())
	resp := map[string]any{"status": st, "install": s.dbInstallState(r.Context()), "systemd": false}
	if a, ok := s.addons(); ok {
		if _, _, err := a.AddonState(r.Context(), database.InstallUnit); err == nil {
			resp["systemd"] = true
		}
		if st.Installed {
			if active, _, err := a.AddonState(r.Context(), database.PhpMyAdminUnit); err == nil {
				resp["phpMyAdminRunning"] = active == "active"
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDatabaseInstall starts the oneshot installer unit (owner only).
func (s *Server) handleDatabaseInstall(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	a, ok := s.addons()
	if !ok {
		writeError(w, http.StatusConflict, CodeNoSystemd, "Installing add-ons needs systemd. Run: sudo homepanel install-database")
		return
	}
	if s.dbInstallState(r.Context()).Running {
		writeError(w, http.StatusConflict, CodeDBInstalling, "The installation is already running.")
		return
	}
	if err := a.StartAddon(r.Context(), database.InstallUnit); err != nil {
		if errors.Is(err, supervisor.ErrNoSystemd) {
			writeError(w, http.StatusConflict, CodeNoSystemd, "Installing add-ons needs systemd. Run: sudo homepanel install-database")
			return
		}
		writeError(w, http.StatusBadGateway, CodeSupervisor, "Could not start the installer: "+err.Error()+". If this persists run: sudo homepanel install-database")
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "database.install", Target: "mariadb", IP: clientIP(r)})
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleRestartPhpMyAdmin(w http.ResponseWriter, r *http.Request) {
	a, ok := s.addons()
	if !ok {
		writeError(w, http.StatusConflict, CodeNoSystemd, "This needs systemd.")
		return
	}
	if err := a.RestartAddon(r.Context(), database.PhpMyAdminUnit); err != nil {
		writeError(w, http.StatusBadGateway, CodeSupervisor, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type dbView struct {
	*database.Record
	SizeBytes int64  `json:"sizeBytes"`
	Tables    int    `json:"tables"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
}

func (s *Server) dbView(ctx context.Context, rec *database.Record, creds *database.Creds) dbView {
	v := dbView{Record: rec, User: rec.Name}
	if creds != nil {
		v.Host, v.Port = creds.Host, creds.Port
	}
	v.SizeBytes, v.Tables, _ = s.databases.Size(ctx, rec.Name)
	return v
}

func (s *Server) handleListServerDatabases(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	recs, err := s.dbRecords.List(r.Context(), srv.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	st := s.databases.Status(r.Context())
	creds, _ := s.databases.Creds(r.Context())
	items := make([]dbView, 0, len(recs))
	for _, rec := range recs {
		items = append(items, s.dbView(r.Context(), rec, creds))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "status": st})
}

type createDBRequest struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

func (s *Server) requireDatabase(w http.ResponseWriter, r *http.Request) bool {
	st := s.databases.Status(r.Context())
	if !st.Installed {
		writeError(w, http.StatusConflict, CodeDBNotInstalled, "MariaDB is not installed yet. Install it under Databases in the panel menu.")
		return false
	}
	if !st.Reachable {
		writeError(w, http.StatusBadGateway, CodeDBUnavailable, "MariaDB is not answering: "+st.Error)
		return false
	}
	return true
}

func (s *Server) handleCreateServerDatabase(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	var req createDBRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name, err := database.DatabaseName(srv.ID, req.Name)
	if err != nil {
		writeFieldError(w, CodeValidation, "name", err.Error())
		return
	}
	label := strings.TrimSpace(req.Label)
	if len(label) > 80 {
		writeFieldError(w, CodeValidation, "label", "Label must be at most 80 characters.")
		return
	}
	if !s.requireDatabase(w, r) {
		return
	}
	password, err := database.NewPassword(24)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sealed, err := s.secrets.Seal(password)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	rec, err := s.dbRecords.Insert(r.Context(), srv.ID, name, label, sealed)
	if errors.Is(err, database.ErrExists) {
		writeFieldError(w, CodeDBExists, "name", "A database with this name already exists.")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.databases.Create(r.Context(), name, password); err != nil {
		_ = s.dbRecords.Delete(r.Context(), rec.ID)
		writeError(w, http.StatusBadGateway, CodeDBUnavailable, "Could not create the database: "+err.Error())
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "database.create", Target: srv.ID, IP: clientIP(r), Details: map[string]any{"database": name}})
	creds, _ := s.databases.Creds(r.Context())
	writeJSON(w, http.StatusCreated, s.credentials(rec, password, creds))
}

func (s *Server) dbRecordFromRequest(w http.ResponseWriter, r *http.Request) (*database.Record, bool) {
	srv := serverFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "dbid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "Database not found.")
		return nil, false
	}
	rec, err := s.dbRecords.Get(r.Context(), id)
	if err != nil || rec.ServerID != srv.ID {
		writeError(w, http.StatusNotFound, CodeNotFound, "Database not found.")
		return nil, false
	}
	return rec, true
}

type dbCredentials struct {
	Database        string `json:"database"`
	User            string `json:"user"`
	Password        string `json:"password"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	FiveMConnString string `json:"fivemConnectionString"`
	JDBCURL         string `json:"jdbcUrl"`
	PhpMyAdminPort  int    `json:"phpMyAdminPort"`
	ID              int64  `json:"id"`
}

func (s *Server) credentials(rec *database.Record, password string, creds *database.Creds) dbCredentials {
	host, port, pma := "127.0.0.1", 3306, 0
	if creds != nil {
		host, port, pma = creds.Host, creds.Port, creds.PhpMyAdminPort
	}
	return dbCredentials{
		ID: rec.ID, Database: rec.Name, User: rec.Name, Password: password, Host: host, Port: port, PhpMyAdminPort: pma,
		// oxmysql / mysql-async; 127.0.0.1 (TCP) avoids socket and DNS surprises.
		FiveMConnString: fmt.Sprintf("set mysql_connection_string \"mysql://%s:%s@%s:%d/%s?charset=utf8mb4\"", rec.Name, password, host, port, rec.Name),
		JDBCURL:         fmt.Sprintf("jdbc:mysql://%s:%d/%s?useSSL=false&characterEncoding=utf8", host, port, rec.Name),
	}
}

// handleDatabaseCredentials reveals the password (audited).
func (s *Server) handleDatabaseCredentials(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	rec, ok := s.dbRecordFromRequest(w, r)
	if !ok {
		return
	}
	password, err := s.secrets.Open(rec.SealedPassword())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "database.reveal", Target: rec.ServerID, IP: clientIP(r), Details: map[string]any{"database": rec.Name}})
	creds, _ := s.databases.Creds(r.Context())
	writeJSON(w, http.StatusOK, s.credentials(rec, password, creds))
}

func (s *Server) handleRotateDatabasePassword(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	rec, ok := s.dbRecordFromRequest(w, r)
	if !ok || !s.requireDatabase(w, r) {
		return
	}
	password, err := database.NewPassword(24)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.databases.SetPassword(r.Context(), rec.Name, password); err != nil {
		writeError(w, http.StatusBadGateway, CodeDBUnavailable, err.Error())
		return
	}
	sealed, _ := s.secrets.Seal(password)
	if err := s.dbRecords.SetPassword(r.Context(), rec.ID, sealed); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "database.password", Target: rec.ServerID, IP: clientIP(r), Details: map[string]any{"database": rec.Name}})
	creds, _ := s.databases.Creds(r.Context())
	writeJSON(w, http.StatusOK, s.credentials(rec, password, creds))
}

func (s *Server) handleDeleteServerDatabase(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	rec, ok := s.dbRecordFromRequest(w, r)
	if !ok || !s.requireDatabase(w, r) {
		return
	}
	if err := s.databases.Drop(r.Context(), rec.Name); err != nil {
		writeError(w, http.StatusBadGateway, CodeDBUnavailable, err.Error())
		return
	}
	_ = s.dbRecords.Delete(r.Context(), rec.ID)
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "database.delete", Target: rec.ServerID, IP: clientIP(r), Details: map[string]any{"database": rec.Name}})
	w.WriteHeader(http.StatusNoContent)
}

// dropServerDatabases removes every database of a deleted server (best effort).
func (s *Server) dropServerDatabases(ctx context.Context, serverID string) {
	if s.dbRecords == nil {
		return
	}
	recs, err := s.dbRecords.List(ctx, serverID)
	if err != nil || len(recs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, rec := range recs {
		if err := s.databases.Drop(ctx, rec.Name); err != nil {
			s.log.Warn("drop database of deleted server", "server", serverID, "database", rec.Name, "err", err)
		}
	}
}

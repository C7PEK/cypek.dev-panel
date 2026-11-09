// Package servers stores server definitions and keeps each server's
// .homepanel/run.json in sync with the database.
package servers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/C7PEK/cypek.dev-panel/internal/db"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

// Category groups templates for icons and defaults.
type Category string

const (
	CategoryFiveM     Category = "fivem"
	CategoryMinecraft Category = "minecraft"
	CategoryDiscord   Category = "discord-bot"
	CategoryOther     Category = "other"
)

func (c Category) Valid() bool {
	switch c {
	case CategoryFiveM, CategoryMinecraft, CategoryDiscord, CategoryOther:
		return true
	}
	return false
}

// Port is a port the server listens on; shown to the user for forwarding.
type Port struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // tcp | udp | tcp+udp
}

// InstallState tracks template installation; a server can only start when ready.
type InstallState string

const (
	InstallPending    InstallState = "pending"
	InstallInstalling InstallState = "installing"
	InstallReady      InstallState = "ready"
	InstallFailed     InstallState = "failed"
)

// Server is a server definition.
type Server struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	TemplateID string            `json:"templateId"`
	Category   Category          `json:"category"`
	Run        shim.RunConfig    `json:"run"`
	Variables  map[string]string `json:"variables"`
	Ports      []Port            `json:"ports"`
	Autostart  bool              `json:"autostart"`
	Limits     supervisor.Limits `json:"limits"`
	// InstallState is "ready" for custom servers from the start; template
	// servers go pending -> installing -> ready|failed.
	InstallState InstallState `json:"installState"`
	InstallError string       `json:"installError,omitempty"`
	CreatedBy    *int64       `json:"createdBy,omitempty"`
	CreatedAt    string       `json:"createdAt"`
	UpdatedAt    string       `json:"updatedAt"`
}

var (
	ErrNotFound    = errors.New("server not found")
	ErrInvalidName = errors.New("invalid server name")
)

const (
	idLen      = 8
	idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	MaxNameLen = 64
)

var idRe = regexp.MustCompile(`^[a-z0-9]{8}$`)

// ValidID reports whether id has the shape we generate. Used before any
// id reaches a path or a unit name.
func ValidID(id string) bool { return idRe.MatchString(id) }

// ValidateName accepts 1–64 printable characters.
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > MaxNameLen || strings.ContainsAny(name, "\n\r\t") {
		return ErrInvalidName
	}
	return nil
}

// Store persists servers.
type Store struct {
	db         *sql.DB
	serversDir string
}

// NewStore binds to the database and the directory holding server dirs.
func NewStore(sqlDB *sql.DB, serversDir string) *Store {
	return &Store{db: sqlDB, serversDir: serversDir}
}

// Dir returns the working directory of a server.
func (s *Store) Dir(id string) string { return filepath.Join(s.serversDir, id) }

func newID() (string, error) {
	b := make([]byte, idLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return string(b), nil
}

// Create inserts the server, creates its directory and writes run.json.
func (s *Store) Create(ctx context.Context, srv *Server) (*Server, error) {
	if err := ValidateName(srv.Name); err != nil {
		return nil, err
	}
	srv.Name = strings.TrimSpace(srv.Name)
	if srv.TemplateID == "" {
		srv.TemplateID = "custom"
	}
	if srv.Category == "" {
		srv.Category = CategoryOther
	}
	if !srv.Category.Valid() {
		return nil, fmt.Errorf("invalid category %q", srv.Category)
	}
	if srv.InstallState == "" {
		srv.InstallState = InstallReady
	}
	// The run config of a template server is only known once installed.
	if srv.InstallState == InstallReady {
		if err := srv.Run.Validate(); err != nil {
			return nil, fmt.Errorf("run config: %w", err)
		}
	}
	if srv.Variables == nil {
		srv.Variables = map[string]string{}
	}
	if srv.Ports == nil {
		srv.Ports = []Port{}
	}
	id, err := newID()
	if err != nil {
		return nil, fmt.Errorf("generate id: %w", err)
	}
	srv.ID = id
	now := db.Now()
	srv.CreatedAt, srv.UpdatedAt = now, now

	dir := s.Dir(id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create server dir: %w", err)
	}
	if srv.InstallState == InstallReady {
		if err := shim.SaveRunConfig(dir, &srv.Run); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
	}
	runJSON, _ := json.Marshal(srv.Run)
	varsJSON, _ := json.Marshal(srv.Variables)
	portsJSON, _ := json.Marshal(srv.Ports)
	_, err = s.db.ExecContext(ctx, `INSERT INTO servers
		(id, name, template_id, category, run_config, variables, ports, autostart, memory_mb, cpu_percent, install_state, install_error, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		srv.ID, srv.Name, srv.TemplateID, string(srv.Category), string(runJSON), string(varsJSON), string(portsJSON),
		boolInt(srv.Autostart), srv.Limits.MemoryMB, srv.Limits.CPUPercent, string(srv.InstallState), srv.InstallError, srv.CreatedBy, now, now)
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("insert server: %w", err)
	}
	return srv, nil
}

const columns = `id, name, template_id, category, run_config, variables, ports, autostart, memory_mb, cpu_percent, install_state, install_error, created_by, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (*Server, error) {
	var srv Server
	var cat, runJSON, varsJSON, portsJSON, installState string
	var autostart int
	var createdBy sql.NullInt64
	if err := row.Scan(&srv.ID, &srv.Name, &srv.TemplateID, &cat, &runJSON, &varsJSON, &portsJSON,
		&autostart, &srv.Limits.MemoryMB, &srv.Limits.CPUPercent, &installState, &srv.InstallError, &createdBy, &srv.CreatedAt, &srv.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	srv.Category = Category(cat)
	srv.InstallState = InstallState(installState)
	srv.Autostart = autostart == 1
	if createdBy.Valid {
		srv.CreatedBy = &createdBy.Int64
	}
	if err := json.Unmarshal([]byte(runJSON), &srv.Run); err != nil {
		return nil, fmt.Errorf("decode run config of %s: %w", srv.ID, err)
	}
	_ = json.Unmarshal([]byte(varsJSON), &srv.Variables)
	_ = json.Unmarshal([]byte(portsJSON), &srv.Ports)
	if srv.Variables == nil {
		srv.Variables = map[string]string{}
	}
	if srv.Ports == nil {
		srv.Ports = []Port{}
	}
	return &srv, nil
}

// Get returns one server.
func (s *Store) Get(ctx context.Context, id string) (*Server, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	srv, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM servers WHERE id = ?`, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("get server %s: %w", id, err)
	}
	return srv, err
}

// List returns all servers ordered by name; when ids is non-nil only those.
func (s *Store) List(ctx context.Context, ids []string) ([]*Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM servers ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	defer rows.Close()
	var allow map[string]bool
	if ids != nil {
		allow = map[string]bool{}
		for _, id := range ids {
			allow[id] = true
		}
	}
	out := []*Server{}
	for rows.Next() {
		srv, err := scan(rows)
		if err != nil {
			return nil, err
		}
		if allow != nil && !allow[srv.ID] {
			continue
		}
		out = append(out, srv)
	}
	return out, rows.Err()
}

// Update saves name, run config, variables, ports, autostart and limits, and
// rewrites run.json. The caller applies limits/autostart to the supervisor.
func (s *Store) Update(ctx context.Context, srv *Server) error {
	if err := ValidateName(srv.Name); err != nil {
		return err
	}
	if srv.InstallState == InstallReady {
		if err := srv.Run.Validate(); err != nil {
			return fmt.Errorf("run config: %w", err)
		}
		if err := shim.SaveRunConfig(s.Dir(srv.ID), &srv.Run); err != nil {
			return err
		}
	}
	runJSON, _ := json.Marshal(srv.Run)
	varsJSON, _ := json.Marshal(srv.Variables)
	portsJSON, _ := json.Marshal(srv.Ports)
	srv.UpdatedAt = db.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE servers SET name = ?, run_config = ?, variables = ?, ports = ?, autostart = ?,
		memory_mb = ?, cpu_percent = ?, install_state = ?, install_error = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(srv.Name), string(runJSON), string(varsJSON), string(portsJSON), boolInt(srv.Autostart),
		srv.Limits.MemoryMB, srv.Limits.CPUPercent, string(srv.InstallState), srv.InstallError, srv.UpdatedAt, srv.ID)
	if err != nil {
		return fmt.Errorf("update server %s: %w", srv.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetInstallState records installation progress without touching anything else.
func (s *Store) SetInstallState(ctx context.Context, id string, state InstallState, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET install_state = ?, install_error = ?, updated_at = ? WHERE id = ?`,
		string(state), errMsg, db.Now(), id)
	if err != nil {
		return fmt.Errorf("set install state of %s: %w", id, err)
	}
	return nil
}

// Delete removes the row and, when removeFiles is set, the server directory.
func (s *Store) Delete(ctx context.Context, id string, removeFiles bool) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete server %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if removeFiles {
		if err := os.RemoveAll(s.Dir(id)); err != nil {
			return fmt.Errorf("remove files of %s: %w", id, err)
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

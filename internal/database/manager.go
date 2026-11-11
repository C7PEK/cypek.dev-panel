package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Manager struct {
	CredsPath string

	mu    sync.Mutex
	db    *sql.DB
	creds *Creds
}

func NewManager(credsPath string) *Manager { return &Manager{CredsPath: credsPath} }

func (m *Manager) conn(ctx context.Context) (*sql.DB, *Creds, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	creds, err := LoadCreds(m.CredsPath)
	if err != nil {
		return nil, nil, err
	}
	if m.db != nil && m.creds != nil && *m.creds == *creds {
		return m.db, m.creds, nil
	}
	if m.db != nil {
		m.db.Close()
	}
	cfg := mysql.NewConfig()
	cfg.User = creds.AdminUser
	cfg.Passwd = creds.AdminPassword
	cfg.Net = "unix"
	cfg.Addr = creds.Socket
	cfg.Timeout = 5 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetConnMaxIdleTime(time.Minute)
	m.db, m.creds = db, creds
	return db, creds, nil
}

type Status struct {
	Installed      bool   `json:"installed"`
	Reachable      bool   `json:"reachable"`
	Version        string `json:"version,omitempty"`
	Error          string `json:"error,omitempty"`
	PhpMyAdminPort int    `json:"phpMyAdminPort,omitempty"`
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
}

func (m *Manager) Status(ctx context.Context) Status {
	db, creds, err := m.conn(ctx)
	if err != nil {
		if err == ErrNotInstalled {
			return Status{}
		}
		return Status{Installed: true, Error: err.Error()}
	}
	st := Status{Installed: true, PhpMyAdminPort: creds.PhpMyAdminPort, Host: creds.Host, Port: creds.Port}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&st.Version); err != nil {
		st.Error = err.Error()
		return st
	}
	st.Reachable = true
	return st
}

var accountHosts = []string{"localhost", "127.0.0.1", "::1"}

func (m *Manager) Create(ctx context.Context, name, password string) error {
	if !ValidIdent(name) || !strings.HasPrefix(name, NamePrefix) || !ValidPassword(password) {
		return fmt.Errorf("refusing unsafe database name or password")
	}
	db, _, err := m.conn(ctx)
	if err != nil {
		return err
	}
	stmts := []string{fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", name)}
	for _, h := range accountHosts {
		stmts = append(stmts,
			fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'", name, h, password),
			fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY '%s'", name, h, password),

			fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'", grantPattern(name), name, h))
	}
	return execAll(ctx, db, stmts)
}

func (m *Manager) Exists(ctx context.Context, name string) (bool, error) {
	db, _, err := m.conn(ctx)
	if err != nil {
		return false, err
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?`, name).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func grantPattern(name string) string {
	return strings.NewReplacer("_", `\_`, "%", `\%`).Replace(name)
}

func (m *Manager) EnsureAccounts(ctx context.Context, name, password string) error {
	if !ValidIdent(name) || !strings.HasPrefix(name, NamePrefix) || !ValidPassword(password) {
		return fmt.Errorf("refusing unsafe database name or password")
	}
	db, _, err := m.conn(ctx)
	if err != nil {
		return err
	}
	var stmts []string
	for _, h := range accountHosts {
		stmts = append(stmts,
			fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'", name, h, password),
			fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'", grantPattern(name), name, h))
	}
	return execAll(ctx, db, stmts)
}

func (m *Manager) SetPassword(ctx context.Context, name, password string) error {
	if !ValidIdent(name) || !strings.HasPrefix(name, NamePrefix) || !ValidPassword(password) {
		return fmt.Errorf("refusing unsafe database name or password")
	}
	db, _, err := m.conn(ctx)
	if err != nil {
		return err
	}
	var stmts []string
	for _, h := range accountHosts {
		stmts = append(stmts, fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY '%s'", name, h, password))
	}
	return execAll(ctx, db, stmts)
}

func (m *Manager) Drop(ctx context.Context, name string) error {
	if !ValidIdent(name) || !strings.HasPrefix(name, NamePrefix) {
		return fmt.Errorf("refusing unsafe database name")
	}
	db, _, err := m.conn(ctx)
	if err != nil {
		return err
	}
	stmts := []string{fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name)}
	for _, h := range accountHosts {
		stmts = append(stmts, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%s'", name, h))
	}
	return execAll(ctx, db, stmts)
}

func (m *Manager) Size(ctx context.Context, name string) (int64, int, error) {
	db, _, err := m.conn(ctx)
	if err != nil {
		return 0, 0, err
	}
	var size sql.NullInt64
	var tables int
	err = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(data_length + index_length), 0), COUNT(*) FROM information_schema.tables WHERE table_schema = ?`, name).Scan(&size, &tables)
	return size.Int64, tables, err
}

func (m *Manager) Creds(ctx context.Context) (*Creds, error) {
	_, c, err := m.conn(ctx)
	return c, err
}

func execAll(ctx context.Context, db *sql.DB, stmts []string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			verb := strings.SplitN(q, " ", 3)
			return fmt.Errorf("mariadb %s %s failed: %w", verb[0], verb[1], err)
		}
	}
	return nil
}

func ValidRootPassword(p string) bool {
	if len(p) < 12 || len(p) > 128 {
		return false
	}
	for _, r := range p {
		if r < 0x21 || r > 0x7e || r == '\'' || r == '"' || r == '\\' || r == '`' {
			return false
		}
	}
	return true
}

func (m *Manager) SetRootPassword(ctx context.Context, password string) error {
	if !ValidRootPassword(password) {
		return fmt.Errorf("password must be 12-128 printable characters without quotes, backslashes or spaces")
	}
	db, _, err := m.conn(ctx)
	if err != nil {
		return err
	}
	return execAll(ctx, db, []string{fmt.Sprintf("ALTER USER 'root'@'localhost' IDENTIFIED VIA unix_socket OR mysql_native_password USING PASSWORD('%s')", password)})
}

func (m *Manager) DisableRootPassword(ctx context.Context) error {
	db, _, err := m.conn(ctx)
	if err != nil {
		return err
	}
	return execAll(ctx, db, []string{"ALTER USER 'root'@'localhost' IDENTIFIED VIA unix_socket"})
}

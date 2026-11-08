// Package audit records who did what, when and from where.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/cypek019/cypek.dev---panel/internal/db"
)

// Entry is one audit record. Details must not contain secrets.
type Entry struct {
	UserID   *int64
	Username string
	Action   string // e.g. "auth.login", "server.start"
	Target   string // e.g. server id, username
	IP       string
	Details  map[string]any
}

// Record is an audit row as returned to the API.
type Record struct {
	ID        int64           `json:"id"`
	CreatedAt string          `json:"createdAt"`
	UserID    *int64          `json:"userId,omitempty"`
	Username  string          `json:"username"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	IP        string          `json:"ip"`
	Details   json.RawMessage `json:"details"`
}

// Logger writes audit entries to the database and mirrors them to slog.
type Logger struct {
	db  *sql.DB
	log *slog.Logger
}

// New returns a Logger bound to a migrated database.
func New(sqlDB *sql.DB, log *slog.Logger) *Logger {
	return &Logger{db: sqlDB, log: log}
}

// Log stores the entry. Failures are logged, never returned: an audit write
// must not break the user-facing action it describes.
func (l *Logger) Log(ctx context.Context, e Entry) {
	details := "{}"
	if len(e.Details) > 0 {
		if b, err := json.Marshal(e.Details); err == nil {
			details = string(b)
		}
	}
	_, err := l.db.ExecContext(ctx,
		`INSERT INTO audit_log (created_at, user_id, username, action, target, ip, details) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		db.Now(), e.UserID, e.Username, e.Action, e.Target, e.IP, details)
	if err != nil {
		l.log.Error("write audit log", "action", e.Action, "err", err)
	}
	l.log.Info("audit", "action", e.Action, "user", e.Username, "target", e.Target, "ip", e.IP)
}

// Recent returns the newest limit records.
func (l *Logger) Recent(ctx context.Context, limit int) ([]Record, error) {
	rows, err := l.db.QueryContext(ctx,
		`SELECT id, created_at, user_id, username, action, target, ip, details FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query audit log: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var uid sql.NullInt64
		var details string
		if err := rows.Scan(&r.ID, &r.CreatedAt, &uid, &r.Username, &r.Action, &r.Target, &r.IP, &details); err != nil {
			return nil, fmt.Errorf("scan audit row: %w", err)
		}
		if uid.Valid {
			r.UserID = &uid.Int64
		}
		r.Details = json.RawMessage(details)
		out = append(out, r)
	}
	return out, rows.Err()
}

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/cypek019/cypek.dev---panel/internal/db"
)

// Record is a database the panel created for a server.
type Record struct {
	ID        int64  `json:"id"`
	ServerID  string `json:"serverId"`
	Name      string `json:"name"`
	Label     string `json:"label"`
	CreatedAt string `json:"createdAt"`
	password  string // sealed
}

var (
	ErrRecordNotFound = errors.New("database not found")
	ErrExists         = errors.New("a database with this name already exists")
)

// Store persists server_databases rows.
type Store struct{ db *sql.DB }

// NewStore wraps a migrated database.
func NewStore(sqlDB *sql.DB) *Store { return &Store{db: sqlDB} }

func (s *Store) Insert(ctx context.Context, serverID, name, label, sealedPassword string) (*Record, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO server_databases (server_id, name, label, password, created_at) VALUES (?, ?, ?, ?, ?)`,
		serverID, name, label, sealedPassword, db.Now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrExists
		}
		return nil, fmt.Errorf("insert database record: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

func scanRecord(row interface{ Scan(...any) error }) (*Record, error) {
	var r Record
	if err := row.Scan(&r.ID, &r.ServerID, &r.Name, &r.Label, &r.password, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return &r, nil
}

const recCols = `id, server_id, name, label, password, created_at`

func (s *Store) Get(ctx context.Context, id int64) (*Record, error) {
	return scanRecord(s.db.QueryRowContext(ctx, `SELECT `+recCols+` FROM server_databases WHERE id = ?`, id))
}

func (s *Store) List(ctx context.Context, serverID string) ([]*Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recCols+` FROM server_databases WHERE server_id = ? ORDER BY id`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()
	out := []*Record{}
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListAll returns every database record.
func (s *Store) ListAll(ctx context.Context) ([]*Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recCols+` FROM server_databases ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()
	out := []*Record{}
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SetPassword(ctx context.Context, id int64, sealed string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE server_databases SET password = ? WHERE id = ?`, sealed, id)
	return err
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM server_databases WHERE id = ?`, id)
	return err
}

// SealedPassword exposes the stored (encrypted) password to the caller,
// which holds the key to open it.
func (r *Record) SealedPassword() string { return r.password }

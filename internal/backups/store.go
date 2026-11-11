package backups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/db"
)

type Record struct {
	ID        int64  `json:"id"`
	ServerID  string `json:"serverId"`
	FileName  string `json:"fileName"`
	Size      int64  `json:"size"`
	Note      string `json:"note,omitempty"`
	Trigger   string `json:"trigger"`
	CreatedBy *int64 `json:"createdBy,omitempty"`
	CreatedAt string `json:"createdAt"`
}

var ErrNotFound = errors.New("backup not found")

type Store struct {
	db *sql.DB
}

func NewStore(sqlDB *sql.DB) *Store { return &Store{db: sqlDB} }

func (s *Store) Insert(ctx context.Context, r *Record) (*Record, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO backups (server_id, file_name, size, note, trigger, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ServerID, r.FileName, r.Size, r.Note, r.Trigger, r.CreatedBy, db.Now())
	if err != nil {
		return nil, fmt.Errorf("insert backup: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

const cols = `id, server_id, file_name, size, note, trigger, created_by, created_at`

func scan(row interface{ Scan(...any) error }) (*Record, error) {
	var r Record
	var by sql.NullInt64
	if err := row.Scan(&r.ID, &r.ServerID, &r.FileName, &r.Size, &r.Note, &r.Trigger, &by, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if by.Valid {
		r.CreatedBy = &by.Int64
	}
	return &r, nil
}

func (s *Store) Get(ctx context.Context, id int64) (*Record, error) {
	r, err := scan(s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM backups WHERE id = ?`, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("get backup %d: %w", id, err)
	}
	return r, err
}

func (s *Store) List(ctx context.Context, serverID string) ([]*Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM backups WHERE server_id = ? ORDER BY id DESC`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	defer rows.Close()
	out := []*Record{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM backups WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete backup %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Settings(ctx context.Context, serverID, category string) (Settings, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT backup_settings FROM servers WHERE id = ?`, serverID).Scan(&raw)
	if err != nil {
		return Settings{}, fmt.Errorf("read backup settings: %w", err)
	}
	if raw == "" || raw == "{}" {
		return DefaultSettings(category), nil
	}
	var st Settings
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return DefaultSettings(category), nil
	}
	if st.Excludes == nil {
		st.Excludes = []string{}
	}
	return st, nil
}

func (s *Store) SaveSettings(ctx context.Context, serverID string, st Settings) error {
	if err := st.Validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(st)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET backup_settings = ?, updated_at = ? WHERE id = ?`, string(b), db.Now(), serverID)
	if err != nil {
		return fmt.Errorf("save backup settings: %w", err)
	}
	return nil
}

func Items(records []*Record) []Item {
	out := make([]Item, 0, len(records))
	for _, r := range records {
		t, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		out = append(out, Item{ID: r.ID, FileName: r.FileName, CreatedAt: t})
	}
	return out
}

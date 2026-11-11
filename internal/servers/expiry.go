package servers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrExpired = errors.New("the server has expired; an admin has to extend it first")

type Expiry struct {
	ServerID  string
	ExpiresAt time.Time
	ExpiredAt time.Time
	WarnedAt  time.Time
}

func parseT(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func fmtT(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Store) Expiry(ctx context.Context, id string) (*Expiry, error) {
	var e Expiry
	var exp, expired, warned string
	err := s.db.QueryRowContext(ctx, `SELECT server_id, expires_at, expired_at, warned_at FROM server_expiry WHERE server_id = ?`, id).Scan(&e.ServerID, &exp, &expired, &warned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read expiry of %s: %w", id, err)
	}
	e.ExpiresAt, e.ExpiredAt, e.WarnedAt = parseT(exp), parseT(expired), parseT(warned)
	return &e, nil
}

func (s *Store) Expiries(ctx context.Context) ([]Expiry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT server_id, expires_at, expired_at, warned_at FROM server_expiry`)
	if err != nil {
		return nil, fmt.Errorf("list expiries: %w", err)
	}
	defer rows.Close()
	var out []Expiry
	for rows.Next() {
		var e Expiry
		var exp, expired, warned string
		if err := rows.Scan(&e.ServerID, &exp, &expired, &warned); err != nil {
			return nil, err
		}
		e.ExpiresAt, e.ExpiredAt, e.WarnedAt = parseT(exp), parseT(expired), parseT(warned)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SetExpiry(ctx context.Context, id string, at *time.Time) error {
	if at == nil {
		_, err := s.db.ExecContext(ctx, `DELETE FROM server_expiry WHERE server_id = ?`, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO server_expiry (server_id, expires_at, expired_at, warned_at) VALUES (?, ?, '', '')
		ON CONFLICT(server_id) DO UPDATE SET expires_at = excluded.expires_at, expired_at = '', warned_at = ''`, id, fmtT(*at))
	if err != nil {
		return fmt.Errorf("set expiry of %s: %w", id, err)
	}
	return nil
}

func (s *Store) MarkExpired(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE server_expiry SET expired_at = ? WHERE server_id = ?`, fmtT(at), id)
	return err
}

func (s *Store) MarkWarned(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE server_expiry SET warned_at = ? WHERE server_id = ?`, fmtT(at), id)
	return err
}

func (s *Store) CheckStartable(ctx context.Context, id string) error {
	e, err := s.Expiry(ctx, id)
	if err != nil {
		return err
	}
	if e != nil && !e.ExpiredAt.IsZero() {
		return ErrExpired
	}
	return nil
}

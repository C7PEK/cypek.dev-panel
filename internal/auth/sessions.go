package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/db"
)

const (
	SessionTTL = 14 * 24 * time.Hour

	sessionIdleTTL  = 3 * 24 * time.Hour
	sessionTokenLen = 32
	csrfTokenLen    = 32
)

type Session struct {
	ID        string
	UserID    int64
	CSRFToken string
	CreatedAt time.Time
	ExpiresAt time.Time
	LastSeen  time.Time
	IP        string
	UserAgent string

	APIKey bool
}

var ErrSessionInvalid = errors.New("session invalid or expired")

func (s *Store) CreateSession(ctx context.Context, userID int64, ip, userAgent string) (token string, sess *Session, err error) {
	token, err = randomToken(sessionTokenLen)
	if err != nil {
		return "", nil, err
	}
	csrf, err := randomToken(csrfTokenLen)
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	sess = &Session{
		ID:        hashToken(token),
		UserID:    userID,
		CSRFToken: csrf,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
		LastSeen:  now,
		IP:        ip,
		UserAgent: truncate(userAgent, 256),
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, csrf_token, created_at, expires_at, last_seen_at, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.CSRFToken, fmtTime(sess.CreatedAt), fmtTime(sess.ExpiresAt), fmtTime(sess.LastSeen), sess.IP, sess.UserAgent)
	if err != nil {
		return "", nil, fmt.Errorf("insert session: %w", err)
	}
	return token, sess, nil
}

func (s *Store) LookupSession(ctx context.Context, token string) (*Session, *User, error) {
	if token == "" {
		return nil, nil, ErrSessionInvalid
	}
	id := hashToken(token)
	var sess Session
	var created, expires, seen string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, csrf_token, created_at, expires_at, last_seen_at, ip, user_agent FROM sessions WHERE id = ?`, id).
		Scan(&sess.ID, &sess.UserID, &sess.CSRFToken, &created, &expires, &seen, &sess.IP, &sess.UserAgent)
	if err != nil {
		return nil, nil, ErrSessionInvalid
	}
	sess.CreatedAt, _ = db.ParseTime(created)
	sess.ExpiresAt, _ = db.ParseTime(expires)
	sess.LastSeen, _ = db.ParseTime(seen)
	now := time.Now().UTC()
	if now.After(sess.ExpiresAt) || now.Sub(sess.LastSeen) > sessionIdleTTL {
		_ = s.DeleteSession(ctx, id)
		return nil, nil, ErrSessionInvalid
	}
	user, err := s.GetUserByID(ctx, sess.UserID)
	if err != nil || user.Disabled {
		_ = s.DeleteSession(ctx, id)
		return nil, nil, ErrSessionInvalid
	}
	if now.Sub(sess.LastSeen) > time.Minute {
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, fmtTime(now), id)
		sess.LastSeen = now
	}
	return &sess, user, nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) DeleteSessionByToken(ctx context.Context, token string) error {
	return s.DeleteSession(ctx, hashToken(token))
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, fmtTime(time.Now().UTC()))
	if err != nil {
		return 0, fmt.Errorf("purge sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/db"
)

// APIKey is a long-lived credential for automation. It acts as its user,
// optionally narrowed to a set of per-server permissions (scope).
type APIKey struct {
	ID         int64    `json:"id"`
	UserID     int64    `json:"userId"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scope      []string `json:"scope"` // ["*"] or permission names
	CreatedAt  string   `json:"createdAt"`
	ExpiresAt  string   `json:"expiresAt,omitempty"`
	LastUsedAt string   `json:"lastUsedAt,omitempty"`
}

// APIKeyPrefix makes keys recognisable in logs and secret scanners.
const APIKeyPrefix = "hp_"

var ErrAPIKeyInvalid = errors.New("invalid api key")

// AllowsPermission reports whether the scope covers perm.
func (k *APIKey) AllowsPermission(perm Permission) bool {
	for _, s := range k.Scope {
		if s == "*" || s == string(perm) {
			return true
		}
	}
	return false
}

// CreateAPIKey mints a key; the plaintext is returned exactly once.
func (s *Store) CreateAPIKey(ctx context.Context, userID int64, name string, scope []string, expires *time.Time) (plaintext string, key *APIKey, err error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return "", nil, errors.New("key name must be 1–64 characters")
	}
	if len(scope) == 0 {
		scope = []string{"*"}
	}
	for _, sc := range scope {
		if sc != "*" && !Permission(sc).Valid() {
			return "", nil, fmt.Errorf("unknown permission %q in scope", sc)
		}
	}
	raw, err := randomToken(32)
	if err != nil {
		return "", nil, err
	}
	plaintext = APIKeyPrefix + raw
	scopeJSON, _ := json.Marshal(scope)
	var exp any
	if expires != nil {
		exp = expires.UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO api_keys (user_id, name, token_hash, prefix, scope, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, name, hashToken(plaintext), plaintext[:len(APIKeyPrefix)+6], string(scopeJSON), db.Now(), exp)
	if err != nil {
		return "", nil, fmt.Errorf("insert api key: %w", err)
	}
	id, _ := res.LastInsertId()
	key, err = s.getAPIKey(ctx, id)
	return plaintext, key, err
}

const apiKeyCols = `id, user_id, name, prefix, scope, created_at, COALESCE(expires_at,''), COALESCE(last_used_at,'')`

func scanAPIKey(row interface{ Scan(...any) error }) (*APIKey, error) {
	var k APIKey
	var scope string
	if err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &scope, &k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(scope), &k.Scope)
	if k.Scope == nil {
		k.Scope = []string{"*"}
	}
	return &k, nil
}

func (s *Store) getAPIKey(ctx context.Context, id int64) (*APIKey, error) {
	return scanAPIKey(s.db.QueryRowContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys WHERE id = ?`, id))
}

// ListAPIKeys returns a user's keys.
func (s *Store) ListAPIKeys(ctx context.Context, userID int64) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()
	out := []*APIKey{}
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteAPIKey removes one of the user's keys.
func (s *Store) DeleteAPIKey(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// LookupAPIKey resolves a bearer token to its key and user.
func (s *Store) LookupAPIKey(ctx context.Context, plaintext string) (*APIKey, *User, error) {
	if !strings.HasPrefix(plaintext, APIKeyPrefix) {
		return nil, nil, ErrAPIKeyInvalid
	}
	k, err := scanAPIKey(s.db.QueryRowContext(ctx, `SELECT `+apiKeyCols+` FROM api_keys WHERE token_hash = ?`, hashToken(plaintext)))
	if err != nil {
		return nil, nil, ErrAPIKeyInvalid
	}
	if k.ExpiresAt != "" {
		if t, err := db.ParseTime(k.ExpiresAt); err == nil && time.Now().After(t) {
			return nil, nil, ErrAPIKeyInvalid
		}
	}
	u, err := s.GetUserByID(ctx, k.UserID)
	if err != nil || u.Disabled {
		return nil, nil, ErrAPIKeyInvalid
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, db.Now(), k.ID)
	return k, u, nil
}

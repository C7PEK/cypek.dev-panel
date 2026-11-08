// Package auth implements users, sessions, roles and the first-run setup flow.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cypek019/cypek.dev---panel/internal/db"
)

// Role is a global role.
type Role string

const (
	RoleOwner Role = "owner"
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	return r == RoleOwner || r == RoleAdmin || r == RoleUser
}

// AtLeast reports whether r has at least the privileges of min
// (owner > admin > user).
func (r Role) AtLeast(min Role) bool {
	rank := map[Role]int{RoleUser: 1, RoleAdmin: 2, RoleOwner: 3}
	return rank[r] >= rank[min]
}

// User is a panel account. PasswordHash is never serialised to the API.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Role         Role   `json:"role"`
	Disabled     bool   `json:"disabled"`
	TOTPEnabled  bool   `json:"totpEnabled"`
	CreatedAt    string `json:"createdAt"`
	LastLoginAt  string `json:"lastLoginAt,omitempty"`
	PasswordHash string `json:"-"`
}

var (
	ErrNotFound        = errors.New("not found")
	ErrUsernameTaken   = errors.New("username already taken")
	ErrInvalidUsername = errors.New("invalid username")
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,31}$`)

// ValidateUsername accepts 2–32 chars of letters, digits, dot, underscore, dash.
func ValidateUsername(u string) error {
	if !usernameRe.MatchString(u) {
		return ErrInvalidUsername
	}
	return nil
}

// Store persists users and sessions.
type Store struct {
	db *sql.DB
}

// NewStore wraps a migrated database.
func NewStore(sqlDB *sql.DB) *Store { return &Store{db: sqlDB} }

// DB exposes the underlying handle for sibling packages (audit log, settings).
func (s *Store) DB() *sql.DB { return s.db }

// CreateUser hashes the password and inserts the user.
func (s *Store) CreateUser(ctx context.Context, username, password string, role Role) (*User, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := db.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		username, hash, string(role), now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("insert user %s: %w", username, err)
	}
	id, _ := res.LastInsertId()
	return s.GetUserByID(ctx, id)
}

// CountUsers returns the number of accounts; zero means setup has not run.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

const userColumns = `id, username, password_hash, role, disabled, totp_enabled, created_at, COALESCE(last_login_at, '')`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var disabled, totp int
	var role string
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &disabled, &totp, &u.CreatedAt, &u.LastLoginAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.Role = Role(role)
	u.Disabled = disabled == 1
	u.TOTPEnabled = totp == 1
	return &u, nil
}

// GetUserByID looks a user up by primary key.
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("get user %d: %w", id, err)
	}
	return u, err
}

// GetUserByUsername looks a user up by (case-insensitive) username.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, username))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("get user %s: %w", username, err)
	}
	return u, err
}

// ListUsers returns all users ordered by id.
func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetPassword replaces a user's password and invalidates all their sessions.
func (s *Store) SetPassword(ctx context.Context, userID int64, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, db.Now(), userID)
	if err != nil {
		return fmt.Errorf("update password for user %d: %w", userID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("revoke sessions for user %d: %w", userID, err)
	}
	return tx.Commit()
}

// Authenticate verifies credentials. A wrong username and a wrong password
// both return ErrInvalidCredentials, and both cost one argon2 computation so
// usernames cannot be enumerated by timing.
func (s *Store) Authenticate(ctx context.Context, username, password string) (*User, error) {
	u, err := s.GetUserByUsername(ctx, username)
	if errors.Is(err, ErrNotFound) {
		_, _ = VerifyPassword(dummyHash, password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil {
		return nil, fmt.Errorf("verify password for %s: %w", username, err)
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}
	if u.Disabled {
		return nil, ErrAccountDisabled
	}
	return u, nil
}

// TouchLogin records a successful login time.
func (s *Store) TouchLogin(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, db.Now(), userID)
	if err != nil {
		return fmt.Errorf("touch login for user %d: %w", userID, err)
	}
	return nil
}

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrAccountDisabled    = errors.New("account disabled")
)

// dummyHash is a valid argon2id hash of a random string, used to equalise the
// cost of a login attempt against a non-existent user.
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

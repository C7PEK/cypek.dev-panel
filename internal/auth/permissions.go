package auth

import (
	"context"
	"fmt"
)

// Permission is a per-server right granted to users with the global `user`
// role. Owners and admins implicitly hold all of them on every server.
type Permission string

const (
	PermConsoleRead  Permission = "console.read"
	PermConsoleWrite Permission = "console.write"
	PermPower        Permission = "power"
	PermFilesRead    Permission = "files.read"
	PermFilesWrite   Permission = "files.write"
	PermSchedules    Permission = "schedules"
	PermBackups      Permission = "backups"
	PermSettings     Permission = "settings"
	PermDelete       Permission = "delete"
)

// AllPermissions lists every per-server permission.
var AllPermissions = []Permission{
	PermConsoleRead, PermConsoleWrite, PermPower, PermFilesRead, PermFilesWrite,
	PermSchedules, PermBackups, PermSettings, PermDelete,
}

// Valid reports whether p is known.
func (p Permission) Valid() bool {
	for _, q := range AllPermissions {
		if p == q {
			return true
		}
	}
	return false
}

// IsAdmin reports whether the user bypasses per-server permissions.
func (u *User) IsAdmin() bool { return u.Role.AtLeast(RoleAdmin) }

// Require is the single permission check used by every handler and
// WebSocket: admins pass, users need an explicit grant on that server.
func (s *Store) Require(ctx context.Context, u *User, perm Permission, serverID string) (bool, error) {
	if u == nil {
		return false, nil
	}
	if u.IsAdmin() {
		return true, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM server_permissions WHERE server_id = ? AND user_id = ? AND permission = ?`,
		serverID, u.ID, string(perm)).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check permission %s on %s: %w", perm, serverID, err)
	}
	return n > 0, nil
}

// GrantPermissions replaces the user's permission set on a server.
func (s *Store) GrantPermissions(ctx context.Context, serverID string, userID int64, perms []Permission) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM server_permissions WHERE server_id = ? AND user_id = ?`, serverID, userID); err != nil {
		return fmt.Errorf("clear permissions: %w", err)
	}
	for _, p := range perms {
		if !p.Valid() {
			return fmt.Errorf("unknown permission %q", p)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO server_permissions (server_id, user_id, permission) VALUES (?, ?, ?)`, serverID, userID, string(p)); err != nil {
			return fmt.Errorf("grant %s: %w", p, err)
		}
	}
	return tx.Commit()
}

// VisibleServerIDs returns the servers a user has any permission on.
// Admins see everything, signalled by all == true.
func (s *Store) VisibleServerIDs(ctx context.Context, u *User) (ids []string, all bool, err error) {
	if u.IsAdmin() {
		return nil, true, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT server_id FROM server_permissions WHERE user_id = ?`, u.ID)
	if err != nil {
		return nil, false, fmt.Errorf("list visible servers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, false, err
		}
		ids = append(ids, id)
	}
	return ids, false, rows.Err()
}

package auth

import (
	"context"
	"fmt"
)

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

var AllPermissions = []Permission{
	PermConsoleRead, PermConsoleWrite, PermPower, PermFilesRead, PermFilesWrite,
	PermSchedules, PermBackups, PermSettings, PermDelete,
}

func (p Permission) Valid() bool {
	for _, q := range AllPermissions {
		if p == q {
			return true
		}
	}
	return false
}

func (u *User) IsAdmin() bool { return u.Role.AtLeast(RoleAdmin) }

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

func (s *Store) ServerGrants(ctx context.Context, serverID string) (map[int64][]Permission, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, permission FROM server_permissions WHERE server_id = ? ORDER BY user_id, permission`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	defer rows.Close()
	out := map[int64][]Permission{}
	for rows.Next() {
		var uid int64
		var p string
		if err := rows.Scan(&uid, &p); err != nil {
			return nil, err
		}
		out[uid] = append(out[uid], Permission(p))
	}
	return out, rows.Err()
}

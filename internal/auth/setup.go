package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	settingSetupTokenHash = "setup_token_hash"
	setupTokenLen         = 24
	// SetupTokenFile is written (mode 0600) in the data dir so install.sh and
	// `journalctl` are not the only ways to find the token.
	SetupTokenFile = "setup-token"
)

var ErrSetupNotRequired = errors.New("setup already completed")
var ErrSetupTokenInvalid = errors.New("invalid setup token")

// SetupRequired reports whether no account exists yet.
func (s *Store) SetupRequired(ctx context.Context) (bool, error) {
	n, err := s.CountUsers(ctx)
	return n == 0, err
}

// EnsureSetupToken makes sure a setup token exists while setup is required.
// It returns the plaintext token only when a new one was generated; otherwise
// it returns "" and the caller should point the user at SetupTokenFile.
func (s *Store) EnsureSetupToken(ctx context.Context, dataDir string) (token string, generated bool, err error) {
	required, err := s.SetupRequired(ctx)
	if err != nil {
		return "", false, err
	}
	if !required {
		_ = os.Remove(filepath.Join(dataDir, SetupTokenFile))
		return "", false, nil
	}
	existing, err := s.getSetting(ctx, settingSetupTokenHash)
	if err != nil {
		return "", false, err
	}
	tokenPath := filepath.Join(dataDir, SetupTokenFile)
	if existing != "" {
		// Keep the stored hash; re-read the plaintext from disk if it is still there
		// so the banner can print it again after a restart.
		if b, err := os.ReadFile(tokenPath); err == nil && hashToken(string(b)) == existing {
			return string(b), false, nil
		}
		// Hash exists but plaintext is gone: rotate so the owner can still proceed.
	}
	token, err = randomToken(setupTokenLen)
	if err != nil {
		return "", false, err
	}
	if err := s.setSetting(ctx, settingSetupTokenHash, hashToken(token)); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		return "", false, fmt.Errorf("write setup token file: %w", err)
	}
	return token, true, nil
}

// CompleteSetup creates the owner account after validating the setup token.
func (s *Store) CompleteSetup(ctx context.Context, dataDir, token, username, password string) (*User, error) {
	required, err := s.SetupRequired(ctx)
	if err != nil {
		return nil, err
	}
	if !required {
		return nil, ErrSetupNotRequired
	}
	want, err := s.getSetting(ctx, settingSetupTokenHash)
	if err != nil {
		return nil, err
	}
	got := hashToken(token)
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return nil, ErrSetupTokenInvalid
	}
	user, err := s.CreateUser(ctx, username, password, RoleOwner)
	if err != nil {
		return nil, err
	}
	if err := s.deleteSetting(ctx, settingSetupTokenHash); err != nil {
		return nil, err
	}
	_ = os.Remove(filepath.Join(dataDir, SetupTokenFile))
	return user, nil
}

func (s *Store) getSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}
	return v, nil
}

func (s *Store) setSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

func (s *Store) deleteSetting(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key); err != nil {
		return fmt.Errorf("delete setting %s: %w", key, err)
	}
	return nil
}

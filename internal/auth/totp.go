package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/cypek019/cypek.dev---panel/internal/db"
)

// TOTP issuer shown in authenticator apps.
const TOTPIssuer = "panel.cypek.dev"

const backupCodeCount = 8

var (
	ErrTOTPInvalid    = errors.New("invalid one-time code")
	ErrTOTPNotPending = errors.New("no 2FA setup in progress")
	ErrTOTPEnabled    = errors.New("2FA is already enabled")
	ErrTOTPDisabled   = errors.New("2FA is not enabled")
)

// Sealer encrypts the TOTP secret at rest (the secrets box).
type Sealer interface {
	Seal(string) (string, error)
	Open(string) (string, error)
}

// BeginTOTP generates a new secret for the user and stores it unconfirmed.
// It returns the key so the caller can render the QR code.
func (s *Store) BeginTOTP(ctx context.Context, u *User, sealer Sealer) (*otp.Key, error) {
	if u.TOTPEnabled {
		return nil, ErrTOTPEnabled
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: TOTPIssuer, AccountName: u.Username})
	if err != nil {
		return nil, fmt.Errorf("generate totp: %w", err)
	}
	sealed, err := sealer.Seal(key.Secret())
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET totp_secret = ?, totp_enabled = 0, updated_at = ? WHERE id = ?`, sealed, db.Now(), u.ID); err != nil {
		return nil, fmt.Errorf("store totp secret: %w", err)
	}
	return key, nil
}

// ConfirmTOTP enables 2FA once the user proves they have the secret, and
// returns the one-time backup codes (plaintext, shown exactly once).
func (s *Store) ConfirmTOTP(ctx context.Context, u *User, sealer Sealer, code string) ([]string, error) {
	secret, enabled, err := s.totpSecret(ctx, u.ID, sealer)
	if err != nil {
		return nil, err
	}
	if enabled {
		return nil, ErrTOTPEnabled
	}
	if secret == "" {
		return nil, ErrTOTPNotPending
	}
	if !validTOTP(secret, code) {
		return nil, ErrTOTPInvalid
	}
	codes, hashes, err := newBackupCodes()
	if err != nil {
		return nil, err
	}
	hb, _ := json.Marshal(hashes)
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET totp_enabled = 1, totp_backup_codes = ?, updated_at = ? WHERE id = ?`, string(hb), db.Now(), u.ID); err != nil {
		return nil, fmt.Errorf("enable totp: %w", err)
	}
	return codes, nil
}

// DisableTOTP turns 2FA off after a valid code (or backup code).
func (s *Store) DisableTOTP(ctx context.Context, u *User, sealer Sealer, code string) error {
	ok, err := s.VerifyTOTP(ctx, u.ID, sealer, code)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTOTPInvalid
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET totp_secret = NULL, totp_enabled = 0, totp_backup_codes = '[]', updated_at = ? WHERE id = ?`, db.Now(), u.ID)
	if err != nil {
		return fmt.Errorf("disable totp: %w", err)
	}
	return nil
}

// VerifyTOTP checks a 6-digit code or a backup code (which is consumed).
func (s *Store) VerifyTOTP(ctx context.Context, userID int64, sealer Sealer, code string) (bool, error) {
	secret, enabled, err := s.totpSecret(ctx, userID, sealer)
	if err != nil {
		return false, err
	}
	if !enabled {
		return false, ErrTOTPDisabled
	}
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if validTOTP(secret, code) {
		return true, nil
	}
	return s.consumeBackupCode(ctx, userID, code)
}

func (s *Store) totpSecret(ctx context.Context, userID int64, sealer Sealer) (secret string, enabled bool, err error) {
	var sealed sql.NullString
	var en int
	if err := s.db.QueryRowContext(ctx, `SELECT totp_secret, totp_enabled FROM users WHERE id = ?`, userID).Scan(&sealed, &en); err != nil {
		return "", false, fmt.Errorf("read totp: %w", err)
	}
	if !sealed.Valid || sealed.String == "" {
		return "", en == 1, nil
	}
	plain, err := sealer.Open(sealed.String)
	if err != nil {
		return "", false, err
	}
	return plain, en == 1, nil
}

func validTOTP(secret, code string) bool {
	ok, err := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	return err == nil && ok
}

func newBackupCodes() (codes, hashes []string, err error) {
	for i := 0; i < backupCodeCount; i++ {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		c := strings.ToLower(hex.EncodeToString(b)) // 10 hex chars
		c = c[:5] + "-" + c[5:]
		codes = append(codes, c)
		hashes = append(hashes, hashBackup(c))
	}
	return codes, hashes, nil
}

func hashBackup(code string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.ReplaceAll(code, "-", ""))))
	return hex.EncodeToString(sum[:])
}

func (s *Store) consumeBackupCode(ctx context.Context, userID int64, code string) (bool, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT totp_backup_codes FROM users WHERE id = ?`, userID).Scan(&raw); err != nil {
		return false, err
	}
	var hashes []string
	_ = json.Unmarshal([]byte(raw), &hashes)
	want := hashBackup(code)
	for i, h := range hashes {
		if h == want {
			hashes = append(hashes[:i], hashes[i+1:]...)
			hb, _ := json.Marshal(hashes)
			_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_backup_codes = ? WHERE id = ?`, string(hb), userID)
			return err == nil, err
		}
	}
	return false, nil
}

// BackupCodesLeft reports how many unused backup codes remain.
func (s *Store) BackupCodesLeft(ctx context.Context, userID int64) int {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT totp_backup_codes FROM users WHERE id = ?`, userID).Scan(&raw); err != nil {
		return 0
	}
	var hashes []string
	_ = json.Unmarshal([]byte(raw), &hashes)
	return len(hashes)
}

// PendingLogins holds half-finished logins waiting for a one-time code.
type PendingLogins struct {
	ttl   time.Duration
	items map[string]pendingLogin
}

type pendingLogin struct {
	userID  int64
	expires time.Time
	tries   int
}

// NewPendingLogins creates the store with a 5 minute window.
func NewPendingLogins() *PendingLogins {
	return &PendingLogins{ttl: 5 * time.Minute, items: map[string]pendingLogin{}}
}

// Add returns an opaque token for the user.
func (p *PendingLogins) Add(userID int64) (string, error) {
	tok, err := randomToken(24)
	if err != nil {
		return "", err
	}
	now := time.Now()
	for k, v := range p.items {
		if now.After(v.expires) {
			delete(p.items, k)
		}
	}
	p.items[tok] = pendingLogin{userID: userID, expires: now.Add(p.ttl)}
	return tok, nil
}

// Take looks a token up; after 5 wrong codes the token is burned.
func (p *PendingLogins) Take(token string, success bool) (int64, bool) {
	v, ok := p.items[token]
	if !ok || time.Now().After(v.expires) {
		delete(p.items, token)
		return 0, false
	}
	if success {
		delete(p.items, token)
		return v.userID, true
	}
	v.tries++
	if v.tries >= 5 {
		delete(p.items, token)
		return 0, false
	}
	p.items[token] = v
	return v.userID, true
}

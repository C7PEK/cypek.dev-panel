package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := db.Migrate(context.Background(), sqlDB, "", log); err != nil {
		t.Fatal(err)
	}
	return NewStore(sqlDB)
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !ok {
		t.Fatalf("verify = %v, %v; want true, nil", ok, err)
	}
	ok, err = VerifyPassword(hash, "wrong")
	if err != nil || ok {
		t.Fatalf("verify wrong = %v, %v; want false, nil", ok, err)
	}
	if _, err := VerifyPassword("$bcrypt$nope", "x"); err == nil {
		t.Error("expected error for foreign hash format")
	}
	if _, err := VerifyPassword(dummyHash, "x"); err != nil {
		t.Errorf("dummyHash must be parseable: %v", err)
	}
}

func TestValidateUsername(t *testing.T) {
	good := []string{"ab", "olek", "user.name", "a-b_c", "User123"}
	bad := []string{"", "a", ".dot", "-dash", "has space", "zażółć", "toolong_toolong_toolong_toolong_x", "a/b"}
	for _, u := range good {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("%q rejected: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := ValidateUsername(u); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
}

func TestUsersAndAuthenticate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if n, _ := s.CountUsers(ctx); n != 0 {
		t.Fatalf("CountUsers = %d", n)
	}
	u, err := s.CreateUser(ctx, "Olek", "password123", RoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != RoleOwner || u.Username != "Olek" {
		t.Errorf("unexpected user %+v", u)
	}
	if _, err := s.CreateUser(ctx, "olek", "password123", RoleUser); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("case-insensitive duplicate: err = %v", err)
	}
	if _, err := s.CreateUser(ctx, "short", "1234567", RoleUser); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password: err = %v", err)
	}
	if _, err := s.CreateUser(ctx, "x", "password123", RoleUser); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("bad username: err = %v", err)
	}

	got, err := s.Authenticate(ctx, "olek", "password123")
	if err != nil || got.ID != u.ID {
		t.Fatalf("Authenticate = %v, %v", got, err)
	}
	if _, err := s.Authenticate(ctx, "olek", "nope"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := s.Authenticate(ctx, "ghost", "nope"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: %v", err)
	}
	if _, err := s.GetUserByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing id: %v", err)
	}
	list, err := s.ListUsers(ctx)
	if err != nil || len(list) != 1 {
		t.Errorf("ListUsers = %v, %v", list, err)
	}
}

func TestSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "owner", "password123", RoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	token, sess, err := s.CreateSession(ctx, u.ID, "10.0.0.5", "UnitTest/1.0")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || sess.CSRFToken == "" || sess.ID == token {
		t.Fatalf("bad session %+v token=%q", sess, token)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT id FROM sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == token {
		t.Error("raw token must not be stored")
	}

	got, user, err := s.LookupSession(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sess.ID || user.ID != u.ID || got.CSRFToken != sess.CSRFToken {
		t.Errorf("lookup mismatch: %+v %+v", got, user)
	}
	if _, _, err := s.LookupSession(ctx, "garbage"); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("garbage token: %v", err)
	}
	if _, _, err := s.LookupSession(ctx, ""); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("empty token: %v", err)
	}

	// Expired sessions are rejected and deleted.
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := s.db.Exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, fmtTime(past), sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LookupSession(ctx, token); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("expired session accepted: %v", err)
	}
	var n int
	_ = s.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Errorf("expired session not deleted, %d rows", n)
	}

	// Password change revokes sessions.
	token2, _, _ := s.CreateSession(ctx, u.ID, "", "")
	if err := s.SetPassword(ctx, u.ID, "newpassword123"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LookupSession(ctx, token2); !errors.Is(err, ErrSessionInvalid) {
		t.Error("session survived password change")
	}
	if _, err := s.Authenticate(ctx, "owner", "newpassword123"); err != nil {
		t.Errorf("new password rejected: %v", err)
	}

	// Disabled users cannot use sessions.
	token3, _, _ := s.CreateSession(ctx, u.ID, "", "")
	if _, err := s.db.Exec(`UPDATE users SET disabled = 1 WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LookupSession(ctx, token3); !errors.Is(err, ErrSessionInvalid) {
		t.Error("disabled user session accepted")
	}
	if _, err := s.Authenticate(ctx, "owner", "newpassword123"); !errors.Is(err, ErrAccountDisabled) {
		t.Errorf("disabled login: %v", err)
	}
}

func TestSetupFlow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dataDir := t.TempDir()

	token, generated, err := s.EnsureSetupToken(ctx, dataDir)
	if err != nil || !generated || token == "" {
		t.Fatalf("EnsureSetupToken = %q, %v, %v", token, generated, err)
	}
	b, err := os.ReadFile(filepath.Join(dataDir, SetupTokenFile))
	if err != nil || string(b) != token {
		t.Fatalf("token file = %q, %v", b, err)
	}
	// Restart: same token comes back, not regenerated.
	again, generated, err := s.EnsureSetupToken(ctx, dataDir)
	if err != nil || generated || again != token {
		t.Fatalf("second EnsureSetupToken = %q, %v, %v", again, generated, err)
	}
	// Plaintext lost: token rotates instead of locking the owner out.
	os.Remove(filepath.Join(dataDir, SetupTokenFile))
	rotated, generated, err := s.EnsureSetupToken(ctx, dataDir)
	if err != nil || !generated || rotated == token {
		t.Fatalf("rotate = %q, %v, %v", rotated, generated, err)
	}

	if _, err := s.CompleteSetup(ctx, dataDir, token, "owner", "password123"); !errors.Is(err, ErrSetupTokenInvalid) {
		t.Errorf("old token accepted: %v", err)
	}
	if _, err := s.CompleteSetup(ctx, dataDir, "", "owner", "password123"); !errors.Is(err, ErrSetupTokenInvalid) {
		t.Errorf("empty token accepted: %v", err)
	}
	u, err := s.CompleteSetup(ctx, dataDir, rotated, "owner", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != RoleOwner {
		t.Errorf("role = %s", u.Role)
	}
	if req, _ := s.SetupRequired(ctx); req {
		t.Error("setup still required")
	}
	if _, err := os.Stat(filepath.Join(dataDir, SetupTokenFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("token file not removed after setup")
	}
	if _, err := s.CompleteSetup(ctx, dataDir, rotated, "other", "password123"); !errors.Is(err, ErrSetupNotRequired) {
		t.Errorf("second setup: %v", err)
	}
	var v sql.NullString
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, settingSetupTokenHash).Scan(&v)
	if v.Valid {
		t.Error("token hash still in settings")
	}
	if tok, gen, err := s.EnsureSetupToken(ctx, dataDir); err != nil || gen || tok != "" {
		t.Errorf("EnsureSetupToken after setup = %q %v %v", tok, gen, err)
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	rl := NewRateLimiter(3, time.Minute)
	rl.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if ok, _ := rl.Allow("ip"); !ok {
			t.Fatalf("attempt %d blocked", i+1)
		}
	}
	ok, retry := rl.Allow("ip")
	if ok || retry <= 0 {
		t.Fatalf("4th attempt allowed, retry=%v", retry)
	}
	if ok, _ := rl.Allow("other"); !ok {
		t.Error("separate key blocked")
	}
	rl.Reset("ip")
	if ok, _ := rl.Allow("ip"); !ok {
		t.Error("blocked after reset")
	}
	now = now.Add(2 * time.Minute)
	for i := 0; i < 3; i++ {
		if ok, _ := rl.Allow("other"); !ok {
			t.Fatal("window did not expire")
		}
	}
}

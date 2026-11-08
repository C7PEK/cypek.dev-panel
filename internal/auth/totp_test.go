package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

type plainSealer struct{}

func (plainSealer) Seal(s string) (string, error) { return "sealed:" + s, nil }
func (plainSealer) Open(s string) (string, error) { return s[len("sealed:"):], nil }

func TestTOTPLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "owner", "password123", RoleOwner)
	sealer := plainSealer{}

	key, err := s.BeginTOTP(ctx, u, sealer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmTOTP(ctx, u, sealer, "000000"); !errors.Is(err, ErrTOTPInvalid) {
		t.Errorf("bad confirm: %v", err)
	}
	code, _ := totp.GenerateCode(key.Secret(), time.Now())
	codes, err := s.ConfirmTOTP(ctx, u, sealer, code)
	if err != nil || len(codes) != 8 {
		t.Fatalf("confirm: %v %v", codes, err)
	}
	u, _ = s.GetUserByID(ctx, u.ID)
	if !u.TOTPEnabled {
		t.Fatal("not enabled")
	}
	if _, err := s.BeginTOTP(ctx, u, sealer); !errors.Is(err, ErrTOTPEnabled) {
		t.Errorf("begin while enabled: %v", err)
	}
	code, _ = totp.GenerateCode(key.Secret(), time.Now())
	if ok, err := s.VerifyTOTP(ctx, u.ID, sealer, code); err != nil || !ok {
		t.Errorf("verify code: %v %v", ok, err)
	}
	if ok, _ := s.VerifyTOTP(ctx, u.ID, sealer, "123456"); ok {
		t.Error("wrong code accepted")
	}
	if ok, _ := s.VerifyTOTP(ctx, u.ID, sealer, codes[0]); !ok {
		t.Error("backup code rejected")
	}
	if ok, _ := s.VerifyTOTP(ctx, u.ID, sealer, codes[0]); ok {
		t.Error("backup code reused")
	}
	if left := s.BackupCodesLeft(ctx, u.ID); left != 7 {
		t.Errorf("codes left = %d", left)
	}
	if err := s.DisableTOTP(ctx, u, sealer, "000000"); !errors.Is(err, ErrTOTPInvalid) {
		t.Errorf("disable with bad code: %v", err)
	}
	code, _ = totp.GenerateCode(key.Secret(), time.Now())
	if err := s.DisableTOTP(ctx, u, sealer, code); err != nil {
		t.Fatal(err)
	}
	u, _ = s.GetUserByID(ctx, u.ID)
	if u.TOTPEnabled || s.BackupCodesLeft(ctx, u.ID) != 0 {
		t.Error("not disabled cleanly")
	}

	p := NewPendingLogins()
	tok, _ := p.Add(u.ID)
	for i := 0; i < 4; i++ {
		if id, ok := p.Take(tok, false); !ok || id != u.ID {
			t.Fatalf("try %d: %v %v", i, id, ok)
		}
	}
	if _, ok := p.Take(tok, false); ok {
		t.Error("token survived 5 failures")
	}
	tok, _ = p.Add(u.ID)
	if id, ok := p.Take(tok, true); !ok || id != u.ID {
		t.Error("success take failed")
	}
	if _, ok := p.Take(tok, true); ok {
		t.Error("token reusable")
	}
}

package secrets

import (
	"bytes"
	"testing"
)

func TestSealOpen(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	b, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New([]byte("short")); err == nil {
		t.Error("short key accepted")
	}
	sealed, err := b.Seal("cfxk_secret")
	if err != nil || !IsSealed(sealed) || sealed == "cfxk_secret" {
		t.Fatalf("Seal = %q, %v", sealed, err)
	}
	sealed2, _ := b.Seal("cfxk_secret")
	if sealed == sealed2 {
		t.Error("nonce reused")
	}
	if got, err := b.Open(sealed); err != nil || got != "cfxk_secret" {
		t.Errorf("Open = %q, %v", got, err)
	}
	if got, _ := b.Open("plain"); got != "plain" {
		t.Error("plain value altered")
	}
	if s, _ := b.Seal(""); s != "" {
		t.Error("empty should stay empty")
	}
	other, _ := New(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(sealed); err == nil {
		t.Error("wrong key decrypted")
	}
	if _, err := b.Open("enc:v1:!!!"); err == nil {
		t.Error("garbage accepted")
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")
	base := Default()
	base.DataDir = filepath.Join(dir, "data")

	cfg, err := LoadOrCreate(path, base)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Listen != ":8420" {
		t.Errorf("Listen = %q, want :8420", cfg.Listen)
	}
	if cfg.DataDir != base.DataDir {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.SessionSecret == "" || cfg.EncryptionKey == "" {
		t.Error("secrets not generated")
	}
	if cfg.SessionSecret == cfg.EncryptionKey {
		t.Error("secrets must differ")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config mode = %o, want 600", mode)
	}

	// Second call must load, not regenerate.
	again, err := LoadOrCreate(path, base)
	if err != nil {
		t.Fatal(err)
	}
	if again.SessionSecret != cfg.SessionSecret {
		t.Error("secret regenerated on second load")
	}
}

func TestGenerateRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Generate(path, nil); err != nil {
		t.Fatal(err)
	}
	if err := Generate(path, nil); err == nil {
		t.Fatal("expected error when overwriting existing config")
	}
}

func TestLoadRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"unknown key":       "listen = \":1\"\nsession_secret = \"" + strings.Repeat("A", 44) + "\"\nencryption_key = \"" + strings.Repeat("A", 44) + "\"\nbogus = 1\n",
		"missing secret":    "listen = \":1\"\n",
		"short secret":      "listen = \":1\"\nsession_secret = \"AAAA\"\nencryption_key = \"AAAA\"\n",
		"tls without files": "listen = \":1\"\nsession_secret = \"" + strings.Repeat("A", 44) + "\"\nencryption_key = \"" + strings.Repeat("A", 44) + "\"\n[tls]\nenabled = true\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

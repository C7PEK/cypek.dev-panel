package db

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateFreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.db")
	sqlDB, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx := context.Background()
	if err := Migrate(ctx, sqlDB, path, log); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Idempotent.
	if err := Migrate(ctx, sqlDB, path, log); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('users','sessions','settings','audit_log')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("tables = %d, want 4", n)
	}
	var mode string
	if err := sqlDB.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	var fk int
	if err := sqlDB.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Error("foreign_keys not enabled")
	}
	// Fresh DB: file was empty/nonexistent before first migrate, so no backup expected.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak-") {
			t.Errorf("unexpected backup for fresh db: %s", e.Name())
		}
	}
}

func TestBackupBeforeMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.db")
	// Simulate an existing, non-empty database with no migrations applied.
	if err := os.WriteFile(path, []byte("not really sqlite, but non-empty"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := backupFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("expected a backup path")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

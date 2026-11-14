package db

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
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

func TestCategoryMigrationKeepsRelatedRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.db")
	sqlDB, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	ctx := context.Background()
	sub, _ := fs.Sub(migrations, "migrations")
	p, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, sub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 11); err != nil {
		t.Fatal(err)
	}
	now := "2026-11-14T00:00:00Z"
	for _, q := range []string{
		`INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES (1, 'owner', 'x', 'owner', '` + now + `', '` + now + `')`,
		`INSERT INTO servers (id, name, category, run_config, created_by, created_at, updated_at, install_state) VALUES ('abcd1234', 'MC', 'minecraft', '{}', 1, '` + now + `', '` + now + `', 'ready')`,
		`INSERT INTO server_permissions (user_id, server_id, permission) VALUES (1, 'abcd1234', 'power')`,
		`INSERT INTO schedules (server_id, name, cron_expr, actions, created_at, updated_at) VALUES ('abcd1234', 'r', '0 4 * * *', '[]', '` + now + `', '` + now + `')`,
		`INSERT INTO backups (server_id, file_name, created_at) VALUES ('abcd1234', 'b.tar.zst', '` + now + `')`,
		`INSERT INTO server_databases (server_id, name, password, created_at) VALUES ('abcd1234', 'hp_mc', 'x', '` + now + `')`,
		`INSERT INTO server_alerts (server_id) VALUES ('abcd1234')`,
		`INSERT INTO server_expiry (server_id, expires_at) VALUES ('abcd1234', '` + now + `')`,
	} {
		if _, err := sqlDB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO servers (id, name, category, run_config, created_at, updated_at) VALUES ('web00001', 'W', 'website', '{}', '` + now + `', '` + now + `')`); err == nil {
		t.Fatal("old schema accepted the website category; the test does not reproduce the bug")
	}

	if err := Migrate(ctx, sqlDB, path, slog.New(slog.NewTextHandler(os.Stderr, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	count := func(table string) int {
		var n int
		if err := sqlDB.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, table := range []string{"servers", "server_permissions", "schedules", "backups", "server_databases", "server_alerts", "server_expiry"} {
		if n := count(table); n != 1 {
			t.Errorf("%s has %d rows after the migration, want 1", table, n)
		}
	}
	var fk int
	_ = sqlDB.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign keys left off after the migration")
	}
	if _, err := sqlDB.Exec(`INSERT INTO servers (id, name, category, run_config, created_at, updated_at) VALUES ('web00001', 'W', 'website', '{}', '` + now + `', '` + now + `')`); err != nil {
		t.Fatalf("website category after migration: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO servers (id, name, category, run_config, created_at, updated_at) VALUES ('tun00001', 'T', 'tunnel', '{}', '` + now + `', '` + now + `')`); err != nil {
		t.Fatalf("tunnel category after migration: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO servers (id, name, run_config, created_at, updated_at, install_state) VALUES ('bad00001', 'B', '{}', '` + now + `', '` + now + `', 'bogus')`); err == nil {
		t.Error("install_state check was lost")
	}
	if _, err := sqlDB.Exec(`DELETE FROM servers WHERE id = 'abcd1234'`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"server_permissions", "schedules", "backups", "server_databases", "server_alerts", "server_expiry"} {
		if n := count(table); n != 0 {
			t.Errorf("%s kept %d rows after deleting the server (cascade broken)", table, n)
		}
	}
}

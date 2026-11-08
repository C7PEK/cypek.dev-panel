package database

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// TestRealMariaDB runs against the installed add-on (opt-in:
// HOMEPANEL_DB_TEST=1, as the homepanel user so it reads the real creds).
func TestRealMariaDB(t *testing.T) {
	if os.Getenv("HOMEPANEL_DB_TEST") != "1" {
		t.Skip("set HOMEPANEL_DB_TEST=1")
	}
	ctx := context.Background()
	m := NewManager(CredsPath)
	st := m.Status(ctx)
	if !st.Reachable {
		t.Fatalf("status = %+v", st)
	}
	t.Logf("MariaDB %s", st.Version)
	name, _ := DatabaseName("zzreal01", "test")
	pw, _ := NewPassword(24)
	defer m.Drop(ctx, name)
	if err := m.Create(ctx, name, pw); err != nil {
		t.Fatal(err)
	}
	// Connect like a game server: TCP 127.0.0.1 with the new account.
	connect := func(pass string) error {
		cfg := mysql.NewConfig()
		cfg.User, cfg.Passwd, cfg.Net, cfg.Addr, cfg.DBName, cfg.Timeout = name, pass, "tcp", "127.0.0.1:3306", name, 3*time.Second
		db, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			return err
		}
		defer db.Close()
		start := time.Now()
		if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS t (id INT PRIMARY KEY AUTO_INCREMENT, v VARCHAR(50))"); err != nil {
			return err
		}
		for i := 0; i < 200; i++ {
			if _, err := db.ExecContext(ctx, "INSERT INTO t (v) VALUES (?)", "x"); err != nil {
				return err
			}
		}
		t.Logf("200 autocommit inserts in %s (%.1f ms each)", time.Since(start).Round(time.Millisecond), float64(time.Since(start).Microseconds())/200/1000)
		// Must not see other databases.
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema', ?)", name).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("account sees %d other databases", n)
		}
		return nil
	}
	if err := connect(pw); err != nil {
		t.Fatalf("connect as %s: %v", name, err)
	}
	size, tables, err := m.Size(ctx, name)
	if err != nil || tables != 1 || size == 0 {
		t.Errorf("size = %d tables = %d err = %v", size, tables, err)
	}
	pw2, _ := NewPassword(24)
	if err := m.SetPassword(ctx, name, pw2); err != nil {
		t.Fatal(err)
	}
	if err := connect(pw); err == nil {
		t.Error("old password still works")
	}
	if err := connect(pw2); err != nil {
		t.Errorf("new password: %v", err)
	}
	if err := m.Create(ctx, "mysql", pw); err == nil {
		t.Error("created a database outside the hp_ prefix")
	}
	if err := m.Drop(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := connect(pw2); err == nil {
		t.Error("user survived Drop")
	}
}

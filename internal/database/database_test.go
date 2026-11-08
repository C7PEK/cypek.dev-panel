package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamesAndPasswords(t *testing.T) {
	n, err := DatabaseName("abcd1234", "")
	if err != nil || n != "hp_abcd1234_main" {
		t.Errorf("default = %q, %v", n, err)
	}
	for _, bad := range []string{"x y", "a`b", "toolongtoolongtoolong", "a;drop", "Ą", "a'b", "a-b"} {
		if _, err := DatabaseName("abcd1234", bad); err == nil {
			t.Errorf("suffix %q accepted", bad)
		}
	}
	if n, _ := DatabaseName("abcd1234", "  ESX "); n != "hp_abcd1234_esx" {
		t.Errorf("normalised = %q", n)
	}
	for i := 0; i < 50; i++ {
		p, err := NewPassword(24)
		if err != nil || !ValidPassword(p) || len(p) != 24 {
			t.Fatalf("password %q %v", p, err)
		}
	}
	for _, bad := range []string{"short", "has'quote123456", "spaces in it 123", strings.Repeat("a", 65)} {
		if ValidPassword(bad) {
			t.Errorf("password %q accepted", bad)
		}
	}
}

func TestCredsAndLog(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadCreds(filepath.Join(dir, "missing.json")); err != ErrNotInstalled {
		t.Errorf("missing = %v", err)
	}
	p := filepath.Join(dir, "c.json")
	_ = os.WriteFile(p, []byte(`{"adminUser":"homepanel_admin","adminPassword":"abc"}`), 0o600)
	c, err := LoadCreds(p)
	if err != nil || c.Socket != DefaultSocket || c.Port != 3306 || c.Host != "127.0.0.1" {
		t.Errorf("creds = %+v, %v", c, err)
	}
	_ = os.WriteFile(p, []byte(`{}`), 0o600)
	if _, err := LoadCreds(p); err == nil {
		t.Error("incomplete creds accepted")
	}
	logp := filepath.Join(dir, "l.log")
	_ = os.WriteFile(logp, []byte("10:00:00 Step 1\n10:00:01   apt output\n10:00:02 DONE\n"), 0o600)
	lines, done, failed := ReadInstallLog(logp)
	if len(lines) != 3 || !done || failed {
		t.Errorf("log = %v %v %v", lines, done, failed)
	}
	_ = os.WriteFile(logp, []byte("10:00:00 Step 1\n10:00:02 FAILED: boom\n"), 0o600)
	if _, done, failed := ReadInstallLog(logp); done || !failed {
		t.Errorf("failed log: %v %v", done, failed)
	}
}

func TestTuningConfig(t *testing.T) {
	gb := uint64(1024 * 1024 * 1024)
	cases := map[uint64]string{256 * 1024 * 1024: "128M", 4 * gb: "1024M", 32 * gb: "2048M", 0: "128M"}
	for ram, want := range cases {
		c := TuningConfig(ram)
		if !strings.Contains(c, "innodb_buffer_pool_size        = "+want) {
			t.Errorf("ram %d: pool not %s:\n%s", ram, want, c)
		}
		for _, must := range []string{"skip-name-resolve", "innodb_flush_log_at_trx_commit = 2", "bind-address                   = 127.0.0.1", "utf8mb4"} {
			if !strings.Contains(c, must) {
				t.Errorf("missing %q", must)
			}
		}
	}
	if TotalRAM() == 0 {
		t.Error("TotalRAM = 0")
	}
}

func TestGrantPattern(t *testing.T) {
	if got := grantPattern("hp_abcd1234_main"); got != `hp\_abcd1234\_main` {
		t.Errorf("grantPattern = %q", got)
	}
}

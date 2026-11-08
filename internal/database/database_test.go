package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamesAndPasswords(t *testing.T) {
	for _, bad := range []string{"", "x y", "a`b", "toolongtoolongtoolongtoolong", "a;drop", "Ą", "a'b", "a-b", "1abc", "_x"} {
		if _, err := DatabaseName(bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if n, _ := DatabaseName("  ESX "); n != "hp_esx" {
		t.Errorf("normalised = %q", n)
	}
	slugs := map[string]string{"Mój ESX #2": "moj_esx_2", "test": "test", "!!!": "db", "123 server": "server", "Bardzo Długa Nazwa Serwera Do Testu": "bardzo_dluga_nazwa_se"}
	for in, want := range slugs {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
		if _, err := DatabaseName(Slug(in)); err != nil {
			t.Errorf("slug of %q not a valid name: %v", in, err)
		}
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
		c := TuningConfig(ram, true)
		if !strings.Contains(c, "innodb_buffer_pool_size        = "+want) {
			t.Errorf("ram %d: pool not %s:\n%s", ram, want, c)
		}
		for _, must := range []string{"skip-name-resolve", "innodb_flush_log_at_trx_commit = 2", "bind-address                   = 127.0.0.1,::1", "utf8mb4"} {
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

func TestValidRootPassword(t *testing.T) {
	for _, ok := range []string{"Abcdefgh1234", "x!@#$%^&*()-_=+[]{};:,.<>/?|~" + "12"} {
		if !ValidRootPassword(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"short", "has space in it", "quote'quote123", `back\slash12345`, "tick`tick12345", "żółćżółćżółć1"} {
		if ValidRootPassword(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBindListSupport(t *testing.T) {
	cases := map[string]bool{"11.8.6-MariaDB-0+deb13u1": true, "10.11.6-MariaDB": true, "10.6.18-MariaDB": false, "": false, "8.0.36": false}
	for v, want := range cases {
		if got := SupportsBindList(v); got != want {
			t.Errorf("SupportsBindList(%q) = %v", v, got)
		}
	}
	if !strings.Contains(TuningConfig(1<<30, false), "bind-address                   = 127.0.0.1\n") {
		t.Error("ipv4-only bind not rendered")
	}
}

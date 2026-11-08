package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "txData", "ESX.base"), 0o750)
	_ = os.MkdirAll(filepath.Join(dir, "txData", "Other.base"), 0o750)
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "server.cfg"), []byte("ensure oxmysql\n"), 0o640)
	_ = os.WriteFile(filepath.Join(dir, "txData", "ESX.base", "server.cfg"), []byte("set mysql_slow_query_warning 300\nensure oxmysql\n"), 0o640)
	_ = os.WriteFile(filepath.Join(dir, "txData", "Other.base", "server.cfg"), []byte("# set mysql_slow_query_warning 50\nensure x\n"), 0o640)
	_ = os.WriteFile(filepath.Join(outside, "server.cfg"), []byte("x\n"), 0o640)
	_ = os.Symlink(outside, filepath.Join(dir, "txData", "Evil.base"))

	tpl := &Template{ConfigDefaults: []ConfigDefault{{Files: []string{"server.cfg", "txData/*/server.cfg"}, Key: "mysql_slow_query_warning", Line: "set mysql_slow_query_warning 1500"}}}
	changed, err := ApplyConfigDefaults(dir, tpl)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, ",") != "server.cfg,txData/Other.base/server.cfg" {
		t.Errorf("changed = %v", changed)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "server.cfg"))
	if !strings.HasPrefix(string(b), configDefaultMarker+"\nset mysql_slow_query_warning 1500\n\nensure oxmysql") {
		t.Errorf("root cfg = %q", b)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "txData", "ESX.base", "server.cfg"))
	if strings.Contains(string(b), "1500") {
		t.Error("user's own value was overridden")
	}
	b, _ = os.ReadFile(filepath.Join(outside, "server.cfg"))
	if string(b) != "x\n" {
		t.Error("file outside the server dir was modified through a symlink")
	}
	// Idempotent.
	if changed, _ := ApplyConfigDefaults(dir, tpl); len(changed) != 0 {
		t.Errorf("second run changed %v", changed)
	}
	if _, err := Parse([]byte("id: x\nname: X\ncategory: other\nstart: {command: x}\nconfigDefaults: [{files: [../x], key: a, line: b}]\n")); err == nil {
		t.Error("traversal glob accepted")
	}
}

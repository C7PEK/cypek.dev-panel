package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareScriptRuns(t *testing.T) {
	dir := t.TempDir()
	content := "#!/bin/ash\r\napt update\r\napt install -y curl jq\r\nmkdir -p /mnt/server/sub\r\ncd /mnt/server\r\necho \"v=${MC_VERSION}\" > /mnt/server/sub/out.txt\r\njava21 -version 2>/dev/null || echo nojava\r\n"
	script := PrepareScript(content, dir)
	if strings.Contains(script, "/mnt/server") || strings.Contains(script, "\r") || strings.Contains(script, "#!/bin/ash") {
		t.Errorf("not normalised:\n%s", script)
	}
	path := filepath.Join(dir, "install.sh")
	_ = os.WriteFile(path, []byte(script), 0o700)
	cmd := exec.Command("/bin/bash", path)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "SERVER_DIR=" + dir, "MC_VERSION=1.21.11; rm -rf /"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "skipped (no root): apt install -y curl jq") {
		t.Errorf("apt not neutralised:\n%s", out)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "sub", "out.txt"))
	if string(b) != "v=1.21.11; rm -rf /\n" {
		t.Errorf("out.txt = %q", b)
	}
}

func TestMissingTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got := MissingTools("curl -s x | jq -r .a\nxz -d f.xz\necho jquery\n")
	if strings.Join(got, ",") != "curl,jq,xz-utils" {
		t.Fatalf("got %v", got)
	}
	if got := MissingTools("echo hi\n"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

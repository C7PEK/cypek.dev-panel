package runtimes

import (
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
)

func TestRealNodeAndPython(t *testing.T) {
	if os.Getenv("HOMEPANEL_REAL_INSTALL") != "1" {
		t.Skip("set HOMEPANEL_REAL_INSTALL=1 to download real runtimes")
	}
	root := os.Getenv("HOMEPANEL_REAL_DIR")
	if root == "" {
		root = t.TempDir()
	}
	dir := filepath.Join(root, "runtimes")
	logf := func(f string, a ...any) { t.Logf(f, a...) }
	ctx := context.Background()

	node := &Node{Dir: dir, Provider: providers.NewNode()}
	nodeBin, err := node.Ensure(ctx, "26", logf, nil)
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	out, err := osexec.Command(nodeBin, "-e", "console.log('node ok', process.version)").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "node ok v26") {
		t.Errorf("node run: %s %v", out, err)
	}
	npm := filepath.Join(filepath.Dir(nodeBin), "npm")
	out, err = osexec.Command(nodeBin, npm, "--version").CombinedOutput()
	if err != nil {
		t.Errorf("npm: %s %v", out, err)
	}

	py := &Python{Dir: dir, Provider: providers.NewPython()}
	pyBin, err := py.Ensure(ctx, "3.12", logf, nil)
	if err != nil {
		t.Fatalf("python: %v", err)
	}
	venv := filepath.Join(t.TempDir(), "venv")
	if out, err := osexec.Command(pyBin, "-m", "venv", venv).CombinedOutput(); err != nil {
		t.Fatalf("venv: %s %v", out, err)
	}
	out, err = osexec.Command(filepath.Join(venv, "bin", "pip"), "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "pip") {
		t.Errorf("pip: %s %v", out, err)
	}
	fmt.Println(string(out))
}

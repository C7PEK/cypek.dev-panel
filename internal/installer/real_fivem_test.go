package installer

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
	builtin "github.com/C7PEK/cypek.dev-panel/templates"
)

// TestRealFiveMInstall downloads the real FXServer artifact (~70 MB) and
// cfx-server-data, boots the server through the shim and watches the console
// for a minute. Without a valid key (HOMEPANEL_FIVEM_KEY) it only proves the
// artifact starts on this host; with one it waits for the ready line.
// Opt-in: HOMEPANEL_REAL_INSTALL=1, HOMEPANEL_REAL_DIR optional.
func TestRealFiveMInstall(t *testing.T) {
	if os.Getenv("HOMEPANEL_REAL_INSTALL") != "1" {
		t.Skip("set HOMEPANEL_REAL_INSTALL=1 to download real artifacts")
	}
	root := os.Getenv("HOMEPANEL_REAL_DIR")
	if root == "" {
		root = t.TempDir()
	}
	key := os.Getenv("HOMEPANEL_FIVEM_KEY")
	if key == "" {
		key = "cfxk_placeholder_key_for_startup_test_only"
	}
	serverDir := filepath.Join(root, "servers", "fxtest00")
	t.Cleanup(func() { os.RemoveAll(serverDir) })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	m := New(Deps{FiveM: providers.NewFiveM(), Log: log})
	reg, _ := templates.Load(builtin.Builtin, "")
	tpl, _ := reg.Get("fivem")
	vars, err := tpl.ValidateValues(map[string]string{"LICENSE_KEY": key, "PORT": "30199"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	done := make(chan struct{})
	var res *Result
	var ierr error
	if err := m.Start(ctx, "fxtest00", serverDir, tpl, vars, func(r *Result, e error) { res, ierr = r, e; close(done) }); err != nil {
		t.Fatal(err)
	}
	<-done
	job, _ := m.Get("fxtest00")
	t.Logf("install log:\n%s", strings.Join(job.Lines, "\n"))
	if ierr != nil {
		t.Fatalf("install: %v", ierr)
	}
	for _, f := range []string{"artifact/run.sh", "artifact/alpine/opt/cfx-server/FXServer", "resources/[gamemodes]/[maps]/fivem-map-hipster/fxmanifest.lua", "server.cfg"} {
		if _, err := os.Stat(filepath.Join(serverDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(serverDir, "server.cfg"))
	if !strings.Contains(string(cfg), "sv_licenseKey \""+key+"\"") || !strings.Contains(string(cfg), "endpoint_add_udp \"0.0.0.0:30199\"") {
		t.Errorf("server.cfg:\n%s", cfg)
	}
	_ = shim.SaveRunConfig(serverDir, &res.Run)
	rc, _ := shim.LoadRunConfig(serverDir)
	sock := filepath.Join(os.TempDir(), "hp-fxtest.sock")
	s, err := shim.New(rc, shim.Options{ID: "fxtest00", ServerDir: serverDir, SocketPath: sock, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	exit := make(chan int, 1)
	go func() {
		code, _ := s.Run(runCtx)
		exit <- code
	}()
	var c *shim.Client
	for i := 0; i < 100 && c == nil; i++ {
		time.Sleep(100 * time.Millisecond)
		c, _ = shim.Dial(ctx, sock)
	}
	if c == nil {
		t.Fatal("shim socket never came up")
	}
	defer c.Close()
	var out strings.Builder
	deadline := time.After(90 * time.Second)
	ready := c.Hello.State == shim.StateRunning
loop:
	for {
		select {
		case f, ok := <-c.Frames():
			if !ok {
				break loop
			}
			if f.Type == shim.TypeOutput {
				out.WriteString(f.Data)
			}
			if f.Type == shim.TypeState && f.State == shim.StateRunning {
				ready = true
				break loop
			}
			if f.Type == shim.TypeState && f.State == shim.StateStopped {
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	t.Logf("ready=%v console tail:\n%s", ready, tail(out.String(), 40))
	if !strings.Contains(out.String(), "FXServer") && !strings.Contains(out.String(), "citizen") && !strings.Contains(out.String(), "resource") {
		t.Errorf("FXServer produced no recognisable output")
	}
	runCancel()
	select {
	case <-exit:
	case <-time.After(2 * time.Minute):
		t.Error("server did not stop")
	}
}

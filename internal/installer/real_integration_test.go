package installer

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cypek019/cypek.dev---panel/internal/providers"
	"github.com/cypek019/cypek.dev---panel/internal/runtimes"
	"github.com/cypek019/cypek.dev---panel/internal/shim"
	"github.com/cypek019/cypek.dev---panel/internal/templates"
	builtin "github.com/cypek019/cypek.dev---panel/templates"
)

// TestRealPaperInstall downloads the real Paper jar and a real Temurin JRE
// (~110 MB), then boots the server through the shim until it reports ready
// and stops it. Opt-in: HOMEPANEL_REAL_INSTALL=1, optional HOMEPANEL_REAL_DIR
// for the data root (default a temp dir; use /var/lib/homepanel to prime the
// runtime cache on a real host).
func TestRealPaperInstall(t *testing.T) {
	if os.Getenv("HOMEPANEL_REAL_INSTALL") != "1" {
		t.Skip("set HOMEPANEL_REAL_INSTALL=1 to download real artifacts")
	}
	root := os.Getenv("HOMEPANEL_REAL_DIR")
	if root == "" {
		root = t.TempDir()
	}
	serverDir := filepath.Join(root, "servers", "realtest")
	t.Cleanup(func() { os.RemoveAll(serverDir) })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	m := New(Deps{Paper: providers.NewPaper(), Java: &runtimes.Java{Dir: filepath.Join(root, "runtimes"), Adoptium: providers.NewAdoptium()}, Log: log})
	reg, _ := templates.Load(builtin.Builtin, "")
	tpl, _ := reg.Get("minecraft-paper")
	vars, err := tpl.ValidateValues(map[string]string{"MC_VERSION": "latest", "EULA": "true", "RAM_MB": "1024", "PORT": "25599", "MOTD": "panel.cypek.dev test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	done := make(chan struct{})
	var res *Result
	var ierr error
	if err := m.Start(ctx, "realtest", serverDir, tpl, vars, func(r *Result, e error) { res, ierr = r, e; close(done) }); err != nil {
		t.Fatal(err)
	}
	<-done
	job, _ := m.Get("realtest")
	t.Logf("install log:\n%s", strings.Join(job.Lines, "\n"))
	if ierr != nil {
		t.Fatalf("install: %v", ierr)
	}
	if err := shim.SaveRunConfig(serverDir, &res.Run); err != nil {
		t.Fatal(err)
	}
	cfg, _ := shim.LoadRunConfig(serverDir)
	sock := filepath.Join(os.TempDir(), "hp-realtest.sock")
	s, err := shim.New(cfg, shim.Options{ID: "realtest", ServerDir: serverDir, SocketPath: sock, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	exit := make(chan int, 1)
	go func() {
		code, err := s.Run(runCtx)
		if err != nil {
			t.Errorf("shim: %v", err)
		}
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
	start := time.Now()
	var out strings.Builder
	ready := c.Hello.State == shim.StateRunning
	for !ready {
		select {
		case f, ok := <-c.Frames():
			if !ok {
				t.Fatalf("shim closed before ready; output:\n%s", tail(out.String(), 40))
			}
			if f.Type == shim.TypeOutput {
				out.WriteString(f.Data)
			}
			if f.Type == shim.TypeState && f.State == shim.StateRunning {
				ready = true
			}
			if f.Type == shim.TypeState && f.State == shim.StateStopped {
				t.Fatalf("server exited before ready; output:\n%s", tail(out.String(), 60))
			}
		case <-time.After(4 * time.Minute):
			t.Fatalf("server not ready after 4 minutes; output:\n%s", tail(out.String(), 60))
		}
	}
	t.Logf("Paper ready after %s", time.Since(start).Round(time.Second))
	_ = c.Send(shim.Frame{Type: shim.TypeStop})
	select {
	case code := <-exit:
		if code != 0 {
			t.Errorf("exit code %d", code)
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("server did not stop")
	}
}

func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

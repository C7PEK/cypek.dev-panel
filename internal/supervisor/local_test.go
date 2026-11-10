package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/shim"
)

// The test binary plays two extra roles: `<bin> shim ...` runs a real shim
// (what the local supervisor spawns) and HOMEPANEL_FAKE_GAME=1 is the game.
func TestMain(m *testing.M) {
	if os.Getenv("HOMEPANEL_FAKE_GAME") == "1" {
		os.Exit(fakeGame())
	}
	if len(os.Args) > 1 && os.Args[1] == "shim" {
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		os.Exit(shim.Main(os.Args[2:], "", log))
	}
	os.Exit(m.Run())
}

func fakeGame() int {
	fmt.Println("Done (0.01s)!")
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch line {
		case "stop":
			fmt.Println("bye")
			return 0
		case "crash":
			return 7
		default:
			fmt.Printf("> %s\n", line)
		}
	}
	return 0
}

func newLocal(t *testing.T) (*Local, string) {
	t.Helper()
	root, err := os.MkdirTemp("", "hp-local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	layout := Layout{ServersDir: filepath.Join(root, "servers"), RunDir: filepath.Join(root, "run")}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sup := NewLocal(layout, os.Args[0], log)
	id := "abcd1234"
	dir := layout.ServerDir(id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfg := &shim.RunConfig{
		Command:    os.Args[0],
		Env:        map[string]string{"HOMEPANEL_FAKE_GAME": "1"},
		Stop:       shim.StopConfig{Command: "stop", TimeoutSec: 5},
		ReadyRegex: `Done`,
	}
	if err := shim.SaveRunConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	return sup, id
}

func waitState(t *testing.T, sup Supervisor, id string, want State) Status {
	t.Helper()
	var last Status
	ok := waitUntil(context.Background(), 10*time.Second, func() bool {
		st, err := sup.Status(context.Background(), id)
		if err != nil {
			return false
		}
		last = st
		return st.State == want
	})
	if !ok {
		t.Fatalf("state = %s, want %s", last.State, want)
	}
	return last
}

func TestLocalLifecycle(t *testing.T) {
	sup, id := newLocal(t)
	ctx := context.Background()

	st, err := sup.Status(ctx, id)
	if err != nil || st.State != StateStopped {
		t.Fatalf("initial status = %+v, %v", st, err)
	}
	if err := sup.Stop(ctx, id); !errors.Is(err, ErrNotRunning) {
		t.Errorf("stop when stopped: %v", err)
	}
	if err := sup.Start(ctx, id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := sup.Start(ctx, id); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("double start: %v", err)
	}
	st = waitState(t, sup, id, StateRunning)
	if st.PID == 0 || st.Since.IsZero() {
		t.Errorf("running status = %+v", st)
	}
	stats, err := sup.Stats(ctx, id)
	if err != nil || stats.MemoryBytes == 0 {
		t.Errorf("stats = %+v, %v", stats, err)
	}

	// Console round trip through Attach.
	c, err := sup.Attach(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Input("ping\n")
	deadline := time.After(5 * time.Second)
	var out strings.Builder
	for !strings.Contains(out.String(), "> ping") {
		select {
		case f, ok := <-c.Frames():
			if !ok {
				t.Fatalf("console closed; got %q", out.String())
			}
			if f.Type == shim.TypeOutput {
				out.WriteString(f.Data)
			}
		case <-deadline:
			t.Fatalf("no echo; got %q", out.String())
		}
	}
	c.Close()

	if err := sup.Restart(ctx, id); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	st2 := waitState(t, sup, id, StateRunning)
	if st2.PID == st.PID {
		t.Error("restart kept the same pid")
	}

	if err := sup.Stop(ctx, id); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st = waitState(t, sup, id, StateStopped)
	if st.ExitCode == nil || *st.ExitCode != 0 {
		t.Errorf("stopped status = %+v", st)
	}
}

func TestLocalCrashAndKill(t *testing.T) {
	sup, id := newLocal(t)
	ctx := context.Background()
	if err := sup.Start(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitState(t, sup, id, StateRunning)
	c, err := sup.Attach(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Input("crash\n")
	// Closing right away can drop the frame before the shim reads it.
	st := waitState(t, sup, id, StateCrashed)
	c.Close()
	if st.ExitCode == nil || *st.ExitCode != 7 {
		t.Errorf("crashed status = %+v", st)
	}

	if err := sup.Start(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitState(t, sup, id, StateRunning)
	if err := sup.Kill(ctx, id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	st = waitState(t, sup, id, StateStopped) // requested kill is not a crash
	if st.ExitCode == nil || *st.ExitCode != 137 {
		t.Errorf("killed status = %+v", st)
	}
}

func TestLocalRejectsBadIDs(t *testing.T) {
	sup, _ := newLocal(t)
	for _, id := range []string{"", "../x", "ABC", "a b", "x/y", strings.Repeat("a", 40)} {
		if err := sup.Start(context.Background(), id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Start(%q) = %v, want ErrInvalidID", id, err)
		}
		if _, err := sup.Status(context.Background(), id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Status(%q) = %v", id, err)
		}
	}
}

func TestLocalStartFailsFastOnBadCommand(t *testing.T) {
	sup, id := newLocal(t)
	dir := sup.layout.ServerDir(id)
	cfg, _ := shim.LoadRunConfig(dir)
	cfg.Command = "/nonexistent/binary"
	_ = shim.SaveRunConfig(dir, cfg)
	err := sup.Start(context.Background(), id)
	if err == nil || !strings.Contains(err.Error(), "exited immediately") {
		t.Fatalf("Start with bad command: %v", err)
	}
}

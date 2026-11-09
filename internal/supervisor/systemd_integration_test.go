package supervisor

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/shim"
)

// TestSystemdReal drives the real homepanel-server@ template unit over D-Bus.
// It needs an installed binary, the unit, the polkit rule and write access to
// /var/lib/homepanel/servers, so it only runs when HOMEPANEL_SYSTEMD_TEST=1
// (ideally as the homepanel user, to exercise the polkit rule).
func TestSystemdReal(t *testing.T) {
	if os.Getenv("HOMEPANEL_SYSTEMD_TEST") != "1" {
		t.Skip("set HOMEPANEL_SYSTEMD_TEST=1 to run against the real systemd")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	layout := Layout{ServersDir: "/var/lib/homepanel/servers", RunDir: "/run", FlatRun: true}
	sup, err := NewSystemd(ctx, layout, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	id := "sdtest01"
	dir := layout.ServerDir(id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sup.Remove(context.Background(), id)
		os.RemoveAll(dir)
	})
	// /bin/cat echoes stdin and exits on SIGTERM: a minimal "game".
	cfg := &shim.RunConfig{Command: "/bin/cat", Stop: shim.StopConfig{TimeoutSec: 10}}
	if err := shim.SaveRunConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}

	if st, err := sup.Status(ctx, id); err != nil || st.State != StateStopped {
		t.Fatalf("initial status = %+v, %v", st, err)
	}
	if err := sup.SetLimits(ctx, id, Limits{MemoryMB: 256, CPUPercent: 50}); err != nil {
		t.Fatalf("SetLimits: %v", err)
	}
	if err := sup.SetAutostart(ctx, id, true); err != nil {
		t.Fatalf("SetAutostart on: %v", err)
	}
	if err := sup.SetAutostart(ctx, id, false); err != nil {
		t.Fatalf("SetAutostart off: %v", err)
	}
	if err := sup.Start(ctx, id); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st := waitState(t, sup, id, StateRunning)
	if st.PID == 0 {
		t.Errorf("running status = %+v", st)
	}
	stats, err := sup.Stats(ctx, id)
	if err != nil || stats.MemoryBytes == 0 {
		t.Errorf("Stats = %+v, %v", stats, err)
	}
	c, err := sup.Attach(ctx, id)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = c.Input("ping from systemd test\n")
	var out strings.Builder
	deadline := time.After(5 * time.Second)
	for !strings.Contains(out.String(), "ping from systemd test") {
		select {
		case f, ok := <-c.Frames():
			if !ok {
				t.Fatalf("console closed; out=%q", out.String())
			}
			if f.Type == shim.TypeOutput {
				out.WriteString(f.Data)
			}
		case <-deadline:
			t.Fatalf("no echo; out=%q", out.String())
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
	if err := sup.Kill(ctx, id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	st = waitState(t, sup, id, StateStopped)
	if st.ExitCode == nil || *st.ExitCode != 137 {
		t.Errorf("killed status = %+v", st)
	}
	if err := sup.Start(ctx, id); err != nil {
		t.Fatalf("Start after kill: %v", err)
	}
	waitState(t, sup, id, StateRunning)
	if err := sup.Stop(ctx, id); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st = waitState(t, sup, id, StateStopped)
	if st.ExitCode == nil {
		t.Errorf("stopped status = %+v", st)
	}
	if err := sup.Remove(ctx, id); err != nil {
		t.Errorf("Remove: %v", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

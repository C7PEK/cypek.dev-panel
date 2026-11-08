package metrics

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cypek019/cypek.dev---panel/internal/supervisor"
)

type fakeSource struct {
	nanos uint64
}

func (f *fakeSource) ServerIDs(context.Context) ([]string, error) { return []string{"a"}, nil }
func (f *fakeSource) Status(context.Context, string) (supervisor.Status, error) {
	f.nanos += 500_000_000 // half a core-second per call
	return supervisor.Status{State: supervisor.StateRunning, MemoryBytes: 1 << 20, CPUNanos: f.nanos}, nil
}

func TestCollectorCPUAndHistory(t *testing.T) {
	src := &fakeSource{}
	c := New(src, time.Second, 3, os.TempDir(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	var got int
	unsub := c.Subscribe(func(h Host, s map[string]Sample) { got++ })
	ctx := context.Background()
	c.tick(ctx)
	time.Sleep(100 * time.Millisecond)
	c.tick(ctx)
	hist := c.History("a")
	if len(hist) != 2 || hist[1].CPUPercent <= 0 || hist[1].MemoryBytes != 1<<20 {
		t.Errorf("history = %+v", hist)
	}
	// 0.5 core-second over ~0.1 s is ~500%; just sanity-check the order of magnitude.
	if hist[1].CPUPercent < 100 || hist[1].CPUPercent > 2000 {
		t.Errorf("cpu%% = %v", hist[1].CPUPercent)
	}
	c.tick(ctx)
	c.tick(ctx)
	if len(c.History("a")) != 3 {
		t.Errorf("history not capped: %d", len(c.History("a")))
	}
	if got != 4 {
		t.Errorf("listener calls = %d", got)
	}
	unsub()
	c.tick(ctx)
	if got != 4 {
		t.Error("listener called after unsubscribe")
	}
	h := c.Host()
	if h.CPUCount == 0 || h.MemTotal == 0 || h.DiskTotal == 0 {
		t.Errorf("host sample incomplete: %+v", h)
	}
}

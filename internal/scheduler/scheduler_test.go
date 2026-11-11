package scheduler

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/db"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

type fakeSup struct {
	mu     sync.Mutex
	state  supervisor.State
	calls  []string
	inputs []string
}

func (f *fakeSup) rec(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakeSup) Start(context.Context, string) error {
	f.rec("start")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state == supervisor.StateRunning {
		return supervisor.ErrAlreadyRunning
	}
	f.state = supervisor.StateRunning
	return nil
}
func (f *fakeSup) Stop(context.Context, string) error {
	f.rec("stop")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state != supervisor.StateRunning {
		return supervisor.ErrNotRunning
	}
	f.state = supervisor.StateStopped
	return nil
}
func (f *fakeSup) Restart(ctx context.Context, id string) error {
	f.rec("restart")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state != supervisor.StateRunning {
		return supervisor.ErrNotRunning
	}
	return nil
}
func (f *fakeSup) Kill(context.Context, string) error { f.rec("kill"); return nil }
func (f *fakeSup) Status(context.Context, string) (supervisor.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return supervisor.Status{State: f.state}, nil
}
func (f *fakeSup) SetLimits(context.Context, string, supervisor.Limits) error { return nil }
func (f *fakeSup) SetAutostart(context.Context, string, bool) error           { return nil }
func (f *fakeSup) Attach(context.Context, string) (supervisor.Console, error) {
	return nil, supervisor.ErrNotRunning
}
func (f *fakeSup) Stats(context.Context, string) (supervisor.Stats, error) {
	return supervisor.Stats{}, nil
}
func (f *fakeSup) Remove(context.Context, string) error { return nil }
func (f *fakeSup) Name() string                         { return "fake" }

var _ supervisor.Supervisor = (*fakeSup)(nil)

func newStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := db.Migrate(context.Background(), sqlDB, "", log); err != nil {
		t.Fatal(err)
	}

	if _, err := sqlDB.Exec(`INSERT INTO servers (id, name, run_config, created_at, updated_at) VALUES ('abcd1234','s','{}','x','x')`); err != nil {
		t.Fatal(err)
	}
	return NewStore(sqlDB)
}

func TestValidateAndNextRuns(t *testing.T) {
	good := &Schedule{Name: "nightly", CronExpr: "0 4 * * *", Actions: []Action{{Type: "warn", Message: "Restart in 1 minute"}, {Type: "wait", Seconds: 60}, {Type: "restart"}}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	if n, err := NextRuns("@daily", time.UTC, 3); err != nil || len(n) != 3 || n[1].Sub(n[0]) != 24*time.Hour {
		t.Errorf("NextRuns = %v, %v", n, err)
	}
	bad := []*Schedule{
		{Name: "", CronExpr: "* * * * *", Actions: []Action{{Type: "stop"}}},
		{Name: "x", CronExpr: "every day", Actions: []Action{{Type: "stop"}}},
		{Name: "x", CronExpr: "* * * * * *", Actions: []Action{{Type: "stop"}}},
		{Name: "x", CronExpr: "* * * * *", Actions: nil},
		{Name: "x", CronExpr: "* * * * *", Actions: []Action{{Type: "explode"}}},
		{Name: "x", CronExpr: "* * * * *", Actions: []Action{{Type: "warn"}}},
		{Name: "x", CronExpr: "* * * * *", Actions: []Action{{Type: "command", Text: "a\nb"}}},
		{Name: "x", CronExpr: "* * * * *", Actions: []Action{{Type: "wait", Seconds: 0}}},
		{Name: "x", CronExpr: "* * * * *", Actions: []Action{{Type: "wait", Seconds: 3600}, {Type: "wait", Seconds: 3600}, {Type: "wait", Seconds: 3600}}},
	}
	for i, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("bad schedule %d accepted", i)
		}
	}
}

func TestStoreCRUDAndRuns(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	s, err := st.Create(ctx, &Schedule{ServerID: "abcd1234", Name: "n", CronExpr: "*/5 * * * *", Enabled: true, Actions: []Action{{Type: "stop"}}, OnlyIfRunning: true})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := st.List(ctx, "abcd1234")
	if len(list) != 1 || list[0].ID != s.ID || !list[0].OnlyIfRunning {
		t.Errorf("list = %+v", list)
	}
	s.Name = "renamed"
	s.Enabled = false
	if err := st.Update(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(ctx, s.ID)
	if got.Name != "renamed" || got.Enabled {
		t.Errorf("after update = %+v", got)
	}
	runID, _ := st.startRun(ctx, s.ID, "manual")
	st.finishRun(ctx, runID, "ok", "done")
	runs, _ := st.Runs(ctx, s.ID, 10)
	if len(runs) != 1 || runs[0].Result != "ok" || runs[0].Log != "done" {
		t.Errorf("runs = %+v", runs)
	}
	for i := 0; i < 60; i++ {
		id, _ := st.startRun(ctx, s.ID, "cron")
		st.finishRun(ctx, id, "ok", "")
	}
	st.pruneRuns(ctx, s.ID, 50)
	runs, _ = st.Runs(ctx, s.ID, 100)
	if len(runs) != 50 {
		t.Errorf("prune kept %d", len(runs))
	}
	if err := st.Delete(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(ctx, s.ID); err != ErrNotFound {
		t.Errorf("after delete: %v", err)
	}
	if runs, _ := st.Runs(ctx, s.ID, 10); len(runs) != 0 {
		t.Error("runs not cascaded")
	}
}

func TestEngineExecute(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sup := &fakeSup{state: supervisor.StateRunning}
	var players int
	target := Target{
		WarnCommand: func(_ context.Context, _ string, msg string) (string, bool, error) { return "say " + msg, true, nil },
		Players:     func(context.Context, string) int { return players },
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	e := New(st, sup, target, log)

	s, _ := st.Create(ctx, &Schedule{ServerID: "abcd1234", Name: "restart", CronExpr: "0 4 * * *", Enabled: true,
		Actions: []Action{{Type: "wait", Seconds: 1}, {Type: "restart"}}, OnlyIfRunning: true, SkipIfPlayers: true})

	players = 2
	if err := e.RunNow(s.ID); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, e, s.ID)
	runs, _ := st.Runs(ctx, s.ID, 5)
	if len(runs) != 1 || runs[0].Result != "skipped" || !strings.Contains(runs[0].Log, "player") {
		t.Fatalf("runs = %+v", runs)
	}

	players = 0
	_ = e.RunNow(s.ID)
	time.Sleep(200 * time.Millisecond)
	if !e.IsRunning(s.ID) {
		t.Error("expected the chain to be running during the wait")
	}
	if err := e.RunNow(s.ID); err == nil {
		t.Error("second RunNow while running should fail")
	}
	waitIdle(t, e, s.ID)
	runs, _ = st.Runs(ctx, s.ID, 5)
	if runs[0].Result != "ok" || !strings.Contains(runs[0].Log, "Restarting") {
		t.Errorf("run = %+v", runs[0])
	}
	if strings.Join(sup.calls, ",") != "restart" {
		t.Errorf("calls = %v", sup.calls)
	}
	got, _ := st.Get(ctx, s.ID)
	if got.LastRunAt == "" || got.NextRunAt == "" {
		t.Errorf("times not recorded: %+v", got)
	}

	sup.state = supervisor.StateStopped
	_ = e.RunNow(s.ID)
	waitIdle(t, e, s.ID)
	runs, _ = st.Runs(ctx, s.ID, 5)
	if runs[0].Result != "skipped" {
		t.Errorf("stopped run = %+v", runs[0])
	}
	s2, _ := st.Create(ctx, &Schedule{ServerID: "abcd1234", Name: "warn", CronExpr: "0 4 * * *", Enabled: true,
		Actions: []Action{{Type: "warn", Message: "hi"}}, OnlyIfRunning: false})
	_ = e.RunNow(s2.ID)
	waitIdle(t, e, s2.ID)
	runs, _ = st.Runs(ctx, s2.ID, 5)
	if runs[0].Result != "failed" || !strings.Contains(runs[0].Log, "failed") {
		t.Errorf("warn run = %+v", runs[0])
	}

	s3, _ := st.Create(ctx, &Schedule{ServerID: "abcd1234", Name: "long", CronExpr: "0 4 * * *", Enabled: true,
		Actions: []Action{{Type: "wait", Seconds: 600}}, OnlyIfRunning: false})
	_ = e.RunNow(s3.ID)
	time.Sleep(100 * time.Millisecond)
	if !e.Cancel(s3.ID) {
		t.Error("cancel found nothing running")
	}
	waitIdle(t, e, s3.ID)
	runs, _ = st.Runs(ctx, s3.ID, 5)
	if runs[0].Result != "failed" || !strings.Contains(runs[0].Log, "ancel") {
		t.Errorf("cancelled run = %+v", runs[0])
	}
	_ = shim.MaxInputBytes
}

func waitIdle(t *testing.T, e *Engine, id int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if !e.IsRunning(id) {
			time.Sleep(100 * time.Millisecond)
			return
		}
	}
	t.Fatal("chain did not finish")
}

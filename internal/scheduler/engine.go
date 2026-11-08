package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/cypek019/cypek.dev---panel/internal/db"
	"github.com/cypek019/cypek.dev---panel/internal/supervisor"
)

// Target is what the engine needs from the rest of the panel for one server.
type Target struct {
	// WarnCommand renders the template's warn command for a message; ok=false
	// when the server has none.
	WarnCommand func(ctx context.Context, serverID, message string) (cmd string, ok bool, err error)
	// Players returns the current player count (-1 when unknown).
	Players func(ctx context.Context, serverID string) int
	// Backup creates a backup; may return ErrBackupsUnavailable.
	Backup func(ctx context.Context, serverID string, logf func(string, ...any)) error
}

// ErrBackupsUnavailable is returned by Target.Backup until backups exist.
var ErrBackupsUnavailable = errors.New("backups are not available yet")

// Engine owns the cron instance and executes chains.
type Engine struct {
	store  *Store
	sup    supervisor.Supervisor
	target Target
	log    *slog.Logger
	loc    *time.Location

	cron    *cron.Cron
	mu      sync.Mutex
	entries map[int64]cron.EntryID
	running map[int64]context.CancelFunc
	ctx     context.Context
}

// New creates an engine in the system time zone.
func New(store *Store, sup supervisor.Supervisor, target Target, log *slog.Logger) *Engine {
	loc := time.Local
	return &Engine{store: store, sup: sup, target: target, log: log, loc: loc,
		cron: cron.New(cron.WithLocation(loc), cron.WithParser(Parser)), entries: map[int64]cron.EntryID{}, running: map[int64]context.CancelFunc{}}
}

// Location is the time zone schedules are interpreted in.
func (e *Engine) Location() *time.Location { return e.loc }

// Start loads all enabled schedules and starts the cron. Missed runs are
// logged, never caught up.
func (e *Engine) Start(ctx context.Context) error {
	e.ctx = ctx
	all, err := e.store.List(ctx, "")
	if err != nil {
		return err
	}
	now := time.Now()
	for _, s := range all {
		if s.NextRunAt != "" {
			if t, err := db.ParseTime(s.NextRunAt); err == nil && t.Before(now) && s.Enabled {
				e.log.Warn("schedule missed while the panel was down (not caught up)", "schedule", s.ID, "name", s.Name, "due", t.In(e.loc).Format(time.RFC3339))
			}
		}
		if s.Enabled {
			e.add(s)
		}
	}
	e.cron.Start()
	go func() {
		<-ctx.Done()
		stop := e.cron.Stop()
		<-stop.Done()
	}()
	return nil
}

// Reload (re)registers one schedule after create/update/delete.
func (e *Engine) Reload(ctx context.Context, id int64) {
	e.mu.Lock()
	if entry, ok := e.entries[id]; ok {
		e.cron.Remove(entry)
		delete(e.entries, id)
	}
	e.mu.Unlock()
	s, err := e.store.Get(ctx, id)
	if err != nil {
		return
	}
	if s.Enabled {
		e.add(s)
	} else {
		e.store.setTimes(ctx, id, "", "")
	}
}

func (e *Engine) add(s *Schedule) {
	id := s.ID
	entry, err := e.cron.AddFunc(s.CronExpr, func() { e.fire(id, "cron") })
	if err != nil {
		e.log.Error("schedule has an invalid cron expression", "schedule", id, "expr", s.CronExpr, "err", err)
		return
	}
	e.mu.Lock()
	e.entries[id] = entry
	e.mu.Unlock()
	if next, err := NextRuns(s.CronExpr, e.loc, 1); err == nil && len(next) == 1 {
		e.store.setTimes(context.Background(), id, "", next[0].UTC().Format(time.RFC3339Nano))
	}
}

// RunNow executes a schedule immediately (manual trigger).
func (e *Engine) RunNow(id int64) error {
	e.mu.Lock()
	_, busy := e.running[id]
	e.mu.Unlock()
	if busy {
		return errors.New("this schedule is already running")
	}
	go e.fire(id, "manual")
	return nil
}

// Cancel aborts a running chain.
func (e *Engine) Cancel(id int64) bool {
	e.mu.Lock()
	cancel, ok := e.running[id]
	e.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// IsRunning reports whether a chain is executing right now.
func (e *Engine) IsRunning(id int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.running[id]
	return ok
}

func (e *Engine) fire(id int64, trigger string) {
	base := e.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, MaxChainDuration+10*time.Minute)
	defer cancel()
	e.mu.Lock()
	if _, busy := e.running[id]; busy {
		e.mu.Unlock()
		e.log.Warn("schedule skipped: previous run still in progress", "schedule", id)
		return
	}
	e.running[id] = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.running, id)
		e.mu.Unlock()
	}()

	// Bookkeeping must survive a cancelled chain, so it uses a context that
	// is not cancelled with the run itself.
	bg := context.WithoutCancel(ctx)
	s, err := e.store.Get(bg, id)
	if err != nil {
		return
	}
	runID, err := e.store.startRun(bg, id, trigger)
	if err != nil {
		e.log.Error("start run", "schedule", id, "err", err)
		return
	}
	var logb strings.Builder
	logf := func(format string, args ...any) {
		line := time.Now().In(e.loc).Format("15:04:05") + " " + fmt.Sprintf(format, args...)
		logb.WriteString(line + "\n")
		e.store.updateRunLog(bg, runID, logb.String())
		e.log.Info("schedule", "schedule", id, "name", s.Name, "msg", line)
	}
	result := e.execute(ctx, s, logf)
	e.store.finishRun(bg, runID, result, logb.String())
	e.store.pruneRuns(bg, id, 50)
	next := ""
	if n, err := NextRuns(s.CronExpr, e.loc, 1); err == nil && len(n) == 1 && s.Enabled {
		next = n[0].UTC().Format(time.RFC3339Nano)
	}
	e.store.setTimes(bg, id, db.Now(), next)
}

// execute runs the chain and returns ok | skipped | failed.
func (e *Engine) execute(ctx context.Context, s *Schedule, logf func(string, ...any)) string {
	logf("Starting \"%s\"", s.Name)
	st, err := e.sup.Status(ctx, s.ServerID)
	if err != nil {
		logf("Could not read server status: %v", err)
		return "failed"
	}
	if s.OnlyIfRunning && st.State != supervisor.StateRunning {
		logf("Skipped: server is %s, not running", st.State)
		return "skipped"
	}
	if s.SkipIfPlayers && e.target.Players != nil {
		if n := e.target.Players(ctx, s.ServerID); n > 0 {
			logf("Skipped: %d player(s) online", n)
			return "skipped"
		}
	}
	for i, a := range s.Actions {
		if ctx.Err() != nil {
			logf("Cancelled")
			return "failed"
		}
		if err := e.runAction(ctx, s.ServerID, a, logf); err != nil {
			logf("Action %d (%s) failed: %v", i+1, a.Type, err)
			return "failed"
		}
	}
	logf("Done")
	return "ok"
}

func (e *Engine) runAction(ctx context.Context, serverID string, a Action, logf func(string, ...any)) error {
	switch a.Type {
	case "warn":
		if e.target.WarnCommand == nil {
			return errors.New("warnings are not configured")
		}
		cmd, ok, err := e.target.WarnCommand(ctx, serverID, a.Message)
		if err != nil {
			return err
		}
		if !ok {
			logf("Warning skipped: this server type has no announce command")
			return nil
		}
		logf("Warning: %s", a.Message)
		return e.console(ctx, serverID, cmd)
	case "command":
		logf("Command: %s", a.Text)
		return e.console(ctx, serverID, a.Text)
	case "wait":
		logf("Waiting %ds", a.Seconds)
		select {
		case <-time.After(time.Duration(a.Seconds) * time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case "stop":
		logf("Stopping server")
		err := e.sup.Stop(ctx, serverID)
		if errors.Is(err, supervisor.ErrNotRunning) {
			logf("Already stopped")
			return nil
		}
		return err
	case "start":
		logf("Starting server")
		err := e.sup.Start(ctx, serverID)
		if errors.Is(err, supervisor.ErrAlreadyRunning) {
			logf("Already running")
			return nil
		}
		return err
	case "restart":
		logf("Restarting server")
		err := e.sup.Restart(ctx, serverID)
		if errors.Is(err, supervisor.ErrNotRunning) {
			return e.sup.Start(ctx, serverID)
		}
		return err
	case "kill":
		logf("Force-stopping server")
		err := e.sup.Kill(ctx, serverID)
		if errors.Is(err, supervisor.ErrNotRunning) {
			return nil
		}
		return err
	case "backup":
		if e.target.Backup == nil {
			return ErrBackupsUnavailable
		}
		logf("Creating backup")
		return e.target.Backup(ctx, serverID, logf)
	}
	return fmt.Errorf("unknown action %q", a.Type)
}

// console sends one line to the server's stdin through the shim.
func (e *Engine) console(ctx context.Context, serverID, line string) error {
	c, err := e.sup.Attach(ctx, serverID)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Input(line + "\n")
}

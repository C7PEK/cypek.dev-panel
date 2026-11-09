// Package scheduler runs cron-triggered action chains against servers:
// warn players, wait, run console commands, stop/start/restart, back up.
package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/C7PEK/cypek.dev-panel/internal/db"
)

// Action is one step of a chain. Fields depend on Type:
//
//	warn    {message}               send the template's warn command
//	command {text}                  send a console line
//	wait    {seconds}               sleep
//	stop | start | restart | kill   power actions
//	backup  {}                      create a backup (when backups are available)
type Action struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Text    string `json:"text,omitempty"`
	Seconds int    `json:"seconds,omitempty"`
}

// Schedule is a saved job.
type Schedule struct {
	ID            int64    `json:"id"`
	ServerID      string   `json:"serverId"`
	Name          string   `json:"name"`
	CronExpr      string   `json:"cron"`
	Enabled       bool     `json:"enabled"`
	Actions       []Action `json:"actions"`
	OnlyIfRunning bool     `json:"onlyIfRunning"`
	SkipIfPlayers bool     `json:"skipIfPlayers"`
	CreatedBy     *int64   `json:"createdBy,omitempty"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
	LastRunAt     string   `json:"lastRunAt,omitempty"`
	NextRunAt     string   `json:"nextRunAt,omitempty"`
}

// Run is one execution record.
type Run struct {
	ID         int64  `json:"id"`
	ScheduleID int64  `json:"scheduleId"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt,omitempty"`
	Result     string `json:"result"` // running | ok | skipped | failed
	Trigger    string `json:"trigger"`
	Log        string `json:"log"`
}

var (
	ErrNotFound    = errors.New("schedule not found")
	ErrInvalidCron = errors.New("invalid cron expression")
	ErrInvalidName = errors.New("invalid schedule name")
)

// Parser accepts standard 5-field cron plus descriptors like @daily.
var Parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// MaxChainDuration bounds a whole chain (waits included).
const MaxChainDuration = 2 * time.Hour

// Validate checks a schedule definition.
func (s *Schedule) Validate() error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || len(s.Name) > 80 || strings.ContainsAny(s.Name, "\n\r") {
		return ErrInvalidName
	}
	if _, err := Parser.Parse(s.CronExpr); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCron, err)
	}
	if len(s.Actions) == 0 || len(s.Actions) > 32 {
		return errors.New("a schedule needs between 1 and 32 actions")
	}
	var total time.Duration
	for i := range s.Actions {
		a := &s.Actions[i]
		switch a.Type {
		case "warn":
			a.Message = strings.TrimSpace(a.Message)
			if a.Message == "" || len(a.Message) > 256 || strings.ContainsAny(a.Message, "\n\r") {
				return fmt.Errorf("action %d: warn needs a single-line message", i+1)
			}
		case "command":
			a.Text = strings.TrimSpace(a.Text)
			if a.Text == "" || len(a.Text) > 1024 || strings.ContainsAny(a.Text, "\n\r") {
				return fmt.Errorf("action %d: command needs a single-line text", i+1)
			}
		case "wait":
			if a.Seconds < 1 || a.Seconds > 3600 {
				return fmt.Errorf("action %d: wait must be 1–3600 seconds", i+1)
			}
			total += time.Duration(a.Seconds) * time.Second
		case "stop", "start", "restart", "kill", "backup":
		default:
			return fmt.Errorf("action %d: unknown type %q", i+1, a.Type)
		}
	}
	if total > MaxChainDuration {
		return fmt.Errorf("waits add up to more than %s", MaxChainDuration)
	}
	return nil
}

// NextRuns returns the next n fire times of expr in loc.
func NextRuns(expr string, loc *time.Location, n int) ([]time.Time, error) {
	sched, err := Parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCron, err)
	}
	out := make([]time.Time, 0, n)
	t := time.Now().In(loc)
	for i := 0; i < n; i++ {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out, nil
}

// Store persists schedules and runs.
type Store struct {
	db *sql.DB
}

// NewStore wraps a migrated database.
func NewStore(sqlDB *sql.DB) *Store { return &Store{db: sqlDB} }

const cols = `id, server_id, name, cron_expr, enabled, actions, only_if_running, skip_if_players, created_by, created_at, updated_at, COALESCE(last_run_at,''), COALESCE(next_run_at,'')`

func scan(row interface{ Scan(...any) error }) (*Schedule, error) {
	var s Schedule
	var enabled, oir, sip int
	var actions string
	var createdBy sql.NullInt64
	if err := row.Scan(&s.ID, &s.ServerID, &s.Name, &s.CronExpr, &enabled, &actions, &oir, &sip, &createdBy, &s.CreatedAt, &s.UpdatedAt, &s.LastRunAt, &s.NextRunAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.Enabled, s.OnlyIfRunning, s.SkipIfPlayers = enabled == 1, oir == 1, sip == 1
	if createdBy.Valid {
		s.CreatedBy = &createdBy.Int64
	}
	_ = json.Unmarshal([]byte(actions), &s.Actions)
	if s.Actions == nil {
		s.Actions = []Action{}
	}
	return &s, nil
}

func (st *Store) Create(ctx context.Context, s *Schedule) (*Schedule, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	actions, _ := json.Marshal(s.Actions)
	now := db.Now()
	res, err := st.db.ExecContext(ctx, `INSERT INTO schedules (server_id, name, cron_expr, enabled, actions, only_if_running, skip_if_players, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.ServerID, s.Name, s.CronExpr, b2i(s.Enabled), string(actions), b2i(s.OnlyIfRunning), b2i(s.SkipIfPlayers), s.CreatedBy, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert schedule: %w", err)
	}
	id, _ := res.LastInsertId()
	return st.Get(ctx, id)
}

func (st *Store) Update(ctx context.Context, s *Schedule) error {
	if err := s.Validate(); err != nil {
		return err
	}
	actions, _ := json.Marshal(s.Actions)
	res, err := st.db.ExecContext(ctx, `UPDATE schedules SET name = ?, cron_expr = ?, enabled = ?, actions = ?, only_if_running = ?, skip_if_players = ?, updated_at = ? WHERE id = ?`,
		s.Name, s.CronExpr, b2i(s.Enabled), string(actions), b2i(s.OnlyIfRunning), b2i(s.SkipIfPlayers), db.Now(), s.ID)
	if err != nil {
		return fmt.Errorf("update schedule %d: %w", s.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (st *Store) Get(ctx context.Context, id int64) (*Schedule, error) {
	s, err := scan(st.db.QueryRowContext(ctx, `SELECT `+cols+` FROM schedules WHERE id = ?`, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("get schedule %d: %w", id, err)
	}
	return s, err
}

// List returns schedules of one server, or all when serverID is empty.
func (st *Store) List(ctx context.Context, serverID string) ([]*Schedule, error) {
	q := `SELECT ` + cols + ` FROM schedules`
	var args []any
	if serverID != "" {
		q += ` WHERE server_id = ?`
		args = append(args, serverID)
	}
	rows, err := st.db.QueryContext(ctx, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()
	out := []*Schedule{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (st *Store) Delete(ctx context.Context, id int64) error {
	res, err := st.db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete schedule %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (st *Store) setTimes(ctx context.Context, id int64, last, next string) {
	if last != "" {
		_, _ = st.db.ExecContext(ctx, `UPDATE schedules SET last_run_at = ? WHERE id = ?`, last, id)
	}
	_, _ = st.db.ExecContext(ctx, `UPDATE schedules SET next_run_at = ? WHERE id = ?`, next, id)
}

func (st *Store) startRun(ctx context.Context, scheduleID int64, trigger string) (int64, error) {
	res, err := st.db.ExecContext(ctx, `INSERT INTO schedule_runs (schedule_id, started_at, trigger) VALUES (?, ?, ?)`, scheduleID, db.Now(), trigger)
	if err != nil {
		return 0, fmt.Errorf("insert run: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (st *Store) finishRun(ctx context.Context, runID int64, result, log string) {
	_, _ = st.db.ExecContext(ctx, `UPDATE schedule_runs SET finished_at = ?, result = ?, log = ? WHERE id = ?`, db.Now(), result, log, runID)
}

func (st *Store) updateRunLog(ctx context.Context, runID int64, log string) {
	_, _ = st.db.ExecContext(ctx, `UPDATE schedule_runs SET log = ? WHERE id = ?`, log, runID)
}

// Runs returns the newest runs of a schedule.
func (st *Store) Runs(ctx context.Context, scheduleID int64, limit int) ([]Run, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT id, schedule_id, started_at, COALESCE(finished_at,''), result, trigger, log FROM schedule_runs WHERE schedule_id = ? ORDER BY id DESC LIMIT ?`, scheduleID, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.ScheduleID, &r.StartedAt, &r.FinishedAt, &r.Result, &r.Trigger, &r.Log); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// pruneRuns keeps the newest keep runs of a schedule.
func (st *Store) pruneRuns(ctx context.Context, scheduleID int64, keep int) {
	_, _ = st.db.ExecContext(ctx, `DELETE FROM schedule_runs WHERE schedule_id = ? AND id NOT IN (SELECT id FROM schedule_runs WHERE schedule_id = ? ORDER BY id DESC LIMIT ?)`, scheduleID, scheduleID, keep)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

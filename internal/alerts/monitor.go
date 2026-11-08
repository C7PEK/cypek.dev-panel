package alerts

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cypek019/cypek.dev---panel/internal/shim"
	"github.com/cypek019/cypek.dev---panel/internal/supervisor"
)

// Alert kinds.
const (
	KindDown      = "down"
	KindRecovered = "recovered"
	KindRestarted = "restarted"
	KindError     = "error"
	KindTest      = "test"
)

const (
	// stableFor is how long a server must run before an incident is over.
	stableFor = 30 * time.Second
	// crashLoop crashes in one incident (no stable run in between) count as
	// down even if the server happens to be starting again at the check.
	crashLoop = 2
	// catchUpWindow: a crash that happened while the panel was down is
	// still reported if it is this recent.
	catchUpWindow = 10 * time.Minute
	errorCooldown = 10 * time.Minute
	historySize   = 50
)

// ServerInfo is what the monitor needs to know about a server.
type ServerInfo struct {
	ID   string
	Name string
	Dir  string
	// CleanExitIsDown: a process ending with code 0 means the server is
	// down (bots). Game servers end with 0 after a console "stop", which is
	// intentional.
	CleanExitIsDown bool
}

// HistoryItem records one delivery attempt.
type HistoryItem struct {
	At         time.Time `json:"at"`
	ServerID   string    `json:"serverId"`
	ServerName string    `json:"serverName"`
	Kind       string    `json:"kind"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
}

// Monitor turns status samples and shim events into alerts.
type Monitor struct {
	Store   *Store
	Sender  Sender
	Servers func(ctx context.Context) ([]ServerInfo, error)
	Log     *slog.Logger
	Now     func() time.Time
	// ReadEvents defaults to shim.ReadEvents.
	ReadEvents func(dir string, limit int) ([]shim.Event, error)

	mu       sync.Mutex
	trackers map[string]*tracker
	history  []HistoryItem
	queue    chan job
	once     sync.Once
}

type job struct {
	webhook string
	msg     Message
	item    HistoryItem
	done    chan error
}

type tracker struct {
	cursor    time.Time
	inc       *incident
	lastError time.Time
}

type incident struct {
	start        time.Time
	last         time.Time
	kind         string // crash | exit
	exitCode     *int
	crashes      int
	downSent     bool
	runningSince time.Time
}

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Monitor) start() {
	m.once.Do(func() {
		m.trackers = map[string]*tracker{}
		m.queue = make(chan job, 64)
		if m.ReadEvents == nil {
			m.ReadEvents = shim.ReadEvents
		}
		go m.worker()
	})
}

// worker delivers messages in order so a "down" never arrives after its
// "recovered".
func (m *Monitor) worker() {
	for j := range m.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := m.Sender.Send(ctx, j.webhook, j.msg)
		cancel()
		j.item.OK = err == nil
		if err != nil {
			j.item.Error = err.Error()
			if m.Log != nil {
				m.Log.Warn("discord alert failed", "server", j.item.ServerID, "kind", j.item.Kind, "err", err)
			}
		}
		m.mu.Lock()
		m.history = append([]HistoryItem{j.item}, m.history...)
		if len(m.history) > historySize {
			m.history = m.history[:historySize]
		}
		m.mu.Unlock()
		if j.done != nil {
			j.done <- err
		}
	}
}

// History returns recent deliveries, newest first.
func (m *Monitor) History() []HistoryItem {
	m.start()
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]HistoryItem{}, m.history...)
}

// Tick processes one round of status samples (called by the metrics
// collector every few seconds).
func (m *Monitor) Tick(ctx context.Context, states map[string]supervisor.State) {
	m.start()
	list, err := m.Servers(ctx)
	if err != nil {
		return
	}
	settings, err := m.Store.Settings(ctx)
	if err != nil {
		return
	}
	now := m.now()
	seen := map[string]bool{}
	for _, srv := range list {
		seen[srv.ID] = true
		state, ok := states[srv.ID]
		if !ok {
			continue
		}
		m.tickServer(ctx, srv, state, settings, now)
	}
	m.mu.Lock()
	for id := range m.trackers {
		if !seen[id] {
			delete(m.trackers, id)
		}
	}
	m.mu.Unlock()
}

func eventTime(e shim.Event) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, e.At)
	return t
}

func (m *Monitor) tickServer(ctx context.Context, srv ServerInfo, state supervisor.State, settings Settings, now time.Time) {
	evs, _ := m.ReadEvents(srv.Dir, 30)
	sort.SliceStable(evs, func(i, j int) bool { return eventTime(evs[i]).Before(eventTime(evs[j])) })

	m.mu.Lock()
	tr, known := m.trackers[srv.ID]
	if !known {
		tr = &tracker{}
		m.trackers[srv.ID] = tr
	}
	m.mu.Unlock()

	running := state == supervisor.StateRunning
	alive := running || state == supervisor.StateStarting

	if !known {
		// Start after the existing history; but a fresh crash that left the
		// server down while the panel was restarting still counts.
		if n := len(evs); n > 0 {
			last := evs[n-1]
			tr.cursor = eventTime(last)
			if opensIncident(last, srv) && !alive && now.Sub(tr.cursor) < catchUpWindow {
				tr.inc = &incident{start: tr.cursor, last: tr.cursor, kind: last.Kind, exitCode: last.ExitCode, crashes: 1}
			}
		}
		if tr.inc == nil {
			return
		}
		evs = nil
	}

	var errorEvents []shim.Event
	for _, e := range evs {
		at := eventTime(e)
		if !at.After(tr.cursor) {
			continue
		}
		tr.cursor = at
		switch {
		case opensIncident(e, srv):
			if tr.inc == nil {
				tr.inc = &incident{start: at}
			}
			tr.inc.last, tr.inc.kind, tr.inc.exitCode = at, e.Kind, e.ExitCode
			tr.inc.crashes++
			tr.inc.runningSince = time.Time{}
		case e.Kind == "stop", e.Kind == "exit":
			// Stopped from the panel or the console: intentional.
			tr.inc = nil
		case e.Kind == "error":
			errorEvents = append(errorEvents, e)
		}
	}

	ss, err := m.Store.Server(ctx, srv.ID)
	if err != nil || !ss.Enabled {
		tr.inc = nil
		return
	}
	webhook := firstNonEmpty(ss.Webhook, settings.Webhook)
	mention := firstNonEmpty(ss.Mention, settings.Mention)
	if webhook == "" {
		tr.inc = nil
		return
	}

	if settings.NotifyErrors && len(errorEvents) > 0 && now.Sub(tr.lastError) > errorCooldown {
		tr.lastError = now
		m.enqueue(webhook, m.errorMessage(srv, settings, errorEvents[len(errorEvents)-1]), srv, KindError)
	}

	inc := tr.inc
	if inc == nil {
		return
	}
	if running {
		if inc.runningSince.IsZero() {
			inc.runningSince = now
		}
	} else {
		inc.runningSince = time.Time{}
	}
	grace := time.Duration(settings.GraceSec) * time.Second
	stable := running && now.Sub(inc.runningSince) >= stableFor
	switch {
	case !inc.downSent && stable:
		if settings.NotifyAutoRestart {
			m.enqueue(webhook, m.restartedMessage(srv, settings, inc), srv, KindRestarted)
		}
		tr.inc = nil
	case !inc.downSent && now.Sub(inc.start) >= grace && (!alive || inc.crashes >= crashLoop):
		inc.downSent = true
		m.enqueue(webhook, m.downMessage(srv, settings, inc, mention), srv, KindDown)
	case inc.downSent && stable:
		if settings.NotifyRecovered {
			m.enqueue(webhook, m.recoveredMessage(srv, settings, inc, now), srv, KindRecovered)
		}
		tr.inc = nil
	}
}

func (m *Monitor) enqueue(webhook string, msg Message, srv ServerInfo, kind string) {
	select {
	case m.queue <- job{webhook: webhook, msg: msg, item: HistoryItem{At: m.now(), ServerID: srv.ID, ServerName: srv.Name, Kind: kind}}:
	default:
		if m.Log != nil {
			m.Log.Warn("alert queue full, dropping", "server", srv.ID, "kind", kind)
		}
	}
}

// SendTest posts a test message and waits for the result.
func (m *Monitor) SendTest(ctx context.Context, webhook, mention string, settings Settings, srv *ServerInfo) error {
	m.start()
	men, _ := ParseMention(mention)
	t := texts(settings.Language)
	msg := Message{Content: t.testContent, Embeds: []Embed{{Title: t.testTitle, Description: t.testBody, Color: colorBlue, Timestamp: m.now().UTC().Format(time.RFC3339)}}}
	item := HistoryItem{At: m.now(), Kind: KindTest}
	if srv != nil {
		msg.Embeds[0].Title = t.testTitle + ": " + srv.Name
		item.ServerID, item.ServerName = srv.ID, srv.Name
	}
	msg.withMention(men)
	done := make(chan error, 1)
	select {
	case m.queue <- job{webhook: webhook, msg: msg, item: item, done: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LastLogLines returns the tail of the newest console log, ANSI stripped.
func LastLogLines(dir string, n int) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "logs", "console-*.log"))
	if len(matches) == 0 {
		return nil
	}
	sort.Strings(matches)
	f, err := os.Open(matches[len(matches)-1])
	if err != nil {
		return nil
	}
	defer f.Close()
	const tail = 32 << 10
	if info, err := f.Stat(); err == nil && info.Size() > tail {
		_, _ = f.Seek(info.Size()-tail, 0)
	}
	buf := make([]byte, tail)
	k, _ := f.Read(buf)
	var lines []string
	for _, l := range strings.Split(string(buf[:k]), "\n") {
		l = strings.TrimRight(shim.StripANSI(l), " \t\r")
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func opensIncident(e shim.Event, srv ServerInfo) bool {
	return e.Kind == "crash" || e.Kind == "exit" && srv.CleanExitIsDown
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

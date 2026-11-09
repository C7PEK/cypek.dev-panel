package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/C7PEK/cypek.dev-panel/internal/metrics"
	"github.com/C7PEK/cypek.dev-panel/internal/query"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

// storeSource adapts the server store + supervisor to metrics.Source.
type storeSource struct {
	servers *servers.Store
	sup     supervisor.Supervisor
	api     *Server
}

func (s storeSource) Players(ctx context.Context, id string) int {
	if s.api == nil {
		return -1
	}
	srv, err := s.servers.Get(ctx, id)
	if err != nil {
		return -1
	}
	if p := s.api.playersOf(ctx, srv); p != nil {
		return p.Online
	}
	return -1
}

func (s storeSource) ServerIDs(ctx context.Context) ([]string, error) {
	list, err := s.servers.List(ctx, nil)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, srv := range list {
		if srv.InstallState == servers.InstallReady {
			ids = append(ids, srv.ID)
		}
	}
	return ids, nil
}

func (s storeSource) Status(ctx context.Context, id string) (supervisor.Status, error) {
	st, err := s.sup.Status(ctx, id)
	if err != nil {
		return st, err
	}
	if st.Active() && st.MemoryBytes == 0 {
		if stats, err := s.sup.Stats(ctx, id); err == nil {
			st.MemoryBytes, st.CPUNanos = stats.MemoryBytes, stats.CPUNanos
		}
	}
	return st, nil
}

// eventServer is the per-server part of a tick.
type eventServer struct {
	ID      string            `json:"id"`
	Sample  metrics.Sample    `json:"sample"`
	Players *query.Players    `json:"players,omitempty"`
	History []metrics.Sample  `json:"history,omitempty"` // only in the first message
	Status  supervisor.Status `json:"status"`
}

type eventMsg struct {
	Type    string        `json:"type"` // snapshot | tick
	Host    metrics.Host  `json:"host"`
	Servers []eventServer `json:"servers"`
}

func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.metrics.Host())
}

// handleServerStats returns the sample history for ?minutes=N (default 15,
// max 60) plus a summary of the window.
func (s *Server) handleServerStats(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
	if minutes <= 0 {
		minutes = 15
	}
	if minutes > 60 {
		minutes = 60
	}
	hist := s.metrics.HistorySince(srv.ID, time.Now().Add(-time.Duration(minutes)*time.Minute))
	if hist == nil {
		hist = []metrics.Sample{}
	}
	var sum statsSummary
	for _, h := range hist {
		if h.State != supervisor.StateRunning && h.State != supervisor.StateStarting {
			continue
		}
		sum.Samples++
		sum.AvgCPU += h.CPUPercent
		sum.AvgMemory += float64(h.MemoryBytes)
		if h.CPUPercent > sum.PeakCPU {
			sum.PeakCPU = h.CPUPercent
		}
		if h.MemoryBytes > sum.PeakMemory {
			sum.PeakMemory = h.MemoryBytes
		}
		if h.Players > sum.PeakPlayers {
			sum.PeakPlayers = h.Players
		}
	}
	if sum.Samples > 0 {
		sum.AvgCPU /= float64(sum.Samples)
		sum.AvgMemory /= float64(sum.Samples)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"history":         hist,
		"summary":         sum,
		"intervalSeconds": int(s.metrics.Interval().Seconds()),
		"memoryLimit":     uint64(srv.Limits.MemoryMB) << 20,
		"cpuCount":        s.metrics.Host().CPUCount,
	})
}

type statsSummary struct {
	Samples     int     `json:"samples"`
	AvgCPU      float64 `json:"avgCpuPercent"`
	PeakCPU     float64 `json:"peakCpuPercent"`
	AvgMemory   float64 `json:"avgMemoryBytes"`
	PeakMemory  uint64  `json:"peakMemoryBytes"`
	PeakPlayers int     `json:"peakPlayers"`
}

// serverEvent is a merged history entry: shim lifecycle/error events plus
// panel actions (who pressed stop, which schedule restarted it).
type serverEvent struct {
	shim.Event
	Source   string `json:"source"` // server | panel
	Username string `json:"username,omitempty"`
}

// handleServerEvents returns the newest events (?limit, default 100, max 500)
// and 24h counters for the summary chips.
func (s *Server) handleServerEvents(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	evs, err := shim.ReadEvents(s.servers.Dir(srv.ID), limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]serverEvent, 0, len(evs)+20)
	for _, e := range evs {
		out = append(out, serverEvent{Event: e, Source: "server"})
	}
	actions, err := s.audit.ForTarget(r.Context(), srv.ID, limit)
	if err == nil {
		for _, a := range actions {
			kind := ""
			switch a.Action {
			case "server.start", "server.stop", "server.restart", "server.kill", "server.reinstall", "backup.create", "backup.restore", "schedule.run", "server.update":
				kind = "action"
			default:
				continue
			}
			out = append(out, serverEvent{Event: shim.Event{At: a.CreatedAt, Kind: kind, Message: a.Action}, Source: "panel", Username: a.Username})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At > out[j].At })
	if len(out) > limit {
		out = out[:limit]
	}
	since := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)
	counts := map[string]int{"crash": 0, "error": 0, "start": 0}
	for _, e := range evs {
		if e.At >= since {
			if _, ok := counts[e.Kind]; ok {
				counts[e.Kind]++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "last24h": counts})
}

// handleEvents streams live status, resource samples and player counts for
// every server the user may see: a "snapshot" with history on connect, then
// a "tick" per collector interval. One socket per browser tab replaces the
// dashboard's polling.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		return
	}
	ws.SetReadLimit(4096)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// Drain client frames (pings) and notice disconnects.
	go func() {
		defer cancel()
		for {
			if _, _, err := ws.Read(ctx); err != nil {
				return
			}
		}
	}()
	send := func(m eventMsg) error {
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		b, _ := json.Marshal(m)
		return ws.Write(wctx, websocket.MessageText, b)
	}
	build := func(typ string, samples map[string]metrics.Sample, host metrics.Host) (eventMsg, error) {
		// Fresh user and session every tick: a demoted, disabled or
		// logged-out account stops receiving server data.
		fresh, err := s.store.GetUserByID(ctx, user.ID)
		if err != nil || fresh.Disabled || !s.sessionAlive(ctx, r) {
			return eventMsg{}, errors.New("session ended")
		}
		ids, all, err := s.store.VisibleServerIDs(ctx, fresh)
		if err != nil {
			return eventMsg{}, err
		}
		if !all && ids == nil {
			ids = []string{}
		}
		list, err := s.servers.List(ctx, ids)
		if err != nil {
			return eventMsg{}, err
		}
		msg := eventMsg{Type: typ, Host: host, Servers: make([]eventServer, 0, len(list))}
		for _, srv := range list {
			es := eventServer{ID: srv.ID}
			if sm, ok := samples[srv.ID]; ok {
				es.Sample = sm
				es.Status = supervisor.Status{State: sm.State, MemoryBytes: sm.MemoryBytes}
			} else {
				es.Status = s.statusOf(ctx, srv.ID)
				es.Sample = metrics.Sample{At: time.Now(), State: es.Status.State, MemoryBytes: es.Status.MemoryBytes}
			}
			if es.Status.State == supervisor.StateRunning {
				es.Players = s.playersOf(ctx, srv)
			}
			if typ == "snapshot" {
				// Dashboard sparklines need the last 3 minutes only.
				es.History = s.metrics.HistorySince(srv.ID, time.Now().Add(-3*time.Minute))
			}
			msg.Servers = append(msg.Servers, es)
		}
		return msg, nil
	}

	snapshot, err := build("snapshot", nil, s.metrics.Host())
	if err != nil || send(snapshot) != nil {
		_ = ws.Close(websocket.StatusInternalError, "snapshot failed")
		return
	}
	ticks := make(chan struct {
		host    metrics.Host
		samples map[string]metrics.Sample
	}, 1)
	unsub := s.metrics.Subscribe(func(h metrics.Host, samples map[string]metrics.Sample) {
		select { // drop a tick rather than block the collector on a slow client
		case ticks <- struct {
			host    metrics.Host
			samples map[string]metrics.Sample
		}{h, samples}:
		default:
		}
	})
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			_ = ws.Close(websocket.StatusNormalClosure, "bye")
			return
		case t := <-ticks:
			msg, err := build("tick", t.samples, t.host)
			if err != nil {
				_ = ws.Close(websocket.StatusPolicyViolation, "session ended")
				return
			}
			if err := send(msg); err != nil {
				return
			}
		}
	}
}

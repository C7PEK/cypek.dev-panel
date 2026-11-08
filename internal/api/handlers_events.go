package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/cypek019/cypek.dev---panel/internal/metrics"
	"github.com/cypek019/cypek.dev---panel/internal/query"
	"github.com/cypek019/cypek.dev---panel/internal/servers"
	"github.com/cypek019/cypek.dev---panel/internal/supervisor"
)

// storeSource adapts the server store + supervisor to metrics.Source.
type storeSource struct {
	servers *servers.Store
	sup     supervisor.Supervisor
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

func (s *Server) handleServerStats(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	hist := s.metrics.History(srv.ID)
	if hist == nil {
		hist = []metrics.Sample{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": hist})
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
		ids, all, err := s.store.VisibleServerIDs(ctx, user)
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
				es.History = s.metrics.History(srv.ID)
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
				continue
			}
			if err := send(msg); err != nil {
				return
			}
		}
	}
}

package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

// Sample is one per-server reading.
type Sample struct {
	At          time.Time        `json:"at"`
	State       supervisor.State `json:"state"`
	MemoryBytes uint64           `json:"memoryBytes"`
	// CPUPercent is usage since the previous sample; 100 = one core.
	CPUPercent float64 `json:"cpuPercent"`
	// Players online; -1 when unknown (not running or no query support).
	Players int `json:"players"`
}

// Source provides the servers to watch and their live status.
type Source interface {
	ServerIDs(ctx context.Context) ([]string, error)
	Status(ctx context.Context, id string) (supervisor.Status, error)
	// Players returns the online player count, -1 when unknown.
	Players(ctx context.Context, id string) int
}

// Listener receives every tick (host + all server samples).
type Listener func(host Host, samples map[string]Sample)

// Collector samples everything on an interval and keeps a short history.
type Collector struct {
	src      Source
	interval time.Duration
	keep     int
	diskPath string
	log      *slog.Logger

	mu        sync.RWMutex
	host      Host
	history   map[string][]Sample
	lastCPU   map[string]cpuPoint
	listeners map[int]Listener
	nextID    int
}

type cpuPoint struct {
	nanos uint64
	at    time.Time
}

// New creates a collector sampling every interval and keeping `keep` samples per server.
func New(src Source, interval time.Duration, keep int, diskPath string, log *slog.Logger) *Collector {
	return &Collector{src: src, interval: interval, keep: keep, diskPath: diskPath, log: log,
		history: map[string][]Sample{}, lastCPU: map[string]cpuPoint{}, listeners: map[int]Listener{}}
}

// Run samples until ctx ends.
func (c *Collector) Run(ctx context.Context) {
	c.tick(ctx)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.tick(ctx)
		}
	}
}

func (c *Collector) tick(ctx context.Context) {
	tctx, cancel := context.WithTimeout(ctx, c.interval)
	defer cancel()
	host := SampleHost(tctx, c.diskPath)
	ids, err := c.src.ServerIDs(tctx)
	if err != nil {
		c.log.Debug("metrics: list servers", "err", err)
	}
	now := time.Now()
	samples := make(map[string]Sample, len(ids))
	for _, id := range ids {
		st, err := c.src.Status(tctx, id)
		if err != nil {
			continue
		}
		s := Sample{At: now, State: st.State, MemoryBytes: st.MemoryBytes, Players: -1}
		if st.State == supervisor.StateRunning {
			s.Players = c.src.Players(tctx, id)
		}
		c.mu.Lock()
		if prev, ok := c.lastCPU[id]; ok && st.CPUNanos >= prev.nanos && now.After(prev.at) && st.Active() {
			s.CPUPercent = float64(st.CPUNanos-prev.nanos) / float64(now.Sub(prev.at).Nanoseconds()) * 100
		}
		if st.Active() {
			c.lastCPU[id] = cpuPoint{nanos: st.CPUNanos, at: now}
		} else {
			delete(c.lastCPU, id)
		}
		h := append(c.history[id], s)
		if len(h) > c.keep {
			h = h[len(h)-c.keep:]
		}
		c.history[id] = h
		c.mu.Unlock()
		samples[id] = s
	}
	c.mu.Lock()
	c.host = host
	// Forget servers that disappeared.
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for id := range c.history {
		if !seen[id] {
			delete(c.history, id)
			delete(c.lastCPU, id)
		}
	}
	ls := make([]Listener, 0, len(c.listeners))
	for _, l := range c.listeners {
		ls = append(ls, l)
	}
	c.mu.Unlock()
	for _, l := range ls {
		l(host, samples)
	}
}

// Host returns the latest host snapshot.
func (c *Collector) Host() Host {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.host
}

// History returns the recent samples of a server (oldest first).
func (c *Collector) History(id string) []Sample {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Sample(nil), c.history[id]...)
}

// HistorySince returns samples newer than since (oldest first).
func (c *Collector) HistorySince(id string, since time.Time) []Sample {
	c.mu.RLock()
	defer c.mu.RUnlock()
	h := c.history[id]
	i := 0
	for i < len(h) && h[i].At.Before(since) {
		i++
	}
	return append([]Sample(nil), h[i:]...)
}

// Interval is the sampling period.
func (c *Collector) Interval() time.Duration { return c.interval }

// Subscribe registers a listener; the returned func removes it.
func (c *Collector) Subscribe(l Listener) func() {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.listeners[id] = l
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.listeners, id)
		c.mu.Unlock()
	}
}

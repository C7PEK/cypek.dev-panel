// Package supervisor starts, stops and inspects game servers. The systemd
// implementation drives homepanel-server@<id>.service over D-Bus; the local
// one (dev mode) runs the shim as a detached child process. Both talk to the
// shim's console socket for state and console I/O.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/cypek019/cypek.dev---panel/internal/shim"
)

// State is the panel-level server state.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateCrashed  State = "crashed" // exited without being asked to
	StateUnknown  State = "unknown"
)

// Status is a snapshot of one server.
type Status struct {
	State    State     `json:"state"`
	PID      int       `json:"pid,omitempty"`
	Since    time.Time `json:"since,omitzero"`
	ExitCode *int      `json:"exitCode,omitempty"`
	// Stats are filled when the server is running and the backend can read them.
	MemoryBytes uint64 `json:"memoryBytes,omitempty"`
	CPUNanos    uint64 `json:"cpuNanos,omitempty"`
}

// Active reports whether the process is (or is about to be) alive.
func (s Status) Active() bool {
	return s.State == StateStarting || s.State == StateRunning || s.State == StateStopping
}

// Limits are per-server resource caps; zero means unlimited.
type Limits struct {
	MemoryMB   int `json:"memoryMb"`
	CPUPercent int `json:"cpuPercent"` // 100 = one full core
}

// Stats are live resource numbers.
type Stats struct {
	MemoryBytes uint64
	CPUNanos    uint64
	At          time.Time
}

// Console is an attached shim connection.
type Console = *shim.Client

var (
	ErrAlreadyRunning = errors.New("server is already running")
	ErrNotRunning     = errors.New("server is not running")
	ErrInvalidID      = errors.New("invalid server id")
)

// Supervisor is implemented by systemd (production) and local (dev).
type Supervisor interface {
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
	Kill(ctx context.Context, id string) error
	Status(ctx context.Context, id string) (Status, error)
	SetLimits(ctx context.Context, id string, l Limits) error
	SetAutostart(ctx context.Context, id string, on bool) error
	Attach(ctx context.Context, id string) (Console, error)
	Stats(ctx context.Context, id string) (Stats, error)
	// Remove releases everything the backend holds for a deleted server
	// (unit enablement, failed state). It does not touch server files.
	Remove(ctx context.Context, id string) error
	// Name identifies the backend ("systemd" or "local") for the UI/logs.
	Name() string
}

// idRe is the only shape of id accepted anywhere near a path or unit name.
var idRe = regexp.MustCompile(`^[a-z0-9]{4,32}$`)

// ValidID reports whether id is safe to use in paths and unit names.
func ValidID(id string) bool { return idRe.MatchString(id) }

// Layout maps server ids to directories and sockets.
type Layout struct {
	ServersDir string // e.g. /var/lib/homepanel/servers
	RunDir     string // e.g. /run (systemd: /run/homepanel-<id>) or <data>/run (local)
	// FlatRun selects "<RunDir>/homepanel-<id>/console.sock" (systemd
	// RuntimeDirectory) over "<RunDir>/<id>/console.sock".
	FlatRun bool
}

// ServerDir returns the working directory of a server.
func (l Layout) ServerDir(id string) string { return filepath.Join(l.ServersDir, id) }

// SocketPath returns the console socket path of a server.
func (l Layout) SocketPath(id string) string {
	if l.FlatRun {
		return filepath.Join(l.RunDir, "homepanel-"+id, "console.sock")
	}
	return filepath.Join(l.RunDir, id, "console.sock")
}

func checkID(id string) error {
	if !ValidID(id) {
		return fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	return nil
}

// statusFromShim converts a shim hello frame into a Status.
func statusFromShim(h shim.Frame) Status {
	st := Status{PID: h.PID}
	if t, err := time.Parse(time.RFC3339Nano, h.StartedAt); err == nil {
		st.Since = t
	}
	switch h.State {
	case shim.StateStarting:
		st.State = StateStarting
	case shim.StateRunning:
		st.State = StateRunning
	case shim.StateStopping:
		st.State = StateStopping
	default:
		st.State = StateUnknown
	}
	return st
}

// statusFromExit derives stopped/crashed from the shim's last exit record.
func statusFromExit(serverDir string) Status {
	info, err := shim.ReadExitInfo(serverDir)
	if err != nil || info == nil {
		return Status{State: StateStopped}
	}
	code := info.ExitCode
	st := Status{State: StateStopped, ExitCode: &code}
	if !info.Requested && code != 0 {
		st.State = StateCrashed
	}
	if t, err := time.Parse(time.RFC3339Nano, info.ExitedAt); err == nil {
		st.Since = t
	}
	return st
}

// waitUntil polls fn until it returns true, ctx ends or timeout passes.
func waitUntil(ctx context.Context, timeout time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if fn() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

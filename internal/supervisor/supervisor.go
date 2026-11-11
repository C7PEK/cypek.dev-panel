package supervisor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/shim"
)

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateCrashed  State = "crashed"
	StateUnknown  State = "unknown"
)

type Status struct {
	State    State     `json:"state"`
	PID      int       `json:"pid,omitempty"`
	Since    time.Time `json:"since,omitzero"`
	ExitCode *int      `json:"exitCode,omitempty"`

	Message string `json:"message,omitempty"`

	MemoryBytes uint64 `json:"memoryBytes,omitempty"`
	CPUNanos    uint64 `json:"cpuNanos,omitempty"`
}

func (s Status) Active() bool {
	return s.State == StateStarting || s.State == StateRunning || s.State == StateStopping
}

type Limits struct {
	MemoryMB   int `json:"memoryMb"`
	CPUPercent int `json:"cpuPercent"`
}

type Stats struct {
	MemoryBytes uint64
	CPUNanos    uint64
	At          time.Time
}

type Console = *shim.Client

var (
	ErrAlreadyRunning = errors.New("server is already running")
	ErrNotRunning     = errors.New("server is not running")
	ErrInvalidID      = errors.New("invalid server id")
)

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

	Remove(ctx context.Context, id string) error

	Name() string
}

var idRe = regexp.MustCompile(`^[a-z0-9]{4,32}$`)

func ValidID(id string) bool { return idRe.MatchString(id) }

type Layout struct {
	ServersDir string
	RunDir     string

	FlatRun bool
}

func (l Layout) ServerDir(id string) string { return filepath.Join(l.ServersDir, id) }

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

func statusFromExit(serverDir string) Status {
	info, err := shim.ReadExitInfo(serverDir)
	if err != nil || info == nil {
		return Status{State: StateStopped}
	}
	code := info.ExitCode
	st := Status{State: StateStopped, ExitCode: &code, Message: info.Error}
	if !info.Requested && code != 0 {
		st.State = StateCrashed
	}
	if t, err := time.Parse(time.RFC3339Nano, info.ExitedAt); err == nil {
		st.Since = t
	}
	return st
}

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

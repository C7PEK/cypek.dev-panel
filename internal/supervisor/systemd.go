package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"syscall"
	"time"

	sd "github.com/coreos/go-systemd/v22/dbus"
	"github.com/godbus/dbus/v5"

	"github.com/cypek019/cypek.dev---panel/internal/shim"
)

// Systemd drives homepanel-server@<id>.service instances over the system
// D-Bus. The panel runs as the homepanel user; a polkit rule allows it to
// manage exactly these units (see packaging/polkit).
type Systemd struct {
	layout Layout
	conn   *sd.Conn
	log    *slog.Logger
}

const unitTemplate = "homepanel-server@%s.service"

// NewSystemd connects to the system bus.
func NewSystemd(ctx context.Context, layout Layout, log *slog.Logger) (*Systemd, error) {
	conn, err := sd.NewSystemConnectionContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to systemd over D-Bus: %w", err)
	}
	return &Systemd{layout: layout, conn: conn, log: log}, nil
}

func (s *Systemd) Name() string { return "systemd" }

// Close releases the bus connection.
func (s *Systemd) Close() { s.conn.Close() }

func unitName(id string) string { return fmt.Sprintf(unitTemplate, id) }

// job runs a unit job and waits for its result.
func (s *Systemd) job(ctx context.Context, id, what string, start func(ch chan<- string) (int, error)) error {
	if err := checkID(id); err != nil {
		return err
	}
	ch := make(chan string, 1)
	if _, err := start(ch); err != nil {
		return fmt.Errorf("%s %s: %w", what, unitName(id), err)
	}
	select {
	case res := <-ch:
		if res != "done" && res != "skipped" {
			return fmt.Errorf("%s %s: job result %q", what, unitName(id), res)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Systemd) Start(ctx context.Context, id string) error {
	st, err := s.Status(ctx, id)
	if err == nil && st.Active() {
		return ErrAlreadyRunning
	}
	if _, err := shim.LoadRunConfig(s.layout.ServerDir(id)); err != nil {
		return err
	}
	// A previous crash leaves the unit "failed"; clear it so the state is readable.
	_ = s.conn.ResetFailedUnitContext(ctx, unitName(id))
	if err := s.job(ctx, id, "start", func(ch chan<- string) (int, error) {
		return s.conn.StartUnitContext(ctx, unitName(id), "replace", ch)
	}); err != nil {
		return err
	}
	// The unit is active once the shim runs; wait for the socket so callers can attach.
	sock := s.layout.SocketPath(id)
	if !waitUntil(ctx, 10*time.Second, func() bool {
		_, err := shim.Probe(ctx, sock)
		return err == nil
	}) {
		return fmt.Errorf("server %s started but its console socket did not appear", id)
	}
	return nil
}

func (s *Systemd) Stop(ctx context.Context, id string) error {
	st, err := s.Status(ctx, id)
	if err == nil && !st.Active() {
		return ErrNotRunning
	}
	return s.job(ctx, id, "stop", func(ch chan<- string) (int, error) {
		return s.conn.StopUnitContext(ctx, unitName(id), "replace", ch)
	})
}

func (s *Systemd) Restart(ctx context.Context, id string) error {
	if _, err := shim.LoadRunConfig(s.layout.ServerDir(id)); err != nil {
		return err
	}
	_ = s.conn.ResetFailedUnitContext(ctx, unitName(id))
	return s.job(ctx, id, "restart", func(ch chan<- string) (int, error) {
		return s.conn.RestartUnitContext(ctx, unitName(id), "replace", ch)
	})
}

// Kill asks the shim to SIGKILL the game. The shim then exits 0 (the stop was
// requested), so Restart=on-failure does not bring the server back.
func (s *Systemd) Kill(ctx context.Context, id string) error {
	if err := checkID(id); err != nil {
		return err
	}
	c, err := shim.Dial(ctx, s.layout.SocketPath(id))
	if err != nil {
		// No shim to talk to; make sure nothing lingers in the cgroup.
		s.conn.KillUnitContext(ctx, unitName(id), int32(syscall.SIGKILL))
		return ErrNotRunning
	}
	defer c.Close()
	if err := c.Send(shim.Frame{Type: shim.TypeKill}); err != nil {
		return fmt.Errorf("send kill to %s: %w", id, err)
	}
	select {
	case <-c.Done():
	case <-time.After(15 * time.Second):
		return fmt.Errorf("server %s did not die after kill", id)
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (s *Systemd) Status(ctx context.Context, id string) (Status, error) {
	if err := checkID(id); err != nil {
		return Status{}, err
	}
	props, err := s.conn.GetUnitPropertiesContext(ctx, unitName(id))
	if err != nil {
		return Status{State: StateUnknown}, fmt.Errorf("get properties of %s: %w", unitName(id), err)
	}
	active, _ := props["ActiveState"].(string)
	sub, _ := props["SubState"].(string)
	switch active {
	case "active", "reloading":
		hello, err := shim.Probe(ctx, s.layout.SocketPath(id))
		if err != nil {
			return Status{State: StateStarting}, nil
		}
		st := statusFromShim(hello)
		if stats, err := s.Stats(ctx, id); err == nil {
			st.MemoryBytes, st.CPUNanos = stats.MemoryBytes, stats.CPUNanos
		}
		return st, nil
	case "activating":
		if sub == "auto-restart" {
			st := statusFromExit(s.layout.ServerDir(id))
			st.State = StateCrashed
			return st, nil
		}
		return Status{State: StateStarting}, nil
	case "deactivating":
		return Status{State: StateStopping}, nil
	case "failed":
		st := statusFromExit(s.layout.ServerDir(id))
		st.State = StateCrashed
		return st, nil
	default: // inactive
		return statusFromExit(s.layout.ServerDir(id)), nil
	}
}

func (s *Systemd) SetLimits(ctx context.Context, id string, l Limits) error {
	if err := checkID(id); err != nil {
		return err
	}
	mem := uint64(math.MaxUint64) // "infinity"
	if l.MemoryMB > 0 {
		mem = uint64(l.MemoryMB) * 1024 * 1024
	}
	cpu := uint64(math.MaxUint64)
	if l.CPUPercent > 0 {
		cpu = uint64(l.CPUPercent) * 10_000 // CPUQuotaPerSecUSec: 100% = 1_000_000 µs/s
	}
	props := []sd.Property{
		{Name: "MemoryMax", Value: dbus.MakeVariant(mem)},
		{Name: "CPUQuotaPerSecUSec", Value: dbus.MakeVariant(cpu)},
	}
	// runtime=false persists the drop-in across reboots.
	if err := s.conn.SetUnitPropertiesContext(ctx, unitName(id), false, props...); err != nil {
		return fmt.Errorf("set limits on %s: %w", unitName(id), err)
	}
	return nil
}

func (s *Systemd) SetAutostart(ctx context.Context, id string, on bool) error {
	if err := checkID(id); err != nil {
		return err
	}
	units := []string{unitName(id)}
	if on {
		if _, _, err := s.conn.EnableUnitFilesContext(ctx, units, false, true); err != nil {
			return fmt.Errorf("enable %s: %w", unitName(id), err)
		}
		return nil
	}
	if _, err := s.conn.DisableUnitFilesContext(ctx, units, false); err != nil {
		return fmt.Errorf("disable %s: %w", unitName(id), err)
	}
	return nil
}

func (s *Systemd) Attach(ctx context.Context, id string) (Console, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	c, err := shim.Dial(ctx, s.layout.SocketPath(id))
	if err != nil {
		return nil, ErrNotRunning
	}
	return c, nil
}

// Stats reads cgroup accounting through systemd; covers the whole unit,
// children included.
func (s *Systemd) Stats(ctx context.Context, id string) (Stats, error) {
	if err := checkID(id); err != nil {
		return Stats{}, err
	}
	var st Stats
	mem, err := s.conn.GetServicePropertyContext(ctx, unitName(id), "MemoryCurrent")
	if err != nil {
		return st, fmt.Errorf("MemoryCurrent of %s: %w", unitName(id), err)
	}
	if v, ok := mem.Value.Value().(uint64); ok && v != math.MaxUint64 {
		st.MemoryBytes = v
	}
	cpu, err := s.conn.GetServicePropertyContext(ctx, unitName(id), "CPUUsageNSec")
	if err == nil {
		if v, ok := cpu.Value.Value().(uint64); ok && v != math.MaxUint64 {
			st.CPUNanos = v
		}
	}
	st.At = time.Now()
	return st, nil
}

func (s *Systemd) Remove(ctx context.Context, id string) error {
	if err := checkID(id); err != nil {
		return err
	}
	if err := s.Stop(ctx, id); err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}
	_ = s.SetAutostart(ctx, id, false)
	_ = s.conn.ResetFailedUnitContext(ctx, unitName(id))
	return nil
}

// IsPermissionError reports whether err looks like polkit/D-Bus denying access,
// so the API can explain the fix instead of a bare 500.
func IsPermissionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "AccessDenied") || strings.Contains(msg, "Interactive authentication required") ||
		strings.Contains(msg, "not authorized")
}

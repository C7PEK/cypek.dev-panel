package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cypek019/cypek.dev---panel/internal/shim"
)

// Local runs shims as detached child processes (own session, output to
// .homepanel/shim.log). Used by `serve --dev`; no sandboxing, no autostart.
// Shims outlive a panel restart because they are not killed with the parent;
// the panel finds them again through their sockets.
type Local struct {
	layout Layout
	exe    string
	log    *slog.Logger
}

// NewLocal returns a dev supervisor. exe is the homepanel binary to run `shim` from.
func NewLocal(layout Layout, exe string, log *slog.Logger) *Local {
	return &Local{layout: layout, exe: exe, log: log}
}

func (l *Local) Name() string { return "local" }

func (l *Local) Start(ctx context.Context, id string) error {
	if err := checkID(id); err != nil {
		return err
	}
	if _, err := shim.Probe(ctx, l.layout.SocketPath(id)); err == nil {
		return ErrAlreadyRunning
	}
	dir := l.layout.ServerDir(id)
	if _, err := shim.LoadRunConfig(dir); err != nil {
		return err
	}
	metaDir := filepath.Join(dir, shim.MetaDir)
	logFile, err := os.OpenFile(filepath.Join(metaDir, shim.ShimLogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("open shim log: %w", err)
	}
	defer logFile.Close()

	sock := l.layout.SocketPath(id)
	// Flags come first: the flag package stops at the first positional argument.
	cmd := exec.Command(l.exe, "shim", "--dir", dir, "--socket", sock, id)
	cmd.Dir = dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start shim for %s: %w", id, err)
	}
	// Reap the child when it exits; it is detached, so this never blocks a stop.
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	l.log.Info("shim started", "server", id, "pid", cmd.Process.Pid)

	ok := waitUntil(ctx, 10*time.Second, func() bool {
		select {
		case <-exited:
			return true
		default:
		}
		_, err := shim.Probe(ctx, sock)
		return err == nil
	})
	select {
	case <-exited:
		return fmt.Errorf("shim for %s exited immediately: %s", id, tailFile(filepath.Join(metaDir, shim.ShimLogFile), 5))
	default:
	}
	if !ok {
		return fmt.Errorf("shim for %s did not come up in time", id)
	}
	return nil
}

func (l *Local) Stop(ctx context.Context, id string) error {
	return l.signal(ctx, id, shim.TypeStop)
}

func (l *Local) Kill(ctx context.Context, id string) error {
	return l.signal(ctx, id, shim.TypeKill)
}

// signal sends a stop/kill frame and waits until the shim is gone.
func (l *Local) signal(ctx context.Context, id, frameType string) error {
	if err := checkID(id); err != nil {
		return err
	}
	c, err := shim.Dial(ctx, l.layout.SocketPath(id))
	if err != nil {
		return ErrNotRunning
	}
	defer c.Close()
	if err := c.Send(shim.Frame{Type: frameType}); err != nil {
		return fmt.Errorf("send %s to %s: %w", frameType, id, err)
	}
	cfg, _ := shim.LoadRunConfig(l.layout.ServerDir(id))
	timeout := 30 * time.Second
	if cfg != nil && frameType == shim.TypeStop {
		timeout = time.Duration(cfg.Stop.TimeoutSec)*time.Second + 15*time.Second
	}
	select {
	case <-c.Done():
	case <-time.After(timeout):
		return fmt.Errorf("server %s did not stop within %s", id, timeout)
	case <-ctx.Done():
		return ctx.Err()
	}
	// The shim removes its socket right after closing clients; give it a moment.
	waitUntil(ctx, 2*time.Second, func() bool {
		_, err := os.Stat(l.layout.SocketPath(id))
		return errors.Is(err, os.ErrNotExist)
	})
	return nil
}

func (l *Local) Restart(ctx context.Context, id string) error {
	if err := l.Stop(ctx, id); err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}
	return l.Start(ctx, id)
}

func (l *Local) Status(ctx context.Context, id string) (Status, error) {
	if err := checkID(id); err != nil {
		return Status{}, err
	}
	hello, err := shim.Probe(ctx, l.layout.SocketPath(id))
	if err != nil {
		return statusFromExit(l.layout.ServerDir(id)), nil
	}
	st := statusFromShim(hello)
	if stats, err := procStats(hello.PID); err == nil {
		st.MemoryBytes, st.CPUNanos = stats.MemoryBytes, stats.CPUNanos
	}
	return st, nil
}

func (l *Local) SetLimits(ctx context.Context, id string, lim Limits) error {
	l.log.Debug("limits are not enforced by the local supervisor", "server", id, "memoryMB", lim.MemoryMB, "cpuPercent", lim.CPUPercent)
	return nil
}

func (l *Local) SetAutostart(ctx context.Context, id string, on bool) error {
	return nil
}

func (l *Local) Attach(ctx context.Context, id string) (Console, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	c, err := shim.Dial(ctx, l.layout.SocketPath(id))
	if err != nil {
		return nil, ErrNotRunning
	}
	return c, nil
}

// Stats reads /proc for the game's main process only (children are not
// summed). Good enough for development.
func (l *Local) Stats(ctx context.Context, id string) (Stats, error) {
	if err := checkID(id); err != nil {
		return Stats{}, err
	}
	hello, err := shim.Probe(ctx, l.layout.SocketPath(id))
	if err != nil || hello.PID == 0 {
		return Stats{}, ErrNotRunning
	}
	return procStats(hello.PID)
}

func (l *Local) Remove(ctx context.Context, id string) error {
	if err := l.Kill(ctx, id); err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}
	return nil
}

func procStats(pid int) (Stats, error) {
	statm, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return Stats{}, err
	}
	fields := strings.Fields(string(statm))
	var st Stats
	if len(fields) > 1 {
		pages, _ := strconv.ParseUint(fields[1], 10, 64)
		st.MemoryBytes = pages * uint64(os.Getpagesize())
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err == nil {
		// Fields after the command name (which may contain spaces) start at ")".
		if i := strings.LastIndexByte(string(stat), ')'); i > 0 {
			f := strings.Fields(string(stat[i+1:]))
			if len(f) > 12 {
				utime, _ := strconv.ParseUint(f[11], 10, 64)
				stime, _ := strconv.ParseUint(f[12], 10, 64)
				st.CPUNanos = (utime + stime) * uint64(time.Second/100) // USER_HZ is 100 on Linux
			}
		}
	}
	st.At = time.Now()
	return st, nil
}

func tailFile(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

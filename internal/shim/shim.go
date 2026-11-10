package shim

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Options configure one shim run.
type Options struct {
	ID         string
	ServerDir  string
	SocketPath string
	Log        *slog.Logger
	// BacklogLines is the console backlog size; zero means 2000.
	BacklogLines int
	// KeepLogFiles is how many daily console logs to retain; zero means 30.
	KeepLogFiles int
}

// Shim supervises one process.
type Shim struct {
	opts Options
	cfg  *RunConfig
	log  *slog.Logger

	ring   *lineRing
	logs   *logWriter
	events *eventLog
	ready  *regexp.Regexp
	readyB []byte // tail kept for matching readyRegex across chunks

	mu        sync.Mutex
	state     State
	pid       int
	startedAt time.Time
	exitCode  *int
	requested bool // stop was requested (by signal or frame)
	cmd       *exec.Cmd
	ptmx      *os.File
	clients   map[*client]struct{}
	stopOnce  sync.Once
	killOnce  sync.Once
	exited    chan struct{} // closed as soon as cmd.Wait returns
	attached  chan struct{} // closed when the first console client connects
	attachOne sync.Once
}

type client struct {
	conn net.Conn
	wmu  sync.Mutex
}

func (c *client) send(f Frame) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return WriteFrame(c.conn, f)
}

// New prepares a shim. Run starts the process.
func New(cfg *RunConfig, opts Options) (*Shim, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.BacklogLines <= 0 {
		opts.BacklogLines = 2000
	}
	if opts.KeepLogFiles <= 0 {
		opts.KeepLogFiles = 30
	}
	s := &Shim{
		opts:     opts,
		cfg:      cfg,
		log:      opts.Log,
		ring:     newLineRing(opts.BacklogLines),
		state:    StateStarting,
		clients:  map[*client]struct{}{},
		exited:   make(chan struct{}),
		attached: make(chan struct{}),
	}
	if cfg.ReadyRegex != "" {
		re, err := regexp.Compile(cfg.ReadyRegex)
		if err != nil {
			return nil, fmt.Errorf("readyRegex: %w", err)
		}
		s.ready = re
	}
	return s, nil
}

// Run starts the process and serves the socket until the process exits.
// Cancelling ctx requests a graceful stop. The returned code is what the shim
// should exit with: the process's exit code for an unrequested exit (so
// systemd's Restart=on-failure restarts crashes), and 0 whenever the stop or
// kill was requested through the panel or a signal.
func (s *Shim) Run(ctx context.Context) (int, error) {
	logs, err := newLogWriter(s.opts.ServerDir, s.opts.KeepLogFiles)
	if err != nil {
		return 1, err
	}
	s.logs = logs
	defer logs.Close()
	s.events = newEventLog(s.opts.ServerDir, s.cfg.ErrorRegex)
	s.events.disabled = s.cfg.Oneshot
	defer s.events.close()

	ln, err := s.listen()
	if err != nil {
		return 1, err
	}
	defer ln.Close()
	defer os.Remove(s.opts.SocketPath)

	if s.cfg.Oneshot {
		// A task's output is the point: let the panel attach first so it
		// sees every line, but never hang if nobody comes.
		go s.acceptLoop(ln)
		select {
		case <-s.attached:
		case <-time.After(oneshotAttachWait):
		case <-ctx.Done():
			return 0, nil
		}
	}
	if err := s.startProcess(); err != nil {
		code := ExitConfigError
		s.events.record(Event{Kind: "crash", Message: "Could not start: " + err.Error(), ExitCode: &code})
		_ = writeExitInfo(s.opts.ServerDir, ExitInfo{
			ExitCode: ExitConfigError,
			ExitedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Error:    err.Error(),
		})
		return ExitConfigError, err
	}
	defer os.Remove(filepath.Join(s.opts.ServerDir, MetaDir, PIDFile))

	if !s.cfg.Oneshot {
		go s.acceptLoop(ln)
	}
	readDone := make(chan struct{})
	go func() {
		s.readLoop()
		close(readDone)
	}()

	var waitErr error
	go func() {
		waitErr = s.cmd.Wait()
		close(s.exited)
	}()

	select {
	case <-ctx.Done():
		s.log.Info("stop requested")
		s.gracefulStop()
		<-s.exited
	case <-s.exited:
	}
	// The process is gone but the terminal may still hold its last lines
	// (often the error that made it exit): read them before finish closes
	// the PTY and the clients. Bounded, because a leftover child can keep
	// the terminal open.
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
	}
	code := exitCodeOf(waitErr)
	requested := s.finish(code)
	if requested || s.cfg.Oneshot {
		return 0, nil
	}
	return code, nil
}

// oneshotAttachWait bounds how long a oneshot task waits for the panel.
const oneshotAttachWait = 10 * time.Second

func (s *Shim) listen() (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(s.opts.SocketPath), 0o750); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}
	// A stale socket from a crashed shim would block Listen; a live one means
	// another shim owns this server.
	if conn, err := net.DialTimeout("unix", s.opts.SocketPath, 300*time.Millisecond); err == nil {
		conn.Close()
		return nil, fmt.Errorf("another shim is already serving %s", s.opts.SocketPath)
	}
	_ = os.Remove(s.opts.SocketPath)
	ln, err := net.Listen("unix", s.opts.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", s.opts.SocketPath, err)
	}
	_ = os.Chmod(s.opts.SocketPath, 0o660)
	return ln, nil
}

func (s *Shim) startProcess() error {
	cmd := exec.Command(s.cfg.Command, s.cfg.Args...)
	cmd.Dir = filepath.Join(s.opts.ServerDir, filepath.FromSlash(s.cfg.Dir))
	cmd.Env = s.environment()
	// A relative command is relative to the server dir, not the working dir,
	// so templates can say "artifact/run.sh" or ".venv/bin/python" regardless of dir.
	if strings.Contains(cmd.Path, "/") && !filepath.IsAbs(cmd.Path) {
		cmd.Path = filepath.Join(s.opts.ServerDir, cmd.Path)
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: s.cfg.Cols, Rows: s.cfg.Rows})
	if err != nil {
		return fmt.Errorf("start %s: %w", s.cfg.Command, err)
	}
	s.mu.Lock()
	s.cmd = cmd
	s.ptmx = ptmx
	s.pid = cmd.Process.Pid
	s.startedAt = time.Now()
	if s.ready == nil {
		s.state = StateRunning
	}
	s.mu.Unlock()
	_ = os.WriteFile(filepath.Join(s.opts.ServerDir, MetaDir, PIDFile), []byte(strconv.Itoa(os.Getpid())), 0o600)
	s.log.Info("process started", "pid", cmd.Process.Pid, "command", s.cfg.Command)
	if s.events != nil {
		s.events.record(Event{Kind: "start", Message: fmt.Sprintf("Process started (PID %d)", cmd.Process.Pid)})
	}
	return nil
}

// environment builds a minimal, predictable environment for the game.
func (s *Shim) environment() []string {
	env := map[string]string{
		"HOME": s.opts.ServerDir,
		"PWD":  s.opts.ServerDir,
		"TERM": "xterm-256color",
		"LANG": "C.UTF-8",
	}
	for _, k := range []string{"PATH", "USER", "LOGNAME", "TZ"} {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	if _, ok := env["PATH"]; !ok {
		env["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	for k, v := range s.cfg.Env {
		env[k] = v
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// readLoop pumps PTY output to the log, the backlog and connected clients.
func (s *Shim) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, _ = s.logs.Write(chunk)
			s.ring.Write(chunk)
			s.events.output(chunk)
			s.checkReady(chunk)
			s.broadcast(Frame{Type: TypeOutput, Data: string(chunk)})
		}
		if err != nil {
			// EIO is the normal end: the slave side closed when the process exited.
			if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.EIO) {
				s.log.Debug("pty read ended", "err", err)
			}
			return
		}
	}
}

func (s *Shim) checkReady(chunk []byte) {
	if s.ready == nil {
		return
	}
	s.mu.Lock()
	if s.state != StateStarting {
		s.mu.Unlock()
		return
	}
	s.readyB = append(s.readyB, chunk...)
	if len(s.readyB) > 64*1024 {
		s.readyB = s.readyB[len(s.readyB)-64*1024:]
	}
	matched := s.ready.Match(s.readyB)
	if matched {
		s.state = StateRunning
		s.readyB = nil
	}
	s.mu.Unlock()
	if matched {
		s.log.Info("server ready")
		s.events.record(Event{Kind: "ready", Message: fmt.Sprintf("Ready after %s", time.Since(s.startedAt).Round(time.Second))})
		s.broadcast(Frame{Type: TypeState, State: StateRunning})
	}
}

func (s *Shim) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go s.serveClient(conn)
	}
}

func (s *Shim) serveClient(conn net.Conn) {
	c := &client{conn: conn}
	s.mu.Lock()
	hello := Frame{Type: TypeHello, State: s.state, PID: s.pid, StartedAt: s.startedAt.UTC().Format(time.RFC3339Nano)}
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	s.attachOne.Do(func() { close(s.attached) })
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		conn.Close()
	}()
	if err := c.send(hello); err != nil {
		return
	}
	if backlog := s.ring.Snapshot(); backlog != "" {
		if err := c.send(Frame{Type: TypeOutput, Data: backlog}); err != nil {
			return
		}
	}
	fr := NewFrameReader(conn)
	for {
		f, err := fr.Next()
		if err != nil {
			return
		}
		s.handleFrame(c, f)
	}
}

func (s *Shim) handleFrame(c *client, f Frame) {
	switch f.Type {
	case TypeInput:
		if len(f.Data) > MaxInputBytes {
			_ = c.send(Frame{Type: TypeError, Message: "input too long"})
			return
		}
		s.mu.Lock()
		ptmx, st := s.ptmx, s.state
		s.mu.Unlock()
		if st == StateStopped || ptmx == nil {
			_ = c.send(Frame{Type: TypeError, Message: "process not running"})
			return
		}
		if _, err := ptmx.Write([]byte(f.Data)); err != nil {
			_ = c.send(Frame{Type: TypeError, Message: "write failed: " + err.Error()})
		}
	case TypeStop:
		go s.gracefulStop()
	case TypeKill:
		s.kill()
	case TypeResize:
		if f.Cols > 0 && f.Rows > 0 && f.Cols <= 1000 && f.Rows <= 1000 {
			s.mu.Lock()
			ptmx := s.ptmx
			s.mu.Unlock()
			if ptmx != nil {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: f.Cols, Rows: f.Rows})
			}
		}
	case TypePing:
		_ = c.send(Frame{Type: TypePong})
	default:
		_ = c.send(Frame{Type: TypeError, Message: "unknown frame type " + f.Type})
	}
}

func (s *Shim) broadcast(f Frame) {
	s.mu.Lock()
	targets := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		targets = append(targets, c)
	}
	s.mu.Unlock()
	for _, c := range targets {
		if err := c.send(f); err != nil {
			c.conn.Close()
		}
	}
}

// gracefulStop sends the stop command (or SIGTERM), waits for the timeout,
// then kills the process group. Safe to call more than once.
func (s *Shim) gracefulStop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		if s.state == StateStopped {
			s.mu.Unlock()
			return
		}
		s.state = StateStopping
		s.requested = true
		ptmx, cmd := s.ptmx, s.cmd
		s.mu.Unlock()
		s.broadcast(Frame{Type: TypeState, State: StateStopping})

		if sig, ok := stopSignal(s.cfg.Stop.Command); ok {
			s.log.Info("sending stop signal", "signal", sig.String())
			_ = cmd.Process.Signal(sig)
		} else if s.cfg.Stop.Command != "" {
			s.log.Info("sending stop command", "command", s.cfg.Stop.Command)
			_, _ = ptmx.Write([]byte(s.cfg.Stop.Command + "\n"))
		} else {
			s.log.Info("sending SIGTERM")
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-s.exited:
		case <-time.After(time.Duration(min(s.cfg.Stop.TimeoutSec, MaxStopTimeoutSec)) * time.Second):
			s.log.Warn("stop timeout exceeded, killing", "timeoutSec", s.cfg.Stop.TimeoutSec)
			s.kill()
		}
	})
}

// kill sends SIGKILL to the whole process group.
func (s *Shim) kill() {
	s.killOnce.Do(func() {
		s.mu.Lock()
		s.requested = true
		cmd := s.cmd
		s.mu.Unlock()
		if cmd == nil || cmd.Process == nil {
			return
		}
		// pty.Start puts the child in its own session, so -pid is its group.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			_ = cmd.Process.Kill()
		}
	})
}

// finish records the exit, tells clients and closes everything. It reports
// whether the exit was requested.
func (s *Shim) finish(code int) bool {
	s.mu.Lock()
	s.state = StateStopped
	s.exitCode = &code
	requested := s.requested
	if s.ptmx != nil {
		s.ptmx.Close()
	}
	s.mu.Unlock()
	_ = writeExitInfo(s.opts.ServerDir, ExitInfo{
		ExitCode:  code,
		ExitedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Requested: requested,
	})
	s.log.Info("process exited", "exitCode", code, "requested", requested)
	switch {
	case requested:
		s.events.record(Event{Kind: "stop", Message: "Stopped", ExitCode: &code})
	case code == 0:
		s.events.record(Event{Kind: "exit", Message: "The process exited on its own", ExitCode: &code})
	default:
		s.events.record(Event{Kind: "crash", Message: fmt.Sprintf("Crashed with exit code %d", code), ExitCode: &code})
	}
	s.broadcast(Frame{Type: TypeState, State: StateStopped, ExitCode: &code})
	s.mu.Lock()
	for c := range s.clients {
		c.conn.Close()
	}
	s.mu.Unlock()
	return requested
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if st, ok := ee.Sys().(syscall.WaitStatus); ok && st.Signaled() {
			return 128 + int(st.Signal())
		}
		return ee.ExitCode()
	}
	return 1
}

// stopSignal maps the Pterodactyl-style signal notation ("^C" = SIGINT) used
// by imported templates to a real signal instead of console input.
func stopSignal(command string) (syscall.Signal, bool) {
	switch strings.ToUpper(strings.TrimSpace(command)) {
	case "^C", "SIGINT":
		return syscall.SIGINT, true
	case "SIGTERM":
		return syscall.SIGTERM, true
	case "SIGKILL":
		return syscall.SIGKILL, true
	}
	return 0, false
}

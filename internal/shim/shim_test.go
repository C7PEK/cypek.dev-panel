package shim

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("HOMEPANEL_FAKE_GAME") == "1" {
		os.Exit(fakeGame())
	}
	os.Exit(m.Run())
}

func fakeGame() int {
	mode := os.Getenv("FAKE_MODE")
	fmt.Println("Starting fake server...")
	time.Sleep(50 * time.Millisecond)
	fmt.Println("Done (0.05s)! For help, type \"help\"")
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "stop" && mode == "hang":
			fmt.Println("ignoring stop")
		case line == "stop":
			fmt.Println("Stopping the server")
			return 0
		case line == "crash":
			fmt.Println("boom")
			return 3
		case line == "burst":
			w := bufio.NewWriter(os.Stdout)
			for i := 0; i < 3000; i++ {
				fmt.Fprintf(w, "line %d\n", i)
			}
			fmt.Fprintln(w, "LAST-LINE")
			w.Flush()
			return 3
		default:
			fmt.Printf("> %s\n", line)
		}
	}
	return 0
}

type harness struct {
	t      *testing.T
	dir    string
	sock   string
	shim   *Shim
	result chan int
	cancel context.CancelFunc
}

func start(t *testing.T, cfg RunConfig, mode string) *harness {
	t.Helper()
	dir := t.TempDir()

	sockDir, err := os.MkdirTemp("", "hp-sock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	cfg.Command = os.Args[0]
	cfg.Env = map[string]string{"HOMEPANEL_FAKE_GAME": "1", "FAKE_MODE": mode}
	if err := SaveRunConfig(dir, &cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRunConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	s, err := New(loaded, Options{ID: "test", ServerDir: dir, SocketPath: filepath.Join(sockDir, "c.sock"), Log: log, BacklogLines: 50})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, dir: dir, sock: filepath.Join(sockDir, "c.sock"), shim: s, result: make(chan int, 1), cancel: cancel}
	go func() {
		code, err := s.Run(ctx)
		if err != nil {
			t.Errorf("Run: %v", err)
		}
		h.result <- code
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.result:
		case <-time.After(10 * time.Second):
		}
	})
	return h
}

func (h *harness) dial() *Client {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := Dial(context.Background(), h.sock)
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("dial: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitFor(t *testing.T, c *Client, what string, pred func(Frame, string) bool) string {
	t.Helper()
	var out strings.Builder
	timeout := time.After(10 * time.Second)
	for {
		select {
		case f, ok := <-c.Frames():
			if !ok {
				t.Fatalf("connection closed while waiting for %s; output so far:\n%s", what, out.String())
			}
			if f.Type == TypeOutput {
				out.WriteString(f.Data)
			}
			if pred(f, out.String()) {
				return out.String()
			}
		case <-timeout:
			t.Fatalf("timeout waiting for %s; output so far:\n%s", what, out.String())
		}
	}
}

func (h *harness) exitCode() int {
	h.t.Helper()
	select {
	case code := <-h.result:
		h.result <- code
		return code
	case <-time.After(10 * time.Second):
		h.t.Fatal("shim did not exit")
		return -1
	}
}

func TestLifecycleWithStopCommand(t *testing.T) {
	h := start(t, RunConfig{Stop: StopConfig{Command: "stop", TimeoutSec: 5}, ReadyRegex: `Done \(\d+\.\d+s\)!`}, "")
	c := h.dial()
	defer c.Close()

	if c.Hello.PID == 0 || c.Hello.StartedAt == "" {
		t.Errorf("hello = %+v", c.Hello)
	}

	if c.Hello.State != StateRunning {
		waitFor(t, c, "running state", func(f Frame, _ string) bool { return f.Type == TypeState && f.State == StateRunning })
	}
	if err := c.Input("hello world\n"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "echo", func(_ Frame, out string) bool { return strings.Contains(out, "> hello world") })

	if err := c.Send(Frame{Type: TypeStop}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "stopping", func(f Frame, _ string) bool { return f.Type == TypeState && f.State == StateStopping })
	var code *int
	out := waitFor(t, c, "stopped", func(f Frame, _ string) bool {
		if f.Type == TypeState && f.State == StateStopped {
			code = f.ExitCode
			return true
		}
		return false
	})
	if code == nil || *code != 0 {
		t.Errorf("exit code frame = %v", code)
	}
	if !strings.Contains(out, "Stopping the server") {
		t.Errorf("stop command not delivered, output:\n%s", out)
	}
	if got := h.exitCode(); got != 0 {
		t.Errorf("Run exit code = %d", got)
	}
	info, err := ReadExitInfo(h.dir)
	if err != nil || info == nil || info.ExitCode != 0 || !info.Requested {
		t.Errorf("exit info = %+v, %v", info, err)
	}
	if _, err := os.Stat(h.sock); !os.IsNotExist(err) {
		t.Error("socket not removed after exit")
	}
	logs, _ := filepath.Glob(filepath.Join(h.dir, LogsDir, "console-*.log"))
	if len(logs) != 1 {
		t.Fatalf("console logs = %v", logs)
	}
	b, _ := os.ReadFile(logs[0])
	if !strings.Contains(string(b), "hello world") {
		t.Errorf("console log missing output: %q", b)
	}
}

func TestBacklogOnAttach(t *testing.T) {
	h := start(t, RunConfig{Stop: StopConfig{Command: "stop"}}, "")
	c1 := h.dial()
	_ = c1.Input("first\n")
	waitFor(t, c1, "first echo", func(_ Frame, out string) bool { return strings.Contains(out, "> first") })
	c1.Close()

	c2 := h.dial()
	defer c2.Close()
	out := waitFor(t, c2, "backlog", func(_ Frame, out string) bool { return strings.Contains(out, "> first") })
	if !strings.Contains(out, "Starting fake server") {
		t.Errorf("backlog incomplete:\n%s", out)
	}
}

func TestStopTimeoutKills(t *testing.T) {
	h := start(t, RunConfig{Stop: StopConfig{Command: "stop", TimeoutSec: 1}}, "hang")
	c := h.dial()
	defer c.Close()
	startAt := time.Now()
	_ = c.Send(Frame{Type: TypeStop})
	waitFor(t, c, "stopped", func(f Frame, _ string) bool { return f.Type == TypeState && f.State == StateStopped })
	if code := h.exitCode(); code != 0 {
		t.Errorf("Run code = %d, want 0 (requested stop must not look like a crash)", code)
	}
	if info, _ := ReadExitInfo(h.dir); info == nil || info.ExitCode != 137 || !info.Requested {
		t.Errorf("exit info = %+v, want 137 requested", info)
	}
	if el := time.Since(startAt); el < 900*time.Millisecond || el > 6*time.Second {
		t.Errorf("stop took %v, expected ~1s timeout", el)
	}
}

func TestCrashIsNotRequested(t *testing.T) {
	h := start(t, RunConfig{Stop: StopConfig{Command: "stop"}}, "")
	c := h.dial()
	defer c.Close()
	_ = c.Input("crash\n")
	waitFor(t, c, "stopped", func(f Frame, _ string) bool { return f.Type == TypeState && f.State == StateStopped })
	if code := h.exitCode(); code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	info, _ := ReadExitInfo(h.dir)
	if info == nil || info.Requested || info.ExitCode != 3 {
		t.Errorf("exit info = %+v", info)
	}
}

func TestContextCancelStopsGracefully(t *testing.T) {
	h := start(t, RunConfig{Stop: StopConfig{Command: "stop", TimeoutSec: 5}}, "")
	c := h.dial()
	defer c.Close()
	waitFor(t, c, "ready", func(_ Frame, out string) bool { return strings.Contains(out, "Done") })
	h.cancel()
	out := waitFor(t, c, "stopped", func(f Frame, _ string) bool { return f.Type == TypeState && f.State == StateStopped })
	if !strings.Contains(out, "Stopping the server") {
		t.Errorf("graceful stop not used:\n%s", out)
	}
	if code := h.exitCode(); code != 0 {
		t.Errorf("exit code = %d", code)
	}
}

func TestKillFrame(t *testing.T) {
	h := start(t, RunConfig{Stop: StopConfig{Command: "stop"}}, "hang")
	c := h.dial()
	defer c.Close()
	_ = c.Send(Frame{Type: TypeKill})
	waitFor(t, c, "stopped", func(f Frame, _ string) bool { return f.Type == TypeState && f.State == StateStopped })
	if code := h.exitCode(); code != 0 {
		t.Errorf("Run code = %d, want 0 for requested kill", code)
	}
}

func TestInputLimitsAndUnknownFrames(t *testing.T) {
	h := start(t, RunConfig{Stop: StopConfig{Command: "stop"}}, "")
	c := h.dial()
	defer c.Close()
	_ = c.Input(strings.Repeat("x", MaxInputBytes+1))
	waitFor(t, c, "input error", func(f Frame, _ string) bool { return f.Type == TypeError && strings.Contains(f.Message, "too long") })
	_ = c.Send(Frame{Type: "bogus"})
	waitFor(t, c, "unknown frame error", func(f Frame, _ string) bool { return f.Type == TypeError && strings.Contains(f.Message, "unknown") })
	_ = c.Send(Frame{Type: TypePing})
	waitFor(t, c, "pong", func(f Frame, _ string) bool { return f.Type == TypePong })
}

func TestSecondShimRefusesLiveSocket(t *testing.T) {
	h := start(t, RunConfig{Stop: StopConfig{Command: "stop"}}, "")
	c := h.dial()
	defer c.Close()
	cfg, _ := LoadRunConfig(h.dir)
	s2, err := New(cfg, Options{ID: "dup", ServerDir: h.dir, SocketPath: h.sock})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "already serving") {
		t.Errorf("second shim: err = %v", err)
	}
}

func TestLineRing(t *testing.T) {
	r := newLineRing(3)
	r.Write([]byte("a\nb\nc\nd\npar"))
	if got := r.Snapshot(); got != "b\nc\nd\npar" {
		t.Errorf("snapshot = %q", got)
	}
	r.Write([]byte("tial\n"))
	if got := r.Snapshot(); got != "c\nd\npartial\n" {
		t.Errorf("snapshot = %q", got)
	}
}

func TestLogWriterRotatesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	lw, err := newLogWriter(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	lw.now = func() time.Time { return day }
	for i := 0; i < 4; i++ {
		if _, err := lw.Write([]byte(fmt.Sprintf("day %d\n", i))); err != nil {
			t.Fatal(err)
		}
		day = day.AddDate(0, 0, 1)
	}
	lw.Close()
	logs, _ := filepath.Glob(filepath.Join(dir, LogsDir, "console-*.log"))
	if len(logs) != 2 {
		t.Fatalf("logs = %v, want 2 kept", logs)
	}
	if filepath.Base(logs[0]) != "console-2026-01-03.log" || filepath.Base(logs[1]) != "console-2026-01-04.log" {
		t.Errorf("kept wrong files: %v", logs)
	}
}

func TestRunConfigValidation(t *testing.T) {
	if _, err := LoadRunConfig(t.TempDir()); err == nil {
		t.Error("missing run.json accepted")
	}
	dir := t.TempDir()
	if err := SaveRunConfig(dir, &RunConfig{}); err == nil {
		t.Error("empty command accepted")
	}
	if err := SaveRunConfig(dir, &RunConfig{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRunConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Stop.TimeoutSec != DefaultStopTimeoutSec || cfg.Cols != DefaultCols {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	if _, err := New(&RunConfig{Command: "x", ReadyRegex: "("}, Options{}); err == nil {
		t.Error("bad regex accepted")
	}
}

func TestEventLogErrorsAndLifecycle(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, MetaDir), 0o750)
	l := newEventLog(dir, "")
	l.record(Event{Kind: "start", Message: "Process started"})
	l.output([]byte("[12:00:00 INFO]: Loading\n\x1b[31m[12:00:01 ERROR]: Could not load plugin\x1b[0m\n"))
	l.output([]byte("java.lang.NullPointerException: x\n\tat a.b.C(C.java:1)\n"))
	time.Sleep(errorBurstWindow + 700*time.Millisecond)
	l.output([]byte("[12:00:05 INFO]: fine\nTraceback (most recent call last):\n  File \"bot.py\", line 1\n"))
	code := 3
	l.record(Event{Kind: "crash", Message: "Crashed", ExitCode: &code})
	l.close()

	evs, err := ReadEvents(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "crash,error,error,start" {
		t.Fatalf("kinds = %v", kinds)
	}
	first := evs[2]
	if first.Message != "[12:00:01 ERROR]: Could not load plugin" || len(first.Lines) != 3 {
		t.Errorf("error event = %+v", first)
	}
	if !strings.HasPrefix(evs[1].Message, "Traceback") {
		t.Errorf("python error = %+v", evs[1])
	}
	if evs[0].ExitCode == nil || *evs[0].ExitCode != 3 {
		t.Errorf("crash = %+v", evs[0])
	}
	if got, _ := ReadEvents(dir, 2); len(got) != 2 || got[0].Kind != "crash" {
		t.Errorf("limit = %+v", got)
	}
}

func TestEventLogRateLimit(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, MetaDir), 0o750)
	l := newEventLog(dir, `ERR`)
	for i := 0; i < errorEventsPerMin+15; i++ {
		l.mu.Lock()
		l.line("ERR " + strconv.Itoa(i))
		l.flushLocked()
		l.mu.Unlock()
	}
	l.close()
	evs, _ := ReadEvents(dir, 1000)
	if len(evs) != errorEventsPerMin {
		t.Errorf("events = %d, want %d", len(evs), errorEventsPerMin)
	}
}

func TestShimRecordsLifecycleEvents(t *testing.T) {
	h := start(t, RunConfig{Stop: StopConfig{Command: "stop", TimeoutSec: 5}, ReadyRegex: `Done`}, "")
	c := h.dial()
	defer c.Close()
	waitFor(t, c, "ready", func(f Frame, out string) bool { return strings.Contains(out, "Done") })
	_ = c.Send(Frame{Type: TypeStop})
	waitFor(t, c, "stopped", func(f Frame, _ string) bool { return f.Type == TypeState && f.State == StateStopped })
	h.exitCode()
	time.Sleep(100 * time.Millisecond)
	evs, _ := ReadEvents(h.dir, 10)
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "stop,ready,start" {
		t.Errorf("kinds = %v", kinds)
	}
}

func TestLastOutputBeforeExitIsDelivered(t *testing.T) {
	for i := 0; i < 10; i++ {
		h := start(t, RunConfig{}, "")
		c := h.dial()
		waitFor(t, c, "ready", func(_ Frame, out string) bool { return strings.Contains(out, "Done") })
		_ = c.Send(Frame{Type: TypeInput, Data: "burst\n"})
		var out strings.Builder
		timeout := time.After(10 * time.Second)
	read:
		for {
			select {
			case f, ok := <-c.Frames():
				if !ok {
					break read
				}
				if f.Type == TypeOutput {
					out.WriteString(f.Data)
				}
			case <-timeout:
				t.Fatal("timeout")
			}
		}
		c.Close()
		if !strings.Contains(out.String(), "LAST-LINE") {
			t.Fatalf("run %d: final output lost (%d bytes received)", i, out.Len())
		}
		h.exitCode()
	}
}

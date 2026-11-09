package shim

import (
	"bufio"
	"encoding/json"
	"github.com/C7PEK/cypek.dev-panel/internal/safefs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Event is one entry of the server's history: lifecycle changes recorded by
// the shim plus error bursts detected in the console output. Stored as JSON
// lines in .homepanel/events.jsonl so the panel can read them even while it
// was down when they happened.
type Event struct {
	At       string   `json:"at"`
	Kind     string   `json:"kind"` // start | ready | stop | exit | crash | error
	Message  string   `json:"message,omitempty"`
	Lines    []string `json:"lines,omitempty"` // error context (stack trace)
	Count    int      `json:"count,omitempty"` // similar errors folded into this one
	ExitCode *int     `json:"exitCode,omitempty"`
}

const (
	EventsFile        = "events.jsonl"
	eventsMaxBytes    = 512 * 1024
	errorBurstWindow  = 1500 * time.Millisecond
	errorMaxLines     = 40
	errorEventsPerMin = 20
	// DefaultErrorRegex catches the usual suspects across Java, Node, Python
	// and FXServer logs.
	DefaultErrorRegex = `(?i)(\[[^\]]*\b(ERROR|SEVERE|FATAL)\b|\b(ERROR|SEVERE|FATAL)\]|^\s*(ERROR|FATAL)\b|Exception in thread|Traceback \(most recent call last\)|Unhandled(Promise)?Rejection|\bpanic:|SCRIPT ERROR)`
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\r`)

// StripANSI removes terminal escape sequences from a console line.
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// eventLog appends events and detects error bursts in output.
type eventLog struct {
	path string
	re   *regexp.Regexp

	mu         sync.Mutex
	partial    strings.Builder
	pending    *Event
	deadline   time.Time
	minute     time.Time
	inMinute   int
	suppressed int
	stop       chan struct{}
	disabled   bool // oneshot tasks do not pollute the server's history
}

func newEventLog(serverDir, errorRegex string) *eventLog {
	if errorRegex == "" {
		errorRegex = DefaultErrorRegex
	}
	re, err := regexp.Compile(errorRegex)
	if err != nil {
		re = regexp.MustCompile(DefaultErrorRegex)
	}
	l := &eventLog{path: filepath.Join(serverDir, MetaDir, EventsFile), re: re, stop: make(chan struct{})}
	go l.flusher()
	return l
}

func (l *eventLog) close() {
	l.mu.Lock()
	l.flushLocked()
	l.mu.Unlock()
	select {
	case <-l.stop:
	default:
		close(l.stop)
	}
}

// flusher closes error bursts once the output goes quiet.
func (l *eventLog) flusher() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case now := <-t.C:
			l.mu.Lock()
			if l.pending != nil && now.After(l.deadline) {
				l.flushLocked()
			}
			l.mu.Unlock()
		}
	}
}

// record writes a lifecycle event immediately (after any pending error).
func (l *eventLog) record(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushLocked()
	l.appendLocked(e)
}

// output feeds console output; complete lines are scanned for errors.
func (l *eventLog) output(chunk []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := string(chunk)
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			l.partial.WriteString(s)
			if l.partial.Len() > 8192 { // runaway line without newline
				l.partial.Reset()
			}
			return
		}
		l.partial.WriteString(s[:i])
		l.line(StripANSI(l.partial.String()))
		l.partial.Reset()
		s = s[i+1:]
	}
}

func (l *eventLog) line(line string) {
	line = strings.TrimRight(line, " \t")
	if line == "" {
		return
	}
	now := time.Now()
	if l.pending != nil && now.Before(l.deadline) {
		if len(l.pending.Lines) < errorMaxLines {
			l.pending.Lines = append(l.pending.Lines, truncateLine(line))
		}
		return
	}
	if l.pending != nil {
		l.flushLocked()
	}
	if !l.re.MatchString(line) {
		return
	}
	if now.Sub(l.minute) > time.Minute {
		l.minute, l.inMinute = now, 0
	}
	l.inMinute++
	if l.inMinute > errorEventsPerMin {
		l.suppressed++
		return
	}
	l.pending = &Event{At: now.UTC().Format(time.RFC3339Nano), Kind: "error", Message: truncateLine(line), Lines: []string{truncateLine(line)}}
	l.deadline = now.Add(errorBurstWindow)
}

func (l *eventLog) flushLocked() {
	if l.pending == nil {
		return
	}
	e := *l.pending
	l.pending = nil
	if l.suppressed > 0 {
		e.Count = l.suppressed
		l.suppressed = 0
	}
	if len(e.Lines) <= 1 {
		e.Lines = nil
	}
	l.appendLocked(e)
}

func (l *eventLog) appendLocked(e Event) {
	if l.disabled {
		return
	}
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if st, err := os.Stat(l.path); err == nil && st.Size() > eventsMaxBytes {
		_ = os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(e)
	_, _ = f.Write(append(b, '\n'))
}

func truncateLine(s string) string {
	if len(s) > 500 {
		return s[:500] + "…"
	}
	return s
}

// ReadEvents returns up to limit events, newest first, from events.jsonl and
// its rotated predecessor.
func ReadEvents(serverDir string, limit int) ([]Event, error) {
	base := filepath.Join(serverDir, MetaDir, EventsFile)
	var all []Event
	for _, p := range []string{base + ".1", base} {
		// Regular files only, size-capped: the game can replace them.
		f, err := safefs.OpenRegular(serverDir, p[len(serverDir)+1:])
		if err != nil {
			continue
		}
		if st, err := f.Stat(); err != nil || st.Size() > 8*eventsMaxBytes {
			f.Close()
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			var e Event
			if json.Unmarshal(sc.Bytes(), &e) == nil && e.Kind != "" {
				all = append(all, e)
			}
		}
		f.Close()
	}
	out := make([]Event, 0, min(limit, len(all)))
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, all[i])
	}
	return out, nil
}

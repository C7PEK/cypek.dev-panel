package shim

import (
	"strings"
	"sync"
)

// lineRing keeps the last n console lines for the backlog sent on attach.
type lineRing struct {
	mu      sync.Mutex
	lines   []string
	start   int
	count   int
	partial strings.Builder
}

func newLineRing(n int) *lineRing {
	return &lineRing{lines: make([]string, n)}
}

// Write splits chunk into lines; the trailing partial line is held back.
func (r *lineRing) Write(chunk []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := string(chunk)
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			r.partial.WriteString(s)
			return
		}
		r.partial.WriteString(s[:i+1])
		r.push(r.partial.String())
		r.partial.Reset()
		s = s[i+1:]
	}
}

func (r *lineRing) push(line string) {
	if len(r.lines) == 0 {
		return
	}
	idx := (r.start + r.count) % len(r.lines)
	if r.count == len(r.lines) {
		r.start = (r.start + 1) % len(r.lines)
	} else {
		r.count++
	}
	r.lines[idx] = line
}

// Snapshot returns the backlog as one string (lines plus the partial line).
func (r *lineRing) Snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for i := 0; i < r.count; i++ {
		b.WriteString(r.lines[(r.start+i)%len(r.lines)])
	}
	b.WriteString(r.partial.String())
	return b.String()
}

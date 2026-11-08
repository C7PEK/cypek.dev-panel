package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// logWriter appends console output to logs/console-YYYY-MM-DD.log, switching
// files at midnight (UTC) and pruning the oldest files beyond keep.
type logWriter struct {
	mu   sync.Mutex
	dir  string
	keep int
	day  string
	f    *os.File
	now  func() time.Time
}

const logFilePrefix = "console-"

func newLogWriter(serverDir string, keep int) (*logWriter, error) {
	dir := filepath.Join(serverDir, LogsDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}
	return &logWriter{dir: dir, keep: keep, now: time.Now}, nil
}

func (l *logWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	day := l.now().UTC().Format("2006-01-02")
	if l.f == nil || day != l.day {
		if l.f != nil {
			l.f.Close()
		}
		f, err := os.OpenFile(filepath.Join(l.dir, logFilePrefix+day+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
		if err != nil {
			return 0, err
		}
		l.f, l.day = f, day
		l.prune()
	}
	return l.f.Write(p)
}

// prune removes the oldest console logs beyond keep. Names sort by date.
func (l *logWriter) prune() {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); strings.HasPrefix(n, logFilePrefix) && strings.HasSuffix(n, ".log") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for len(names) > l.keep {
		os.Remove(filepath.Join(l.dir, names[0]))
		names = names[1:]
	}
}

func (l *logWriter) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		err := l.f.Close()
		l.f = nil
		return err
	}
	return nil
}

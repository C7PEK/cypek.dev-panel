package runtimes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	archivepkg "github.com/C7PEK/cypek.dev-panel/internal/archive"
	"github.com/C7PEK/cypek.dev-panel/internal/providers"
)

type Java struct {
	Dir      string
	Adoptium *providers.Adoptium

	mu sync.Mutex
}

type Logf func(format string, args ...any)

func (j *Java) Path(feature int) (string, bool) {
	p := filepath.Join(j.Dir, fmt.Sprintf("java-%d", feature), "bin", "java")
	st, err := os.Stat(p)
	return p, err == nil && st.Mode()&0o111 != 0
}

func (j *Java) Ensure(ctx context.Context, feature int, logf Logf, progress providers.Progress) (string, error) {
	if p, ok := j.Path(feature); ok {
		logf("Java %d already installed (%s)", feature, p)
		return p, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if p, ok := j.Path(feature); ok {
		return p, nil
	}
	build, err := j.Adoptium.Latest(ctx, feature)
	if err != nil {
		return "", err
	}
	logf("Downloading Java %d (%s, %d MB)", feature, build.ReleaseName, build.Size/1024/1024)
	if err := os.MkdirAll(j.Dir, 0o750); err != nil {
		return "", err
	}
	archive := filepath.Join(j.Dir, fmt.Sprintf(".java-%d.tar.gz", feature))
	defer os.Remove(archive)
	if err := providers.Download(ctx, build.URL, archive, build.SHA256, progress); err != nil {
		return "", err
	}
	logf("Checksum OK, extracting")
	target := filepath.Join(j.Dir, fmt.Sprintf("java-%d", feature))
	tmp := target + ".tmp"
	os.RemoveAll(tmp)
	if err := archivepkg.Extract(archive, tmp, archivepkg.Options{Strip: 1}); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("extract Java %d: %w", feature, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	p, ok := j.Path(feature)
	if !ok {
		return "", fmt.Errorf("Java %d archive did not contain bin/java", feature)
	}
	logf("Java %d ready", feature)
	return p, nil
}

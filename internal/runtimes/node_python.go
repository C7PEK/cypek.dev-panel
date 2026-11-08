package runtimes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	archivepkg "github.com/cypek019/cypek.dev---panel/internal/archive"
	"github.com/cypek019/cypek.dev---panel/internal/providers"
)

// Node manages Node.js installs under <Dir>/node-<major>/bin/{node,npm}.
type Node struct {
	Dir      string
	Provider *providers.Node
	mu       sync.Mutex
}

// Ensure returns the node binary for a major version (or "lts").
func (n *Node) Ensure(ctx context.Context, selector string, logf Logf, progress providers.Progress) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if selector != "" && selector != "lts" {
		if p := filepath.Join(n.Dir, "node-"+strings.TrimPrefix(selector, "v"), "bin", "node"); executable(p) {
			logf("Node.js %s already installed", selector)
			return p, nil
		}
	}
	build, err := n.Provider.Latest(ctx, selector)
	if err != nil {
		return "", err
	}
	target := filepath.Join(n.Dir, fmt.Sprintf("node-%d", build.Major))
	if p := filepath.Join(target, "bin", "node"); executable(p) {
		logf("Node.js %d already installed (%s)", build.Major, p)
		return p, nil
	}
	logf("Downloading Node.js %s", build.Version)
	return installTarball(ctx, build.URL, build.SHA256, target, n.Dir, "node", logf, progress)
}

// Python manages CPython installs under <Dir>/python-<minor>/bin/python3.
type Python struct {
	Dir      string
	Provider *providers.Python
	mu       sync.Mutex
}

// Ensure returns the python3 binary for a minor line like "3.12".
func (p *Python) Ensure(ctx context.Context, minor string, logf Logf, progress providers.Progress) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if minor == "" {
		minor = "3.12"
	}
	target := filepath.Join(p.Dir, "python-"+minor)
	if bin := filepath.Join(target, "bin", "python3"); executable(bin) {
		logf("Python %s already installed (%s)", minor, bin)
		return bin, nil
	}
	build, err := p.Provider.Latest(ctx, minor)
	if err != nil {
		return "", err
	}
	logf("Downloading Python %s", build.Version)
	if build.SHA256 == "" {
		logf("Note: no checksum published for this build")
	}
	return installTarball(ctx, build.URL, build.SHA256, target, p.Dir, "python3", logf, progress)
}

// installTarball downloads, verifies and extracts a runtime whose archive
// has one top-level folder, then checks bin/<binary> exists.
func installTarball(ctx context.Context, url, sha256, target, dir, binary string, logf Logf, progress providers.Progress) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	archive := filepath.Join(dir, "."+filepath.Base(target)+filepath.Ext(url))
	if strings.HasSuffix(url, ".tar.xz") {
		archive = filepath.Join(dir, "."+filepath.Base(target)+".tar.xz")
	} else if strings.HasSuffix(url, ".tar.gz") {
		archive = filepath.Join(dir, "."+filepath.Base(target)+".tar.gz")
	}
	defer os.Remove(archive)
	if err := providers.Download(ctx, url, archive, sha256, progress); err != nil {
		return "", err
	}
	if sha256 != "" {
		logf("Checksum OK, extracting")
	} else {
		logf("Extracting")
	}
	tmp := target + ".tmp"
	os.RemoveAll(tmp)
	if err := archivepkg.Extract(archive, tmp, archivepkg.Options{Strip: 1}); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("extract %s: %w", filepath.Base(target), err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	bin := filepath.Join(target, "bin", binary)
	if !executable(bin) {
		return "", fmt.Errorf("%s archive did not contain bin/%s", filepath.Base(target), binary)
	}
	logf("%s ready", filepath.Base(target))
	return bin, nil
}

func executable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode()&0o111 != 0 && !st.IsDir()
}

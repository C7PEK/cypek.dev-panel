package providers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
)

// Python resolves portable CPython builds from astral-sh/python-build-standalone.
// Verified 2026-11-08 against the GitHub releases API:
//
//	GET https://api.github.com/repos/astral-sh/python-build-standalone/releases/latest
//	-> {"tag_name":"20261026","assets":[{"name":"cpython-3.12.16+20261026-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz","browser_download_url":...}, ..., {"name":"SHA256SUMS"}]}
//
// Debian/Ubuntu ship python3 without ensurepip (python3-venv is a separate
// package), so a self-contained build is more predictable for venvs.
type Python struct {
	BaseURL string // GitHub API base
}

// NewPython returns a client for the GitHub releases API.
func NewPython() *Python {
	return &Python{BaseURL: "https://api.github.com/repos/astral-sh/python-build-standalone"}
}

// PythonBuild is a downloadable CPython tarball.
type PythonBuild struct {
	Version string // 3.12.16
	Minor   string // 3.12
	URL     string
	SHA256  string
}

func pythonArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	}
	return "", fmt.Errorf("no CPython builds for %s", runtime.GOARCH)
}

// Latest returns the newest install-only build for a minor line like "3.12".
func (p *Python) Latest(ctx context.Context, minor string) (*PythonBuild, error) {
	arch, err := pythonArch()
	if err != nil {
		return nil, err
	}
	if minor == "" {
		minor = "3.12"
	}
	var rel struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := getJSON(ctx, p.BaseURL+"/releases/latest", &rel, decodeJSON); err != nil {
		return nil, fmt.Errorf("python-build-standalone release: %w", err)
	}
	suffix := "-" + arch + "-unknown-linux-gnu-install_only_stripped.tar.gz"
	prefix := "cpython-" + minor + "."
	var name, url, sumsURL string
	for _, a := range rel.Assets {
		if a.Name == "SHA256SUMS" {
			sumsURL = a.URL
		}
		if strings.HasPrefix(a.Name, prefix) && strings.HasSuffix(a.Name, suffix) {
			name, url = a.Name, a.URL
		}
	}
	if name == "" {
		return nil, fmt.Errorf("python: no %s build for linux/%s in release %s", minor, arch, rel.Tag)
	}
	sum := ""
	if sumsURL != "" {
		sum, err = fetchSum(ctx, sumsURL, name)
		if err != nil {
			return nil, err
		}
	}
	version := strings.TrimPrefix(strings.SplitN(name, "+", 2)[0], "cpython-")
	return &PythonBuild{Version: version, Minor: minor, URL: url, SHA256: sum}, nil
}

func fetchSum(ctx context.Context, sumsURL, file string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := DownloadClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("python checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("python checksums: HTTP %s", resp.Status)
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4<<20))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == file {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("python: %s not in SHA256SUMS", file)
}

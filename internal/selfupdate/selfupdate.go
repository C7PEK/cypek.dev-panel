// Package selfupdate fetches panel releases from GitHub, verifies them
// against the published SHA256SUMS and swaps the binary atomically. It is
// used by `homepanel update` (run as root; the panel user cannot write
// /usr/local/bin) and by the UI's "update available" check.
package selfupdate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
)

// Repo is the GitHub repository releases are published to.
const Repo = "C7PEK/cypek.dev-panel"

// Release describes the newest published version.
type Release struct {
	Tag         string `json:"tag"`     // v1.2.3
	Version     string `json:"version"` // 1.2.3
	Notes       string `json:"notes"`
	PublishedAt string `json:"publishedAt"`
	BinaryURL   string `json:"binaryUrl"`
	SumsURL     string `json:"sumsUrl"`
	AssetName   string `json:"assetName"`
}

// Client talks to the GitHub releases API.
type Client struct {
	APIBase string
	HTTP    *http.Client
}

// New returns a client for api.github.com.
func New() *Client {
	return &Client{APIBase: "https://api.github.com/repos/" + Repo, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// AssetName is the release asset for this platform.
func AssetName() string { return "homepanel-linux-" + runtime.GOARCH }

// Latest fetches the newest non-prerelease release.
func (c *Client) Latest(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIBase+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", providers.UserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("no releases published yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases: HTTP %s", resp.Status)
	}
	var rel struct {
		Tag         string `json:"tag_name"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	out := &Release{Tag: rel.Tag, Version: strings.TrimPrefix(rel.Tag, "v"), Notes: rel.Body, PublishedAt: rel.PublishedAt, AssetName: AssetName()}
	for _, a := range rel.Assets {
		switch a.Name {
		case AssetName():
			out.BinaryURL = a.URL
		case "SHA256SUMS":
			out.SumsURL = a.URL
		}
	}
	if out.BinaryURL == "" || out.SumsURL == "" {
		return nil, fmt.Errorf("release %s has no %s or SHA256SUMS asset", rel.Tag, AssetName())
	}
	return out, nil
}

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// Newer reports whether candidate is a higher semantic version than current.
// Dev builds (git hashes) are never considered up to date.
func Newer(current, candidate string) bool {
	cm := semverRe.FindStringSubmatch(candidate)
	if cm == nil {
		return false
	}
	cu := semverRe.FindStringSubmatch(current)
	if cu == nil {
		return true
	}
	for i := 1; i <= 3; i++ {
		a, _ := strconv.Atoi(cu[i])
		b, _ := strconv.Atoi(cm[i])
		if b != a {
			return b > a
		}
	}
	return false
}

// Apply downloads the release binary, verifies it and replaces target.
// The old binary is kept as target.bak until the next successful update.
func (c *Client) Apply(ctx context.Context, rel *Release, target string, logf func(string, ...any)) error {
	sum, err := c.checksum(ctx, rel.SumsURL, rel.AssetName)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	tmp := filepath.Join(dir, "."+filepath.Base(target)+".new")
	logf("Downloading %s %s", rel.AssetName, rel.Tag)
	if err := providers.Download(ctx, rel.BinaryURL, tmp, sum, nil); err != nil {
		return err
	}
	logf("Checksum OK")
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	bak := target + ".bak"
	_ = os.Remove(bak)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, bak); err != nil {
			return fmt.Errorf("keep old binary: %w", err)
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Rename(bak, target)
		return fmt.Errorf("install new binary: %w", err)
	}
	logf("Installed %s", target)
	return nil
}

func (c *Client) checksum(ctx context.Context, sumsURL, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", providers.UserAgent)
	resp, err := providers.DownloadClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download SHA256SUMS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download SHA256SUMS: HTTP %s", resp.Status)
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("%s not listed in SHA256SUMS", name)
}

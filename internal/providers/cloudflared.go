package providers

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"runtime"
	"strings"
)

type Cloudflared struct {
	ReleasesURL string
	Arch        string
}

func NewCloudflared() *Cloudflared {
	return &Cloudflared{ReleasesURL: "https://github.com/cloudflare/cloudflared/releases"}
}

type CloudflaredRelease struct {
	Version string
	URL     string
	SHA256  string
}

var (
	cloudflaredTagRe = regexp.MustCompile(`^\d{4}\.\d{1,2}\.\d{1,3}$`)
	htmlTagRe        = regexp.MustCompile(`<[^>]+>`)
)

func (c *Cloudflared) arch() (string, error) {
	a := c.Arch
	if a == "" {
		a = runtime.GOARCH
	}
	switch a {
	case "amd64", "arm64":
		return a, nil
	}
	return "", fmt.Errorf("cloudflared: no Linux build for %s", a)
}

func (c *Cloudflared) latestTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.ReleasesURL+"/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	noRedirect := &http.Client{Transport: Client.Transport, Timeout: Client.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", fmt.Errorf("cloudflared latest release: %w", err)
	}
	resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", fmt.Errorf("cloudflared latest release: unexpected answer %s", resp.Status)
	}
	tag := path.Base(loc.Path)
	if !cloudflaredTagRe.MatchString(tag) {
		return "", fmt.Errorf("cloudflared latest release: unexpected version %q", tag)
	}
	return tag, nil
}

func (c *Cloudflared) Resolve(ctx context.Context, version string) (*CloudflaredRelease, error) {
	arch, err := c.arch()
	if err != nil {
		return nil, err
	}
	if version == "" || version == "latest" {
		if version, err = c.latestTag(ctx); err != nil {
			return nil, err
		}
	} else if !cloudflaredTagRe.MatchString(version) {
		return nil, fmt.Errorf("cloudflared: %q is not a release version", version)
	}
	name := "cloudflared-linux-" + arch
	rel := &CloudflaredRelease{Version: version, URL: c.ReleasesURL + "/download/" + version + "/" + name}
	if page, err := getText(ctx, c.ReleasesURL+"/tag/"+version, 8<<20); err == nil {
		rel.SHA256 = releaseNoteSum(page, name)
	}
	return rel, nil
}

func releaseNoteSum(page, name string) string {
	text := html.UnescapeString(htmlTagRe.ReplaceAllString(page, " "))
	re := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(name) + `:\s*([0-9a-f]{64})\b`)
	if m := re.FindStringSubmatch(text); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

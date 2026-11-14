package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type FiveM struct {
	BaseURL string

	mu      sync.Mutex
	cached  *fivemVersions
	fetched time.Time
}

func NewFiveM() *FiveM {
	return &FiveM{BaseURL: "https://changelogs-live.fivem.net/api/changelog/versions/linux/server"}
}

type fivemVersions struct {
	Recommended         string            `json:"recommended"`
	Latest              string            `json:"latest"`
	RecommendedDownload string            `json:"recommended_download"`
	LatestDownload      string            `json:"latest_download"`
	RecommendedTxAdmin  string            `json:"recommended_txadmin"`
	LatestTxAdmin       string            `json:"latest_txadmin"`
	SupportPolicy       map[string]string `json:"support_policy"`
}

type FiveMArtifact struct {
	Build       int
	URL         string
	TxAdmin     string
	Channel     string
	SupportedTo string
}

type FiveMOption struct {
	Value string
	Label string
}

func (f *FiveM) CheckArch() error {
	if runtime.GOARCH != "amd64" {
		return fmt.Errorf("FiveM publishes Linux server builds for x86_64 only; this host is %s", runtime.GOARCH)
	}
	return nil
}

func (f *FiveM) versions(ctx context.Context) (*fivemVersions, error) {
	f.mu.Lock()
	if f.cached != nil && time.Since(f.fetched) < 10*time.Minute {
		v := f.cached
		f.mu.Unlock()
		return v, nil
	}
	f.mu.Unlock()
	var v fivemVersions
	if err := getJSON(ctx, f.BaseURL, &v, func(r io.Reader, into any) error { return json.NewDecoder(r).Decode(into) }); err != nil {
		return nil, fmt.Errorf("fivem changelog: %w", err)
	}
	if v.RecommendedDownload == "" || v.LatestDownload == "" {
		return nil, fmt.Errorf("fivem changelog: response has no download URLs")
	}
	f.mu.Lock()
	f.cached, f.fetched = &v, time.Now()
	f.mu.Unlock()
	return &v, nil
}

func (f *FiveM) Options(ctx context.Context) ([]FiveMOption, error) {
	v, err := f.versions(ctx)
	if err != nil {
		return nil, err
	}
	opts := []FiveMOption{
		{Value: "recommended", Label: fmt.Sprintf("recommended (%s, txAdmin %s)", v.Recommended, v.RecommendedTxAdmin)},
	}
	if v.Latest != v.Recommended {
		opts = append(opts, FiveMOption{Value: "latest", Label: fmt.Sprintf("latest (%s, txAdmin %s)", v.Latest, v.LatestTxAdmin)})
	}
	builds := make([]int, 0, len(v.SupportPolicy))
	for b := range v.SupportPolicy {
		if n, err := strconv.Atoi(b); err == nil {
			builds = append(builds, n)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(builds)))
	for _, b := range builds {
		opts = append(opts, FiveMOption{Value: strconv.Itoa(b), Label: strconv.Itoa(b)})
	}
	return opts, nil
}

func (f *FiveM) Resolve(ctx context.Context, selector string) (*FiveMArtifact, error) {
	if err := f.CheckArch(); err != nil {
		return nil, err
	}
	v, err := f.versions(ctx)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(selector)) {
	case "", "recommended":
		b, _ := strconv.Atoi(v.Recommended)
		return &FiveMArtifact{Build: b, URL: v.RecommendedDownload, TxAdmin: v.RecommendedTxAdmin, Channel: "recommended", SupportedTo: v.SupportPolicy[v.Recommended]}, nil
	case "latest":
		b, _ := strconv.Atoi(v.Latest)
		return &FiveMArtifact{Build: b, URL: v.LatestDownload, TxAdmin: v.LatestTxAdmin, Channel: "latest", SupportedTo: v.SupportPolicy[v.Latest]}, nil
	}
	build, err := strconv.Atoi(selector)
	if err != nil {
		return nil, fmt.Errorf("fivem: %q is not a build number, recommended or latest", selector)
	}

	for _, cand := range []struct{ b, url, tx string }{{v.Recommended, v.RecommendedDownload, v.RecommendedTxAdmin}, {v.Latest, v.LatestDownload, v.LatestTxAdmin}} {
		if cand.b == strconv.Itoa(build) {
			return &FiveMArtifact{Build: build, URL: cand.url, TxAdmin: cand.tx, Channel: "pinned", SupportedTo: v.SupportPolicy[cand.b]}, nil
		}
	}
	return nil, fmt.Errorf("fivem: build %d cannot be downloaded through the changelog API; choose recommended or latest", build)
}

type FiveMEnhanced struct {
	PageURL string

	mu      sync.Mutex
	cached  *FiveMArtifact
	fetched time.Time
}

func NewFiveMEnhanced() *FiveMEnhanced {
	return &FiveMEnhanced{PageURL: "https://docs.fivem.net/docs/server-download/"}
}

var enhancedBuildRe = regexp.MustCompile(`(?i)build\s+(\d+)`)

func (f *FiveMEnhanced) Resolve(ctx context.Context) (*FiveMArtifact, error) {
	f.mu.Lock()
	if f.cached != nil && time.Since(f.fetched) < 10*time.Minute {
		a := *f.cached
		f.mu.Unlock()
		return &a, nil
	}
	f.mu.Unlock()
	if err := (&FiveM{}).CheckArch(); err != nil {
		return nil, err
	}
	page, err := getText(ctx, f.PageURL, 8<<20)
	if err != nil {
		return nil, fmt.Errorf("cfx server download page: %w", err)
	}
	art, err := parseEnhancedDownloads(page)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.cached, f.fetched = art, time.Now()
	f.mu.Unlock()
	a := *art
	return &a, nil
}

func parseEnhancedDownloads(page string) (*FiveMArtifact, error) {
	const marker = `"enhanced":`
	i := strings.Index(page, marker)
	if i < 0 {
		return nil, errors.New("cfx server download page: no GTA V Enhanced downloads listed (the page layout may have changed)")
	}
	var data struct {
		Linux []struct {
			DisplayName string `json:"displayName"`
			Subtitle    string `json:"subtitle"`
			DownloadURL string `json:"downloadURL"`
		} `json:"linux"`
	}
	if err := json.NewDecoder(strings.NewReader(page[i+len(marker):])).Decode(&data); err != nil {
		return nil, fmt.Errorf("cfx server download page: %w", err)
	}
	for _, d := range data.Linux {
		u, err := url.Parse(d.DownloadURL)
		if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Path, ".tar.xz") {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if !strings.HasSuffix(host, ".akamaized.net") && !strings.HasSuffix(host, ".fivem.net") && !strings.HasSuffix(host, ".cfx.re") {
			continue
		}
		build := 0
		if m := enhancedBuildRe.FindStringSubmatch(d.Subtitle); m != nil {
			build, _ = strconv.Atoi(m[1])
		}
		return &FiveMArtifact{Build: build, URL: d.DownloadURL, Channel: "enhanced"}, nil
	}
	return nil, errors.New("cfx server download page: no Linux download for GTA V Enhanced")
}

package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

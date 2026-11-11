package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Paper struct {
	BaseURL string
	Project string

	mu       sync.Mutex
	versions []string
	fetched  time.Time
	siblings map[string]*Paper
}

func (p *Paper) ForProject(name string) *Paper {
	if name == p.Project {
		return p
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.siblings == nil {
		p.siblings = map[string]*Paper{}
	}
	if s, ok := p.siblings[name]; ok {
		return s
	}
	s := &Paper{BaseURL: p.BaseURL, Project: name}
	p.siblings[name] = s
	return s
}

func NewPaper() *Paper {
	return &Paper{BaseURL: "https://fill.papermc.io/v3", Project: "paper"}
}

type PaperVersionInfo struct {
	Version      string
	JavaMinimum  int
	JVMFlags     []string
	LatestBuild  int
	DownloadURL  string
	DownloadName string
	SHA256       string
}

func (p *Paper) Versions(ctx context.Context) ([]string, error) {
	p.mu.Lock()
	if time.Since(p.fetched) < 10*time.Minute && p.versions != nil {
		v := p.versions
		p.mu.Unlock()
		return v, nil
	}
	p.mu.Unlock()

	var resp struct {
		Versions map[string][]string `json:"versions"`
	}
	if err := getJSON(ctx, p.BaseURL+"/projects/"+p.Project, &resp, decodeJSON); err != nil {
		return nil, err
	}
	families := make([]string, 0, len(resp.Versions))
	for f := range resp.Versions {
		families = append(families, f)
	}
	sort.Slice(families, func(i, j int) bool { return compareVersions(families[i], families[j]) > 0 })
	var out []string
	for _, f := range families {
		vs := append([]string(nil), resp.Versions[f]...)
		sort.SliceStable(vs, func(i, j int) bool { return compareVersions(vs[i], vs[j]) > 0 })
		out = append(out, vs...)
	}
	p.mu.Lock()
	p.versions, p.fetched = out, time.Now()
	p.mu.Unlock()
	return out, nil
}

func StableVersions(all []string) []string {
	var out []string
	for _, v := range all {
		if IsPrerelease(v) {
			continue
		}
		out = append(out, v)
	}
	return out
}

func IsPrerelease(v string) bool {
	l := strings.ToLower(v)
	return strings.Contains(l, "-pre") || strings.Contains(l, "-rc") || strings.Contains(l, "snapshot") || strings.Contains(l, "-beta") || strings.Contains(l, "-alpha")
}

func (p *Paper) Resolve(ctx context.Context, version string) (*PaperVersionInfo, error) {
	if version == "" || version == "latest" {
		all, err := p.Versions(ctx)
		if err != nil {
			return nil, err
		}
		stable := StableVersions(all)
		if len(stable) == 0 {
			return nil, fmt.Errorf("%s: no stable versions published", p.Project)
		}
		version = stable[0]
	}
	base := p.BaseURL + "/projects/" + p.Project + "/versions/" + url.PathEscape(version)
	var meta struct {
		Version struct {
			Java struct {
				Version struct {
					Minimum int `json:"minimum"`
				} `json:"version"`
				Flags struct {
					Recommended []string `json:"recommended"`
				} `json:"flags"`
			} `json:"java"`
		} `json:"version"`
	}
	if err := getJSON(ctx, base, &meta, decodeJSON); err != nil {
		return nil, fmt.Errorf("paper version %s: %w", version, err)
	}
	var builds []struct {
		ID        int    `json:"id"`
		Channel   string `json:"channel"`
		Downloads map[string]struct {
			Name      string            `json:"name"`
			Checksums map[string]string `json:"checksums"`
			URL       string            `json:"url"`
		} `json:"downloads"`
	}
	if err := getJSON(ctx, base+"/builds", &builds, decodeJSON); err != nil {
		return nil, fmt.Errorf("paper builds for %s: %w", version, err)
	}
	if len(builds) == 0 {
		return nil, fmt.Errorf("paper: version %s has no builds", version)
	}

	sort.SliceStable(builds, func(i, j int) bool { return builds[i].ID > builds[j].ID })
	chosen := builds[0]
	for _, b := range builds {
		if b.Channel == "STABLE" {
			chosen = b
			break
		}
	}
	dl, ok := chosen.Downloads["server:default"]
	if !ok || dl.URL == "" {
		return nil, fmt.Errorf("paper: build %d of %s has no server download", chosen.ID, version)
	}
	info := &PaperVersionInfo{
		Version:      version,
		JavaMinimum:  meta.Version.Java.Version.Minimum,
		JVMFlags:     meta.Version.Java.Flags.Recommended,
		LatestBuild:  chosen.ID,
		DownloadURL:  dl.URL,
		DownloadName: dl.Name,
		SHA256:       dl.Checksums["sha256"],
	}
	if info.JavaMinimum == 0 {
		info.JavaMinimum = fallbackJavaFor(version)
	}
	return info, nil
}

func fallbackJavaFor(version string) int {
	parts := strings.SplitN(version, ".", 3)
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(strings.SplitN(parts[1], "-", 2)[0])
	}
	switch {
	case major >= 26:
		return 25
	case major == 1 && minor >= 21:
		return 21
	case major == 1 && minor >= 17:
		return 17
	default:
		return 8
	}
}

func compareVersions(a, b string) int {
	ab, as := splitPre(a)
	bb, bs := splitPre(b)
	an := numbers(ab)
	bn := numbers(bb)
	for i := 0; i < len(an) || i < len(bn); i++ {
		var x, y int
		if i < len(an) {
			x = an[i]
		}
		if i < len(bn) {
			y = bn[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	switch {
	case as == "" && bs != "":
		return 1
	case as != "" && bs == "":
		return -1
	}
	return strings.Compare(as, bs)
}

func splitPre(v string) (base, pre string) {
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

func numbers(v string) []int {
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func decodeJSON(r io.Reader, into any) error { return json.NewDecoder(r).Decode(into) }

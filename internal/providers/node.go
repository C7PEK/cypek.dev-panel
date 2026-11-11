package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
)

type Node struct {
	BaseURL string
}

func NewNode() *Node { return &Node{BaseURL: "https://nodejs.org/dist"} }

type NodeBuild struct {
	Version string
	Major   int
	URL     string
	SHA256  string
}

func nodeArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x64", nil
	case "arm64":
		return "arm64", nil
	}
	return "", fmt.Errorf("no Node.js builds for %s", runtime.GOARCH)
}

func (n *Node) Latest(ctx context.Context, selector string) (*NodeBuild, error) {
	arch, err := nodeArch()
	if err != nil {
		return nil, err
	}
	var index []struct {
		Version string `json:"version"`
		LTS     any    `json:"lts"`
		Files   []string
	}
	if err := getJSON(ctx, n.BaseURL+"/index.json", &index, decodeJSON); err != nil {
		return nil, fmt.Errorf("nodejs index: %w", err)
	}
	wantMajor := 0
	if selector != "" && selector != "lts" {
		m, err := strconv.Atoi(strings.TrimPrefix(selector, "v"))
		if err != nil {
			return nil, fmt.Errorf("node version %q must be a major number or lts", selector)
		}
		wantMajor = m
	}
	for _, rel := range index {
		major, _ := strconv.Atoi(strings.TrimPrefix(strings.SplitN(rel.Version, ".", 2)[0], "v"))
		isLTS := false
		if s, ok := rel.LTS.(string); ok && s != "" {
			isLTS = true
		}
		if (wantMajor == 0 && !isLTS) || (wantMajor != 0 && major != wantMajor) {
			continue
		}
		if strings.Contains(rel.Version, "-") {
			continue
		}
		file := fmt.Sprintf("node-%s-linux-%s.tar.xz", rel.Version, arch)
		sum, err := n.checksum(ctx, rel.Version, file)
		if err != nil {
			return nil, err
		}
		return &NodeBuild{Version: rel.Version, Major: major, URL: fmt.Sprintf("%s/%s/%s", n.BaseURL, rel.Version, file), SHA256: sum}, nil
	}
	return nil, fmt.Errorf("node: no release found for %q", selector)
}

func (n *Node) checksum(ctx context.Context, version, file string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%s/SHASUMS256.txt", n.BaseURL, version), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("node checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("node checksums: HTTP %s", resp.Status)
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[1] == file {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("node: %s not in SHASUMS256.txt", file)
}

var _ = json.Marshal

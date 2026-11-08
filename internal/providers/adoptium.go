package providers

import (
	"context"
	"fmt"
	"runtime"
)

// Adoptium finds Temurin JRE builds.
// Verified 2026-11-08 against https://api.adoptium.net/v3:
//
//	GET /v3/assets/latest/{feature}/hotspot?architecture=x64&image_type=jre&os=linux&vendor=eclipse
//	-> [{"release_name": "jdk-21.0.13+9", "binary": {"package": {"link", "checksum" (sha256), "size"}}}]
//	GET /v3/info/available_releases -> {"available_lts_releases": [8, 11, 17, 21, 25], ...}
type Adoptium struct {
	BaseURL string
}

// NewAdoptium returns a client for api.adoptium.net.
func NewAdoptium() *Adoptium { return &Adoptium{BaseURL: "https://api.adoptium.net/v3"} }

// JavaBuild is a downloadable JRE.
type JavaBuild struct {
	Feature     int
	ReleaseName string
	URL         string
	SHA256      string
	Size        int64
}

// Arch maps Go's GOARCH to Adoptium's architecture names.
func Arch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x64", nil
	case "arm64":
		return "aarch64", nil
	default:
		return "", fmt.Errorf("no Temurin builds for architecture %s", runtime.GOARCH)
	}
}

// LTSReleases lists LTS feature versions, ascending.
func (a *Adoptium) LTSReleases(ctx context.Context) ([]int, error) {
	var resp struct {
		LTS []int `json:"available_lts_releases"`
	}
	if err := getJSON(ctx, a.BaseURL+"/info/available_releases", &resp, decodeJSON); err != nil {
		return nil, err
	}
	return resp.LTS, nil
}

// PickFeature chooses the smallest LTS release >= minimum, so a server that
// needs Java 21 gets 21 and one that needs 25 gets 25.
func PickFeature(minimum int, lts []int) int {
	best := 0
	for _, v := range lts {
		if v >= minimum && (best == 0 || v < best) {
			best = v
		}
	}
	if best == 0 {
		// Newer than any LTS we know: use the minimum itself.
		return minimum
	}
	return best
}

// Latest returns the newest JRE build for a feature version on this machine.
func (a *Adoptium) Latest(ctx context.Context, feature int) (*JavaBuild, error) {
	arch, err := Arch()
	if err != nil {
		return nil, err
	}
	var resp []struct {
		ReleaseName string `json:"release_name"`
		Binary      struct {
			Package struct {
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
				Size     int64  `json:"size"`
			} `json:"package"`
		} `json:"binary"`
	}
	u := fmt.Sprintf("%s/assets/latest/%d/hotspot?architecture=%s&image_type=jre&os=linux&vendor=eclipse", a.BaseURL, feature, arch)
	if err := getJSON(ctx, u, &resp, decodeJSON); err != nil {
		return nil, fmt.Errorf("adoptium: %w", err)
	}
	if len(resp) == 0 || resp[0].Binary.Package.Link == "" {
		return nil, fmt.Errorf("adoptium: no Temurin %d JRE for linux/%s", feature, arch)
	}
	return &JavaBuild{
		Feature:     feature,
		ReleaseName: resp[0].ReleaseName,
		URL:         resp[0].Binary.Package.Link,
		SHA256:      resp[0].Binary.Package.Checksum,
		Size:        resp[0].Binary.Package.Size,
	}, nil
}

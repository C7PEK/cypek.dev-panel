package providers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// Fabric resolves the Fabric server launcher (meta.fabricmc.net v2, checked
// 2026-11-09). The launcher jar has no published checksum; it loads the
// vanilla server.jar placed next to it (downloaded and verified separately).
type Fabric struct{ BaseURL string }

func NewFabric() *Fabric { return &Fabric{BaseURL: "https://meta.fabricmc.net/v2"} }

type fabricVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func newestStable(list []fabricVersion) string {
	for _, v := range list {
		if v.Stable {
			return v.Version
		}
	}
	if len(list) > 0 {
		return list[0].Version
	}
	return ""
}

// GameVersions lists the stable game versions Fabric supports, newest first.
func (f *Fabric) GameVersions(ctx context.Context) ([]string, error) {
	var list []fabricVersion
	if err := getJSON(ctx, f.BaseURL+"/versions/game", &list, decodeJSON); err != nil {
		return nil, fmt.Errorf("fabric game versions: %w", err)
	}
	var out []string
	for _, v := range list {
		if v.Stable {
			out = append(out, v.Version)
		}
	}
	return out, nil
}

// FabricServer is the launcher download for one game version.
type FabricServer struct {
	URL, Loader, Installer string
}

func (f *Fabric) Server(ctx context.Context, game string) (*FabricServer, error) {
	var loaders, installers []fabricVersion
	if err := getJSON(ctx, f.BaseURL+"/versions/loader", &loaders, decodeJSON); err != nil {
		return nil, fmt.Errorf("fabric loaders: %w", err)
	}
	if err := getJSON(ctx, f.BaseURL+"/versions/installer", &installers, decodeJSON); err != nil {
		return nil, fmt.Errorf("fabric installers: %w", err)
	}
	loader, installer := newestStable(loaders), newestStable(installers)
	if loader == "" || installer == "" {
		return nil, fmt.Errorf("fabric: no loader or installer published")
	}
	u := fmt.Sprintf("%s/versions/loader/%s/%s/%s/server/jar", f.BaseURL, url.PathEscape(game), url.PathEscape(loader), url.PathEscape(installer))
	return &FabricServer{URL: u, Loader: loader, Installer: installer}, nil
}

// Purpur resolves Purpur builds (api.purpurmc.org v2, checked 2026-11-09).
type Purpur struct{ BaseURL string }

func NewPurpur() *Purpur { return &Purpur{BaseURL: "https://api.purpurmc.org/v2/purpur"} }

// Versions lists Purpur's Minecraft versions, newest first.
func (p *Purpur) Versions(ctx context.Context) ([]string, error) {
	var out struct {
		Versions []string `json:"versions"`
	}
	if err := getJSON(ctx, p.BaseURL, &out, decodeJSON); err != nil {
		return nil, fmt.Errorf("purpur versions: %w", err)
	}
	vs := make([]string, 0, len(out.Versions))
	for i := len(out.Versions) - 1; i >= 0; i-- {
		vs = append(vs, out.Versions[i])
	}
	return vs, nil
}

type PurpurBuild struct {
	Version string
	Build   int
	MD5     string
	URL     string
}

func (p *Purpur) Latest(ctx context.Context, version string) (*PurpurBuild, error) {
	var b struct {
		Version string `json:"version"`
		Build   string `json:"build"`
		Result  string `json:"result"`
		MD5     string `json:"md5"`
	}
	base := p.BaseURL + "/" + url.PathEscape(version)
	if err := getJSON(ctx, base+"/latest", &b, decodeJSON); err != nil {
		return nil, fmt.Errorf("purpur %s: %w", version, err)
	}
	if b.Result != "" && b.Result != "SUCCESS" {
		return nil, fmt.Errorf("purpur %s: latest build did not succeed", version)
	}
	n, _ := strconv.Atoi(b.Build)
	return &PurpurBuild{Version: b.Version, Build: n, MD5: b.MD5, URL: base + "/" + url.PathEscape(b.Build) + "/download"}, nil
}

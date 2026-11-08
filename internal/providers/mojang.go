package providers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Mojang resolves vanilla server jars from the launcher version manifest.
// TODO(verify): manifest v2 layout (latest.release, versions[].url,
// downloads.server.{url,sha1}) as of 2026.
type Mojang struct{ ManifestURL string }

func NewMojang() *Mojang {
	return &Mojang{ManifestURL: "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"}
}

type MojangServer struct {
	Version string
	URL     string
	SHA1    string
}

// Resolve accepts "latest", "snapshot" or a concrete version id.
func (m *Mojang) Resolve(ctx context.Context, version string) (*MojangServer, error) {
	var manifest struct {
		Latest struct {
			Release  string `json:"release"`
			Snapshot string `json:"snapshot"`
		} `json:"latest"`
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := getJSON(ctx, m.ManifestURL, &manifest, decodeJSON); err != nil {
		return nil, fmt.Errorf("minecraft version manifest: %w", err)
	}
	switch version {
	case "", "latest":
		version = manifest.Latest.Release
	case "snapshot":
		version = manifest.Latest.Snapshot
	}
	for _, v := range manifest.Versions {
		if v.ID != version {
			continue
		}
		var meta struct {
			Downloads struct {
				Server struct {
					URL  string `json:"url"`
					SHA1 string `json:"sha1"`
				} `json:"server"`
			} `json:"downloads"`
		}
		if err := getJSON(ctx, v.URL, &meta, decodeJSON); err != nil {
			return nil, fmt.Errorf("minecraft %s metadata: %w", version, err)
		}
		if meta.Downloads.Server.URL == "" {
			return nil, fmt.Errorf("minecraft %s has no server download", version)
		}
		return &MojangServer{Version: version, URL: meta.Downloads.Server.URL, SHA1: meta.Downloads.Server.SHA1}, nil
	}
	return nil, fmt.Errorf("minecraft version %q does not exist", version)
}

// VerifySHA1 checks a file against Mojang's published SHA-1 (it is the only
// checksum the manifest offers).
func VerifySHA1(path, want string) error {
	if want == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s: got sha1 %s, want %s", path, got, want)
	}
	return nil
}

// FabricInstallerURL returns the newest stable Fabric installer jar.
// TODO(verify): meta.fabricmc.net v2 installer list fields (url, stable).
func FabricInstallerURL(ctx context.Context) (string, error) {
	var list []struct {
		URL    string `json:"url"`
		Stable bool   `json:"stable"`
	}
	if err := getJSON(ctx, "https://meta.fabricmc.net/v2/versions/installer", &list, decodeJSON); err != nil {
		return "", fmt.Errorf("fabric installer list: %w", err)
	}
	for _, i := range list {
		if i.Stable && i.URL != "" {
			return i.URL, nil
		}
	}
	if len(list) > 0 && list[0].URL != "" {
		return list[0].URL, nil
	}
	return "", fmt.Errorf("fabric: no installer published")
}

package providers

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// InstallerJar is a mod loader installer to download and run with Java.
type InstallerJar struct {
	Game    string // Minecraft version, e.g. 1.21.11 or 26.3
	Version string // loader version as its maven names it
	URL     string
	// Hash and Sum verify the download; Sum is "" when the maven publishes
	// no checksum for this artifact (old Forge builds).
	Hash func() hash.Hash
	Sum  string
}

// mavenChecksum fetches the .sha256 (then .sha1) file next to a maven
// artifact. A missing file is not an error: very old artifacts have none.
func mavenChecksum(ctx context.Context, artifactURL string) (func() hash.Hash, string) {
	for _, c := range []struct {
		ext  string
		h    func() hash.Hash
		size int
	}{{".sha256", sha256.New, 64}, {".sha1", sha1.New, 40}} {
		body, err := getText(ctx, artifactURL+c.ext, 512)
		if err != nil {
			continue
		}
		sum := strings.ToLower(strings.TrimSpace(strings.Fields(body + " ")[0]))
		if len(sum) == c.size {
			if _, err := hex.DecodeString(sum); err == nil {
				return c.h, sum
			}
		}
	}
	return sha256.New, ""
}

func getText(ctx context.Context, rawURL string, max int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: HTTP %s", rawURL, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max))
	return string(b), err
}

// cached keeps one fetched list for ten minutes (version pickers ask often).
type cached[T any] struct {
	mu   sync.Mutex
	val  T
	when time.Time
}

func (c *cached[T]) get(fetch func() (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.when.IsZero() && time.Since(c.when) < 10*time.Minute {
		return c.val, nil
	}
	v, err := fetch()
	if err != nil {
		return v, err
	}
	c.val, c.when = v, time.Now()
	return v, nil
}

// Forge resolves Minecraft Forge installers. Checked 2026-11-11:
//
//	GET files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json
//	    -> {"promos": {"1.21.11-recommended": "61.2.4", "1.21.11-latest": "61.2.5", ...}}
//	maven.minecraftforge.net/net/minecraftforge/forge/<mc>-<v>/forge-<mc>-<v>-installer.jar(.sha256|.sha1)
type Forge struct {
	PromotionsURL string
	MavenURL      string
	promos        cached[map[string]string]
}

func NewForge() *Forge {
	return &Forge{
		PromotionsURL: "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json",
		MavenURL:      "https://maven.minecraftforge.net/net/minecraftforge/forge",
	}
}

// forgeOldest is the oldest Minecraft version offered: older Forge
// installers need launcher libraries that are no longer hosted.
const forgeOldest = "1.12.2"

func (f *Forge) promotions(ctx context.Context) (map[string]string, error) {
	return f.promos.get(func() (map[string]string, error) {
		var out struct {
			Promos map[string]string `json:"promos"`
		}
		if err := getJSON(ctx, f.PromotionsURL, &out, decodeJSON); err != nil {
			return nil, fmt.Errorf("forge versions: %w", err)
		}
		return out.Promos, nil
	})
}

// GameVersions lists the Minecraft versions Forge has builds for, newest first.
func (f *Forge) GameVersions(ctx context.Context) ([]string, error) {
	promos, err := f.promotions(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for k := range promos {
		mc, _, ok := strings.Cut(k, "-")
		if !ok || seen[mc] || IsPrerelease(mc) || compareVersions(mc, forgeOldest) < 0 {
			continue
		}
		seen[mc] = true
		out = append(out, mc)
	}
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i], out[j]) > 0 })
	return out, nil
}

// Resolve picks the recommended Forge build for a Minecraft version (the
// latest when none is recommended yet). game "latest" is the newest version.
func (f *Forge) Resolve(ctx context.Context, game string) (*InstallerJar, error) {
	if game == "" || game == "latest" {
		list, err := f.GameVersions(ctx)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("forge: no versions published")
		}
		game = list[0]
	}
	promos, err := f.promotions(ctx)
	if err != nil {
		return nil, err
	}
	v := promos[game+"-recommended"]
	if v == "" {
		v = promos[game+"-latest"]
	}
	if v == "" {
		return nil, fmt.Errorf("forge: no build for Minecraft %s", game)
	}
	full := game + "-" + v
	u := f.MavenURL + "/" + url.PathEscape(full) + "/forge-" + url.PathEscape(full) + "-installer.jar"
	h, sum := mavenChecksum(ctx, u)
	return &InstallerJar{Game: game, Version: full, URL: u, Hash: h, Sum: sum}, nil
}

// NeoForge resolves NeoForge installers. Checked 2026-11-11:
//
//	GET maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge
//	    -> {"versions": ["20.2.3-beta", ..., "21.1.200", "26.3.0.102"]} oldest first
//	maven.neoforged.net/releases/net/neoforged/neoforge/<v>/neoforge-<v>-installer.jar(.sha256)
//
// Versions encode the Minecraft version: 21.1.x is 1.21.1, 21.0.x is 1.21,
// and since the year-based releases 26.1.2.x is 26.1.2 and 26.3.0.x is 26.3.
type NeoForge struct {
	BaseURL  string
	versions cached[[]string]
}

func NewNeoForge() *NeoForge { return &NeoForge{BaseURL: "https://maven.neoforged.net"} }

var neoVersionRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:\.(\d+))?(-[A-Za-z0-9.]+)?$`)

// NeoForgeGame maps a NeoForge version to its Minecraft version ("" when
// the scheme is unknown, e.g. April-fools builds).
func NeoForgeGame(v string) string {
	m := neoVersionRe.FindStringSubmatch(v)
	if m == nil {
		return ""
	}
	major, _ := strconv.Atoi(m[1])
	switch {
	case m[4] != "" && major >= 26: // 26.1.2.117 -> 26.1.2, 26.3.0.5 -> 26.3
		if m[3] == "0" {
			return m[1] + "." + m[2]
		}
		return m[1] + "." + m[2] + "." + m[3]
	case m[4] == "" && major >= 20 && major < 26: // 21.1.200 -> 1.21.1, 21.0.5 -> 1.21
		if m[2] == "0" {
			return "1." + m[1]
		}
		return "1." + m[1] + "." + m[2]
	}
	return ""
}

func (n *NeoForge) all(ctx context.Context) ([]string, error) {
	return n.versions.get(func() ([]string, error) {
		var out struct {
			Versions []string `json:"versions"`
		}
		if err := getJSON(ctx, n.BaseURL+"/api/maven/versions/releases/net/neoforged/neoforge", &out, decodeJSON); err != nil {
			return nil, fmt.Errorf("neoforge versions: %w", err)
		}
		return out.Versions, nil
	})
}

// GameVersions lists Minecraft versions with a NeoForge build, newest first.
// A version whose builds are all betas is included (new Minecraft releases
// start that way); Resolve prefers a stable build when there is one.
func (n *NeoForge) GameVersions(ctx context.Context) ([]string, error) {
	all, err := n.all(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range all {
		g := NeoForgeGame(v)
		if g == "" || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i], out[j]) > 0 })
	return out, nil
}

// Resolve picks the newest NeoForge build for a Minecraft version.
func (n *NeoForge) Resolve(ctx context.Context, game string) (*InstallerJar, error) {
	if game == "" || game == "latest" {
		list, err := n.GameVersions(ctx)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("neoforge: no versions published")
		}
		game = list[0]
	}
	all, err := n.all(ctx)
	if err != nil {
		return nil, err
	}
	var stable, every []string
	for _, v := range all {
		if NeoForgeGame(v) != game {
			continue
		}
		every = append(every, v)
		if !IsPrerelease(v) {
			stable = append(stable, v)
		}
	}
	pick := stable
	if len(pick) == 0 {
		pick = every
	}
	if len(pick) == 0 {
		return nil, fmt.Errorf("neoforge: no build for Minecraft %s", game)
	}
	sort.Slice(pick, func(i, j int) bool { return compareVersions(pick[i], pick[j]) > 0 })
	v := pick[0]
	u := n.BaseURL + "/releases/net/neoforged/neoforge/" + url.PathEscape(v) + "/neoforge-" + url.PathEscape(v) + "-installer.jar"
	h, sum := mavenChecksum(ctx, u)
	return &InstallerJar{Game: game, Version: v, URL: u, Hash: h, Sum: sum}, nil
}

// Quilt resolves the Quilt installer (meta.quiltmc.org v3, checked
// 2026-11-11). The installer itself downloads the loader and, with
// --download-server, the vanilla server.
type Quilt struct{ BaseURL string }

func NewQuilt() *Quilt { return &Quilt{BaseURL: "https://meta.quiltmc.org/v3"} }

// GameVersions lists stable Minecraft versions Quilt supports, newest first.
func (q *Quilt) GameVersions(ctx context.Context) ([]string, error) {
	var list []fabricVersion
	if err := getJSON(ctx, q.BaseURL+"/versions/game", &list, decodeJSON); err != nil {
		return nil, fmt.Errorf("quilt game versions: %w", err)
	}
	var out []string
	for _, v := range list {
		if v.Stable {
			out = append(out, v.Version)
		}
	}
	return out, nil
}

// Installer returns the newest installer jar for game ("latest" = newest).
func (q *Quilt) Installer(ctx context.Context, game string) (*InstallerJar, error) {
	if game == "" || game == "latest" {
		list, err := q.GameVersions(ctx)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("quilt: no supported Minecraft versions")
		}
		game = list[0]
	}
	var installers []struct {
		Version string `json:"version"`
		URL     string `json:"url"`
	}
	if err := getJSON(ctx, q.BaseURL+"/versions/installer", &installers, decodeJSON); err != nil {
		return nil, fmt.Errorf("quilt installers: %w", err)
	}
	if len(installers) == 0 || !strings.HasPrefix(installers[0].URL, "https://") {
		return nil, fmt.Errorf("quilt: no installer published")
	}
	h, sum := mavenChecksum(ctx, installers[0].URL)
	return &InstallerJar{Game: game, Version: installers[0].Version, URL: installers[0].URL, Hash: h, Sum: sum}, nil
}

// Bedrock resolves the Bedrock Dedicated Server zip for Linux. Checked
// 2026-11-11: GET net-secondary.web.minecraft-services.net/api/v1.0/download/links
// -> {"result": {"links": [{"downloadType": "serverBedrockLinux", "downloadUrl": "..."}]}}.
// Mojang publishes no checksum. Linux x86_64 only.
// TODO(verify): this endpoint backs minecraft.net's download page and is not documented.
type Bedrock struct{ LinksURL string }

func NewBedrock() *Bedrock {
	return &Bedrock{LinksURL: "https://net-secondary.web.minecraft-services.net/api/v1.0/download/links"}
}

var bedrockVersionRe = regexp.MustCompile(`bedrock-server-([0-9.]+)\.zip$`)

// BedrockServer is the zip for one channel.
type BedrockServer struct{ Version, URL string }

// Resolve returns the current release ("release") or preview ("preview") build.
func (b *Bedrock) Resolve(ctx context.Context, channel string) (*BedrockServer, error) {
	want := "serverBedrockLinux"
	if channel == "preview" {
		want = "serverBedrockPreviewLinux"
	}
	var out struct {
		Result struct {
			Links []struct {
				DownloadType string `json:"downloadType"`
				DownloadURL  string `json:"downloadUrl"`
			} `json:"links"`
		} `json:"result"`
	}
	if err := getJSON(ctx, b.LinksURL, &out, decodeJSON); err != nil {
		return nil, fmt.Errorf("bedrock download links: %w", err)
	}
	for _, l := range out.Result.Links {
		if l.DownloadType == want && strings.HasPrefix(l.DownloadURL, "https://") {
			v := ""
			if m := bedrockVersionRe.FindStringSubmatch(l.DownloadURL); m != nil {
				v = m[1]
			}
			return &BedrockServer{Version: v, URL: l.DownloadURL}, nil
		}
	}
	return nil, fmt.Errorf("bedrock: no %s download published", want)
}

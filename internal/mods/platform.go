// Package mods manages Minecraft plugins and mods of a server from
// Modrinth: it detects the server platform (loader + game version) from the
// server's files, lists the installed jars and installs, updates, disables
// and removes them with checksum verification.
package mods

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Kinds of add-ons.
const (
	KindPlugins = "plugins"
	KindMods    = "mods"
)

type loaderInfo struct {
	kind   string
	compat []string // loaders whose builds run on this one (Modrinth categories)
}

var loaders = map[string]loaderInfo{
	"paper":    {KindPlugins, []string{"paper", "spigot", "bukkit"}},
	"purpur":   {KindPlugins, []string{"purpur", "paper", "spigot", "bukkit"}},
	"folia":    {KindPlugins, []string{"folia"}},
	"spigot":   {KindPlugins, []string{"spigot", "bukkit"}},
	"bukkit":   {KindPlugins, []string{"bukkit"}},
	"velocity": {KindPlugins, []string{"velocity"}},
	"fabric":   {KindMods, []string{"fabric"}},
	"quilt":    {KindMods, []string{"quilt", "fabric"}},
	"forge":    {KindMods, []string{"forge"}},
	"neoforge": {KindMods, []string{"neoforge"}},
}

// KnownLoader reports whether l is a supported loader id.
func KnownLoader(l string) bool { _, ok := loaders[l]; return ok }

var gameVersionRe = regexp.MustCompile(`^\d{1,3}\.\d{1,3}(\.\d{1,3})?$`)

// ValidGameVersion accepts release versions such as 1.21.4 or 26.2.
func ValidGameVersion(v string) bool { return gameVersionRe.MatchString(v) }

// Platform is what the panel knows about a server's add-on support.
type Platform struct {
	Kind        string   `json:"kind"`   // plugins | mods | "" (unknown)
	Loader      string   `json:"loader"` // "" when unknown
	GameVersion string   `json:"gameVersion"`
	Dir         string   `json:"dir"`
	Detected    bool     `json:"detected"`
	Overridden  bool     `json:"overridden"`
	Loaders     []string `json:"loaders"`
}

// Known reports whether add-ons can be searched for and installed.
func (p Platform) Known() bool { return p.Loader != "" }

// ProjectType is the Modrinth search project type.
func (p Platform) ProjectType() string {
	if p.Kind == KindMods {
		return "mod"
	}
	return "plugin"
}

// Hints are what the panel already knows from the template.
type Hints struct {
	TemplateID  string
	GameVersion string // e.g. MC_VERSION_RESOLVED of the Paper template
}

// Detect works out the platform from the files in dir, the template hints
// and the admin's override (stored in .homepanel/mods.json).
func Detect(dir string, h Hints) Platform {
	st := loadState(dir)
	loader := detectLoader(dir, h.TemplateID)
	version := h.GameVersion
	if version == "" || !ValidGameVersion(version) {
		version = detectGameVersion(dir)
	}
	p := Platform{Loader: loader, GameVersion: version, Detected: loader != ""}
	if st.Loader != "" && KnownLoader(st.Loader) {
		p.Loader, p.Overridden = st.Loader, true
	}
	if st.GameVersion != "" && ValidGameVersion(st.GameVersion) {
		p.GameVersion, p.Overridden = st.GameVersion, true
	}
	if info, ok := loaders[p.Loader]; ok {
		p.Kind, p.Loaders = info.kind, info.compat
		p.Dir = info.kind // "plugins" or "mods" folder
	}
	if p.Loader == "velocity" {
		p.GameVersion = "" // proxies are not tied to a game version
	}
	return p
}

func exists(dir string, rel ...string) bool {
	for _, r := range rel {
		if _, err := os.Stat(filepath.Join(dir, r)); err == nil {
			return true
		}
	}
	return false
}

func globAny(dir string, patterns ...string) bool {
	for _, p := range patterns {
		if m, _ := filepath.Glob(filepath.Join(dir, p)); len(m) > 0 {
			return true
		}
	}
	return false
}

func detectLoader(dir, templateID string) string {
	switch {
	case exists(dir, "libraries/net/neoforged"):
		return "neoforge"
	case exists(dir, "libraries/net/minecraftforge"):
		return "forge"
	case exists(dir, "quilt-server-launch.jar", ".quilt"):
		return "quilt"
	case exists(dir, ".fabric", "fabric-server-launch.jar", "fabric-server-launcher.properties"):
		return "fabric"
	case exists(dir, "velocity.toml"):
		return "velocity"
	case exists(dir, "purpur.yml") || globAny(dir, "purpur*.jar"):
		return "purpur"
	case globAny(dir, "folia*.jar", "versions/*/folia-*.jar"):
		return "folia"
	case exists(dir, "config/paper-global.yml", "paper.yml") || globAny(dir, "versions/*/paper-*.jar", "paper*.jar"):
		return "paper"
	case exists(dir, "spigot.yml"):
		return "spigot"
	case exists(dir, "bukkit.yml"):
		return "bukkit"
	}
	// A fresh server has no config files yet; trust the template's name.
	id := strings.ToLower(templateID)
	for _, l := range []string{"neoforge", "forge", "quilt", "fabric", "velocity", "purpur", "folia", "paper", "spigot", "bukkit"} {
		if strings.Contains(id, l) {
			return l
		}
	}
	return ""
}

// detectGameVersion reads version.json from the server jars (vanilla and
// Paperclip jars carry it), then falls back to library folder names.
func detectGameVersion(dir string) string {
	jars, _ := filepath.Glob(filepath.Join(dir, "*.jar"))
	sort.SliceStable(jars, func(i, j int) bool {
		return filepath.Base(jars[i]) == "server.jar" && filepath.Base(jars[j]) != "server.jar"
	})
	if len(jars) > 12 {
		jars = jars[:12]
	}
	for _, j := range jars {
		if v := jarGameVersion(j); v != "" {
			return v
		}
	}
	for _, pattern := range []string{"versions/*", "libraries/net/minecraft/server/*"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		var found []string
		for _, m := range matches {
			// Forge names folders "1.20.1-20230612.114412".
			v, _, _ := strings.Cut(filepath.Base(m), "-")
			if ValidGameVersion(v) {
				found = append(found, v)
			}
		}
		if len(found) > 0 {
			sort.Slice(found, func(i, j int) bool { return versionLess(found[j], found[i]) })
			return found[0]
		}
	}
	return ""
}

func jarGameVersion(path string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "version.json" || f.UncompressedSize64 > 1<<20 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		var meta struct {
			ID string `json:"id"`
		}
		err = json.NewDecoder(io.LimitReader(rc, 1<<20)).Decode(&meta)
		rc.Close()
		if err == nil && ValidGameVersion(meta.ID) {
			return meta.ID
		}
	}
	return ""
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if len(pa[i]) != len(pb[i]) {
			return len(pa[i]) < len(pb[i])
		}
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return len(pa) < len(pb)
}

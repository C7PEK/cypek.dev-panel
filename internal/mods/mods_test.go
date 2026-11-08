package mods

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cypek019/cypek.dev---panel/internal/providers"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJar(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(dir string)
		hints  Hints
		loader string
		ver    string
		kind   string
	}{
		{"paperclip", func(d string) {
			writeJar(t, filepath.Join(d, "server.jar"), map[string]string{"version.json": `{"id": "26.2"}`})
			writeFile(t, filepath.Join(d, "versions/26.2/paper-26.2.jar"), "x")
		}, Hints{}, "paper", "26.2", KindPlugins},
		{"fresh paper template", func(d string) {}, Hints{TemplateID: "minecraft-paper", GameVersion: "1.21.4"}, "paper", "1.21.4", KindPlugins},
		{"purpur", func(d string) { writeFile(t, filepath.Join(d, "purpur.yml"), "") }, Hints{}, "purpur", "", KindPlugins},
		{"fabric", func(d string) {
			writeFile(t, filepath.Join(d, "fabric-server-launch.jar"), "not a zip")
			writeJar(t, filepath.Join(d, "server.jar"), map[string]string{"version.json": `{"id": "1.21.4"}`})
		}, Hints{}, "fabric", "1.21.4", KindMods},
		{"forge", func(d string) {
			writeFile(t, filepath.Join(d, "libraries/net/minecraftforge/forge/x"), "")
			writeFile(t, filepath.Join(d, "libraries/net/minecraft/server/1.20.1-20230612.114412/a"), "")
		}, Hints{}, "forge", "1.20.1", KindMods},
		{"neoforge", func(d string) { writeFile(t, filepath.Join(d, "libraries/net/neoforged/neoforge/x"), "") }, Hints{}, "neoforge", "", KindMods},
		{"velocity has no game version", func(d string) {
			writeFile(t, filepath.Join(d, "velocity.toml"), "")
			writeJar(t, filepath.Join(d, "server.jar"), map[string]string{"version.json": `{"id": "1.21.4"}`})
		}, Hints{}, "velocity", "", KindPlugins},
		{"unknown", func(d string) { writeFile(t, filepath.Join(d, "run.sh"), "") }, Hints{TemplateID: "egg-terraria"}, "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.setup(dir)
			p := Detect(dir, c.hints)
			if p.Loader != c.loader || p.GameVersion != c.ver || p.Kind != c.kind {
				t.Fatalf("got %+v", p)
			}
		})
	}
}

func TestPlatformOverride(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nil)
	if err := m.SetPlatform(dir, "nope", ""); err == nil {
		t.Fatal("unknown loader accepted")
	}
	if err := m.SetPlatform(dir, "fabric", "1.21; rm"); err == nil {
		t.Fatal("bad version accepted")
	}
	if err := m.SetPlatform(dir, "fabric", "1.21.4"); err != nil {
		t.Fatal(err)
	}
	p := Detect(dir, Hints{TemplateID: "minecraft-paper"})
	if p.Loader != "fabric" || p.GameVersion != "1.21.4" || !p.Overridden || p.Dir != "mods" {
		t.Fatalf("override: %+v", p)
	}
	_ = m.SetPlatform(dir, "", "")
	if p := Detect(dir, Hints{TemplateID: "minecraft-paper"}); p.Loader != "paper" || p.Overridden {
		t.Fatalf("cleared override: %+v", p)
	}
}

// fakeModrinth serves a tiny catalogue: "lib" (dependency) and "main"
// (requires lib), plus "evil" with a path-traversal file name and "bad"
// whose checksum does not match.
type fakeModrinth struct {
	srv   *httptest.Server
	files map[string][]byte
}

func hashes(b []byte) map[string]string {
	s1 := sha1.Sum(b)
	s5 := sha512.Sum512(b)
	return map[string]string{"sha1": hex.EncodeToString(s1[:]), "sha512": hex.EncodeToString(s5[:])}
}

func newFake(t *testing.T) *fakeModrinth {
	f := &fakeModrinth{files: map[string][]byte{
		"lib-1.0.jar":  []byte("lib one"),
		"main-1.0.jar": []byte("main one"),
		"main-2.0.jar": []byte("main two"),
		"bad.jar":      []byte("tampered"),
		"fab-1.0.jar":  []byte("fabric mod"),
	}}
	mux := http.NewServeMux()
	version := func(id, project, number, file string, loaders []string, deps []providers.ModrinthDependency) providers.ModrinthVersion {
		h := hashes(f.files[file])
		if file == "bad.jar" {
			h["sha512"] = strings.Repeat("0", 128)
		}
		return providers.ModrinthVersion{ID: id, ProjectID: project, VersionNumber: number, VersionType: "release", Loaders: loaders, GameVersions: []string{"1.21.4"},
			Files: []providers.ModrinthFile{{URL: f.srv.URL + "/dl/" + file, Filename: file, Primary: true, Size: int64(len(f.files[file])), Hashes: h}}, Dependencies: deps}
	}
	versions := func() map[string][]providers.ModrinthVersion {
		return map[string][]providers.ModrinthVersion{
			"lib":  {version("lib1", "lib", "1.0", "lib-1.0.jar", []string{"paper"}, nil)},
			"main": {version("main2", "main", "2.0", "main-2.0.jar", []string{"paper"}, []providers.ModrinthDependency{{ProjectID: "lib", DependencyType: "required"}, {ProjectID: "opt", DependencyType: "optional"}}), version("main1", "main", "1.0", "main-1.0.jar", []string{"paper"}, nil)},
			"fab":  {version("fab1", "fab", "1.0", "fab-1.0.jar", []string{"fabric"}, nil)},
			"evil": {{ID: "evil1", ProjectID: "evil", VersionNumber: "1", Loaders: []string{"paper"}, GameVersions: []string{"1.21.4"}, Files: []providers.ModrinthFile{{URL: "https://x/../../x.jar", Filename: "../../escape.jar", Primary: true, Hashes: map[string]string{"sha512": "00"}}}}},
			"bad":  {version("bad1", "bad", "1", "bad.jar", []string{"paper"}, nil)},
		}
	}
	mux.HandleFunc("/project/{id}/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(versions()[r.PathValue("id")])
	})
	mux.HandleFunc("/version/{id}", func(w http.ResponseWriter, r *http.Request) {
		for _, vs := range versions() {
			for _, v := range vs {
				if v.ID == r.PathValue("id") {
					json.NewEncoder(w).Encode(v)
					return
				}
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		var ids []string
		json.Unmarshal([]byte(r.URL.Query().Get("ids")), &ids)
		var out []providers.ModrinthProject
		for _, id := range ids {
			out = append(out, providers.ModrinthProject{ID: id, Slug: id, Title: strings.ToUpper(id)})
		}
		json.NewEncoder(w).Encode(out)
	})
	byHash := func(latest bool) map[string]providers.ModrinthVersion {
		out := map[string]providers.ModrinthVersion{}
		for _, vs := range versions() {
			for _, v := range vs {
				h := hashes(f.files[v.Files[0].Filename])["sha1"]
				if latest {
					out[h] = vs[0]
				} else {
					out[h] = v
				}
			}
		}
		return out
	}
	mux.HandleFunc("POST /version_files", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Hashes []string }
		json.NewDecoder(r.Body).Decode(&in)
		all, out := byHash(false), map[string]providers.ModrinthVersion{}
		for _, h := range in.Hashes {
			if v, ok := all[h]; ok {
				out[h] = v
			}
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("POST /version_files/update", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Hashes []string }
		json.NewDecoder(r.Body).Decode(&in)
		all, out := byHash(true), map[string]providers.ModrinthVersion{}
		for _, h := range in.Hashes {
			if v, ok := all[h]; ok {
				out[h] = v
			}
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/dl/{file}", func(w http.ResponseWriter, r *http.Request) { w.Write(f.files[r.PathValue("file")]) })
	f.srv = httptest.NewTLSServer(mux)
	t.Cleanup(f.srv.Close)
	oldC, oldD := providers.Client, providers.DownloadClient
	providers.Client, providers.DownloadClient = f.srv.Client(), f.srv.Client()
	t.Cleanup(func() { providers.Client, providers.DownloadClient = oldC, oldD })
	return f
}

func TestInstallUpdateToggleRemove(t *testing.T) {
	f := newFake(t)
	m := NewManager(&providers.Modrinth{BaseURL: f.srv.URL})
	ctx := context.Background()
	dir := t.TempDir()
	p := Detect(dir, Hints{TemplateID: "minecraft-paper", GameVersion: "1.21.4"})

	// Pinned old version first, to test updates; its dependency list is empty.
	res, err := m.Install(ctx, dir, p, "main", "main1")
	if err != nil || len(res.Installed) != 1 || res.Installed[0].File != "main-1.0.jar" {
		t.Fatalf("install main1: %+v %v", res, err)
	}
	// A jar dropped in by hand is identified by hash.
	writeFile(t, filepath.Join(dir, "plugins", "manual.jar"), "lib one")
	writeFile(t, filepath.Join(dir, "plugins", "custom.jar"), "something else")
	items, err := m.List(ctx, dir, p)
	if err != nil || len(items) != 3 {
		t.Fatalf("list: %+v %v", items, err)
	}
	for _, it := range items {
		switch it.File {
		case "manual.jar":
			if it.Project == nil || it.Project.ID != "lib" {
				t.Errorf("manual.jar not identified: %+v", it)
			}
		case "custom.jar":
			if it.Project != nil {
				t.Errorf("custom.jar should be unknown: %+v", it)
			}
		}
	}
	ups, err := m.Updates(ctx, dir, p)
	if err != nil || ups["main-1.0.jar"].VersionID != "main2" || len(ups) != 1 {
		t.Fatalf("updates: %+v %v", ups, err)
	}
	res, err = m.UpdateFile(ctx, dir, p, "main-1.0.jar")
	if err != nil || len(res.Installed) != 1 || res.Installed[0].File != "main-2.0.jar" || len(res.Removed) != 1 {
		t.Fatalf("update: %+v %v", res, err)
	}
	// lib was already present as manual.jar, so it is not installed twice.
	if _, err := os.Stat(filepath.Join(dir, "plugins", "lib-1.0.jar")); err == nil {
		t.Error("dependency installed although already present")
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "main-1.0.jar")); err == nil {
		t.Error("old version not removed")
	}

	// Fresh server: dependencies come along.
	dir2 := t.TempDir()
	res, err = m.Install(ctx, dir2, p, "main", "")
	if err != nil || len(res.Installed) != 2 || !res.Installed[1].Dependency || res.Installed[1].File != "lib-1.0.jar" {
		t.Fatalf("install with deps: %+v %v", res, err)
	}

	// Disable / enable / remove.
	name, err := m.SetEnabled(dir2, p, "lib-1.0.jar", false)
	if err != nil || name != "lib-1.0.jar.disabled" {
		t.Fatalf("disable: %s %v", name, err)
	}
	items, _ = m.List(ctx, dir2, p)
	for _, it := range items {
		if it.File == "lib-1.0.jar.disabled" && (it.Enabled || it.Project == nil) {
			t.Errorf("disabled entry: %+v", it)
		}
	}
	if name, err = m.SetEnabled(dir2, p, name, true); err != nil || name != "lib-1.0.jar" {
		t.Fatalf("enable: %s %v", name, err)
	}
	if err := m.Remove(dir2, p, "lib-1.0.jar"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../plugins/main-2.0.jar", "main-2.0.jar/..", "", "x.txt", "/etc/passwd", "nope.jar"} {
		if err := m.Remove(dir2, p, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("remove %q: %v", bad, err)
		}
	}
}

func TestInstallRejects(t *testing.T) {
	f := newFake(t)
	m := NewManager(&providers.Modrinth{BaseURL: f.srv.URL})
	ctx := context.Background()
	dir := t.TempDir()
	p := Detect(dir, Hints{TemplateID: "minecraft-paper", GameVersion: "1.21.4"})
	if _, err := m.Install(ctx, dir, p, "evil", ""); err == nil || !strings.Contains(err.Error(), "unexpected file name") {
		t.Errorf("evil: %v", err)
	}
	if _, err := m.Install(ctx, dir, p, "bad", ""); err == nil || !errors.Is(err, providers.ErrChecksum) {
		t.Errorf("bad checksum: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "bad.jar")); err == nil {
		t.Error("tampered file kept")
	}
	if _, err := m.Install(ctx, dir, p, "fab", ""); !errors.Is(err, ErrNoCompatible) {
		t.Errorf("fabric mod on paper: %v", err)
	}
	if _, err := m.Install(ctx, dir, p, "main", "fab1"); err == nil {
		t.Error("version of another project accepted")
	}
	if _, err := m.Install(ctx, dir, Platform{}, "main", ""); !errors.Is(err, ErrPlatformUnknown) {
		t.Errorf("unknown platform: %v", err)
	}
	// A plugins folder symlinked outside the server is refused.
	dir3 := t.TempDir()
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(dir3, "plugins"))
	if _, err := m.Install(ctx, dir3, p, "lib", ""); err == nil {
		t.Error("install through symlinked plugins dir")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("wrote outside: %v", entries)
	}
}

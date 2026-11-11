package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
	"github.com/C7PEK/cypek.dev-panel/internal/runtimes"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

// A loader install: the Forge installer is downloaded and verified, the
// Java version comes from Mojang's metadata, and Java is ready before the
// exec step that runs the installer.
func TestForgeInstallerPreparesJavaFirst(t *testing.T) {
	srv, _ := fakeUpstream(t)
	installer := []byte("pretend forge installer")
	sum := sha256.Sum256(installer)
	mux := srv.Config.Handler.(*http.ServeMux)
	mux.HandleFunc("/forge/promotions_slim.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"promos": map[string]string{
			"1.21.11-latest": "61.2.5", "1.21.11-recommended": "61.2.4", "1.20.1-latest": "47.4.29", "1.7.10-latest": "10.13.4.1614",
		}})
	})
	mux.HandleFunc("/maven/1.21.11-61.2.4/forge-1.21.11-61.2.4-installer.jar", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(installer) })
	mux.HandleFunc("/maven/1.21.11-61.2.4/forge-1.21.11-61.2.4-installer.jar.sha256", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hex.EncodeToString(sum[:])))
	})
	mux.HandleFunc("/mojang/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"latest": map[string]string{"release": "1.21.11"},
			"versions": []map[string]string{{"id": "1.21.11", "url": srv.URL + "/mojang/1.21.11.json"}}})
	})
	mux.HandleFunc("/mojang/1.21.11.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"downloads": map[string]any{"server": map[string]string{"url": srv.URL + "/x.jar", "sha1": "00"}},
			"javaVersion": map[string]int{"majorVersion": 21}})
	})

	forge := &providers.Forge{PromotionsURL: srv.URL + "/forge/promotions_slim.json", MavenURL: srv.URL + "/maven"}
	if got, err := forge.GameVersions(context.Background()); err != nil || strings.Join(got, ",") != "1.21.11,1.20.1" {
		t.Fatalf("game versions = %v, %v (1.7.10 must be left out)", got, err)
	}
	root := t.TempDir()
	m := New(Deps{
		Paper:  &providers.Paper{BaseURL: srv.URL, Project: "paper"},
		Forge:  forge,
		Mojang: &providers.Mojang{ManifestURL: srv.URL + "/mojang/manifest.json"},
		Java:   &runtimes.Java{Dir: filepath.Join(root, "runtimes"), Adoptium: &providers.Adoptium{BaseURL: srv.URL}},
		Log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	tpl, err := templates.Parse([]byte(`id: forge-test
name: Forge test
category: minecraft
variables: [{key: MC_VERSION, type: string, default: latest}]
runtime: {kind: java, version: auto}
install:
  - download: {provider: forge, version: "{{MC_VERSION}}", to: forge-installer.jar}
  - exec: {command: /bin/sh, args: ["-c", "printf '%s %s %s' \"$0\" \"$1\" \"$2\" > seen.txt", "{{JAVA}}", "{{LOADER_VERSION}}", "{{MC_VERSION_RESOLVED}}"]}
start: {command: "{{JAVA}}", args: ["@libraries/net/minecraftforge/forge/{{LOADER_VERSION}}/unix_args.txt"]}
`))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "servers", "forge0001")
	done := make(chan error, 1)
	if err := m.Start(context.Background(), "forge0001", dir, tpl, map[string]string{"MC_VERSION": "latest"}, func(_ *Result, e error) { done <- e }); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("install did not finish")
	}
	job, _ := m.Get("forge0001")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, strings.Join(job.Lines, "\n"))
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "forge-installer.jar")); string(b) != string(installer) {
		t.Error("installer not downloaded")
	}
	seen, _ := os.ReadFile(filepath.Join(dir, "seen.txt"))
	java := filepath.Join(root, "runtimes", "java-21", "bin", "java")
	if string(seen) != java+" 1.21.11-61.2.4 1.21.11" {
		t.Errorf("exec saw %q, want java path, loader and game version", seen)
	}
	log := strings.Join(job.Lines, "\n")
	if !strings.Contains(log, "Downloaded and verified forge-installer.jar") || strings.Index(log, "Preparing Java") > strings.Index(log, "Running sh") {
		t.Errorf("log order:\n%s", log)
	}

	// A wrong checksum fails the install.
	sum[0] ^= 0xff
	m2 := New(Deps{Paper: &providers.Paper{BaseURL: srv.URL, Project: "paper"}, Forge: &providers.Forge{PromotionsURL: forge.PromotionsURL, MavenURL: forge.MavenURL},
		Mojang: &providers.Mojang{ManifestURL: srv.URL + "/mojang/manifest.json"},
		Java:   &runtimes.Java{Dir: filepath.Join(root, "runtimes"), Adoptium: &providers.Adoptium{BaseURL: srv.URL}}})
	if err := m2.Start(context.Background(), "forge0002", filepath.Join(root, "servers", "forge0002"), tpl, map[string]string{"MC_VERSION": "1.21.11"}, func(_ *Result, e error) { done <- e }); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Error("tampered installer accepted")
	}
}

func TestNeoForgeGame(t *testing.T) {
	for v, want := range map[string]string{
		"21.1.200": "1.21.1", "21.0.167": "1.21", "20.4.237": "1.20.4", "20.2.3-beta": "1.20.2",
		"26.1.2.117": "26.1.2", "26.3.0.102": "26.3", "26.1.0.5-beta": "26.1",
		"0.25w14craftmine.3-beta": "", "47.1.3": "",
	} {
		if got := providers.NeoForgeGame(v); got != want {
			t.Errorf("%s -> %q, want %q", v, got, want)
		}
	}
}

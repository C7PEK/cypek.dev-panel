package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cypek019/cypek.dev---panel/internal/providers"
	"github.com/cypek019/cypek.dev---panel/internal/runtimes"
	"github.com/cypek019/cypek.dev---panel/internal/templates"
	builtin "github.com/cypek019/cypek.dev---panel/templates"
)

// fakeUpstream serves Paper and Adoptium lookalikes plus the files they point at.
func fakeUpstream(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "jdk-21/bin/java", Mode: 0o755, Typeflag: tar.TypeReg, Size: 2})
	_, _ = tw.Write([]byte("#!"))
	tw.Close()
	gz.Close()
	jre := buf.Bytes()
	jreSum := sha256.Sum256(jre)
	jar := []byte("not really a jar")
	jarSum := sha256.Sum256(jar)

	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/paper", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"versions": map[string][]string{"1.21": {"1.21.11", "1.21.11-rc1"}}})
	})
	mux.HandleFunc("/projects/paper/versions/1.21.11", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": map[string]any{"java": map[string]any{
			"version": map[string]int{"minimum": 21}, "flags": map[string][]string{"recommended": {"-XX:+UseG1GC"}}}}})
	})
	mux.HandleFunc("/projects/paper/versions/1.21.11/builds", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 5, "channel": "STABLE", "downloads": map[string]any{"server:default": map[string]any{
			"name": "paper-1.21.11-5.jar", "url": srv.URL + "/paper.jar", "checksums": map[string]string{"sha256": hex.EncodeToString(jarSum[:])}}}}})
	})
	mux.HandleFunc("/paper.jar", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(jar) })
	mux.HandleFunc("/info/available_releases", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"available_lts_releases": []int{8, 11, 17, 21, 25}})
	})
	mux.HandleFunc("/assets/latest/21/hotspot", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"release_name": "jdk-21", "binary": map[string]any{"package": map[string]any{
			"link": srv.URL + "/jre.tar.gz", "checksum": hex.EncodeToString(jreSum[:]), "size": len(jre)}}}})
	})
	mux.HandleFunc("/jre.tar.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(jre) })
	srv = httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	oldD, oldC := providers.DownloadClient, providers.Client
	providers.DownloadClient, providers.Client = srv.Client(), srv.Client()
	t.Cleanup(func() { providers.DownloadClient, providers.Client = oldD, oldC })
	return srv, string(jar)
}

func TestInstallPaperEndToEnd(t *testing.T) {
	srv, jar := fakeUpstream(t)
	root := t.TempDir()
	m := New(Deps{
		Paper: &providers.Paper{BaseURL: srv.URL, Project: "paper"},
		Java:  &runtimes.Java{Dir: filepath.Join(root, "runtimes"), Adoptium: &providers.Adoptium{BaseURL: srv.URL}},
		Log:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	reg, err := templates.Load(builtin.Builtin, "")
	if err != nil {
		t.Fatal(err)
	}
	tpl, _ := reg.Get("minecraft-paper")
	vars, err := tpl.ValidateValues(map[string]string{"MC_VERSION": "latest", "EULA": "true", "PORT": "25570", "MOTD": "Hello=World"})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "servers", "abcd1234")
	// Pre-existing server.properties: other keys and comments must survive.
	_ = os.MkdirAll(dir, 0o750)
	_ = os.WriteFile(filepath.Join(dir, "server.properties"), []byte("#Minecraft server properties\nmax-players=20\nserver-port=25565\n"), 0o640)

	done := make(chan struct{})
	var result *Result
	var runErr error
	if err := m.Start(context.Background(), "abcd1234", dir, tpl, vars, func(r *Result, e error) {
		result, runErr = r, e
		close(done)
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background(), "abcd1234", dir, tpl, vars, func(*Result, error) {}); err != ErrInstallRunning {
		t.Errorf("second Start: %v", err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("install did not finish")
	}
	job, _ := m.Get("abcd1234")
	if runErr != nil {
		t.Fatalf("install failed: %v\n%s", runErr, strings.Join(job.Lines, "\n"))
	}
	if job.State != JobDone || job.Percent != 100 {
		t.Errorf("job = %+v", job)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "server.jar")); string(b) != jar {
		t.Error("server.jar not downloaded")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "eula.txt")); string(b) != "eula=true\n" {
		t.Errorf("eula.txt = %q", b)
	}
	props, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	ps := string(props)
	for _, want := range []string{"#Minecraft server properties\n", "max-players=20\n", "server-port=25570\n", "query.port=25570\n", "motd=Hello=World\n"} {
		if !strings.Contains(ps, want) {
			t.Errorf("server.properties missing %q:\n%s", want, ps)
		}
	}
	if strings.Count(ps, "server-port=") != 1 {
		t.Errorf("server-port duplicated:\n%s", ps)
	}
	javaPath := filepath.Join(root, "runtimes", "java-21", "bin", "java")
	if result.Run.Command != javaPath {
		t.Errorf("command = %s", result.Run.Command)
	}
	args := strings.Join(result.Run.Args, " ")
	if args != "-Xms2048M -Xmx2048M -XX:+UseG1GC -jar server.jar nogui" {
		t.Errorf("args = %s", args)
	}
	if result.Run.Stop.Command != "stop" || result.Run.ReadyRegex == "" {
		t.Errorf("run = %+v", result.Run)
	}
	if len(result.Ports) != 1 || result.Ports[0].Port != 25570 {
		t.Errorf("ports = %+v", result.Ports)
	}
	if result.Builtins["JAVA"] != javaPath || result.Builtins["MC_VERSION_RESOLVED"] != "1.21.11" || result.Builtins["JVM_FLAGS"] == "" {
		t.Errorf("builtins = %v", result.Builtins)
	}
	if _, user := result.Builtins["RAM_MB"]; user {
		t.Error("user variable leaked into builtins")
	}
}

func TestInstallRefusesWithoutConsent(t *testing.T) {
	srv, _ := fakeUpstream(t)
	root := t.TempDir()
	m := New(Deps{Paper: &providers.Paper{BaseURL: srv.URL, Project: "paper"},
		Java: &runtimes.Java{Dir: filepath.Join(root, "runtimes"), Adoptium: &providers.Adoptium{BaseURL: srv.URL}},
		Log:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))})
	reg, _ := templates.Load(builtin.Builtin, "")
	tpl, _ := reg.Get("minecraft-paper")
	// Bypass ValidateValues on purpose: the installer must enforce consent itself too.
	vars := map[string]string{"MC_VERSION": "1.21.11", "EULA": "false", "RAM_MB": "1024", "PORT": "25565", "MOTD": "x"}
	done := make(chan error, 1)
	if err := m.Start(context.Background(), "abcd1234", filepath.Join(root, "s"), tpl, vars, func(_ *Result, e error) { done <- e }); err != nil {
		t.Fatal(err)
	}
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "EULA") {
		t.Fatalf("expected consent error, got %v", err)
	}
	if job, _ := m.Get("abcd1234"); job.State != JobFailed || job.Error == "" {
		t.Errorf("job = %+v", job)
	}
	if _, err := os.Stat(filepath.Join(root, "s", "eula.txt")); err == nil {
		t.Error("eula.txt written without consent")
	}
}

func TestInsideDir(t *testing.T) {
	for _, bad := range []string{"../x", "/etc/passwd", "a/../../b"} {
		if _, err := insideDir("/srv/s", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if p, err := insideDir("/srv/s", "plugins/x.jar"); err != nil || p != "/srv/s/plugins/x.jar" {
		t.Errorf("good path: %s, %v", p, err)
	}
}

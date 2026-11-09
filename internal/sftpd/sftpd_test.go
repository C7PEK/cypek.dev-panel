package sftpd

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type fakeBackend struct {
	servers  []ServerRef
	writable map[string]bool
	mu       sync.Mutex
	audit    []string
}

func (f *fakeBackend) Authenticate(_ context.Context, username, password, _ string) (int64, string, error) {
	if username == "cypek" && password == "secret-pass" {
		return 7, "CYPEK", nil
	}
	return 0, "", errors.New("bad credentials")
}

func (f *fakeBackend) Servers(context.Context, int64) ([]ServerRef, error) { return f.servers, nil }

func (f *fakeBackend) CanWrite(_ context.Context, _ int64, id string) bool { return f.writable[id] }

func (f *fakeBackend) Audit(_ int64, _, _, action, _ string, _ map[string]any) {
	f.mu.Lock()
	f.audit = append(f.audit, action)
	f.mu.Unlock()
}

type env struct {
	addr    string
	alpha   string
	beta    string
	outside string
	backend *fakeBackend
}

func startServer(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	alpha, beta, outside := filepath.Join(root, "alpha"), filepath.Join(root, "beta"), filepath.Join(root, "outside")
	for _, d := range []string{alpha, beta, outside, filepath.Join(alpha, ".homepanel")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(alpha, "server.properties"), []byte("motd=hi\n"), 0o644)
	os.WriteFile(filepath.Join(alpha, ".homepanel", "run.json"), []byte(`{"env":{"TOKEN":"x"}}`), 0o600)
	os.WriteFile(filepath.Join(beta, "readme.txt"), []byte("beta"), 0o644)
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("nope"), 0o644)
	os.Symlink(outside, filepath.Join(alpha, "escape"))
	os.Symlink(filepath.Join(alpha, ".homepanel"), filepath.Join(alpha, "meta-link"))

	b := &fakeBackend{
		servers:  []ServerRef{{ID: "aaaa1111", Name: "Alpha MC", Dir: alpha}, {ID: "bbbb2222", Name: "Beta/Bot", Dir: beta}},
		writable: map[string]bool{"aaaa1111": true},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	srv := &Server{Listen: addr, HostKeyPath: filepath.Join(root, "hostkey"), Backend: b}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Run(ctx)
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if srv.Fingerprint() == "" || !strings.HasPrefix(srv.Fingerprint(), "SHA256:") {
		t.Fatalf("fingerprint %q", srv.Fingerprint())
	}
	return &env{addr: addr, alpha: alpha, beta: beta, outside: outside, backend: b}
}

func (e *env) dial(t *testing.T, user, pass string) (*sftp.Client, *ssh.Client, error) {
	t.Helper()
	cfg := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.Password(pass)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second}
	conn, err := ssh.Dial("tcp", e.addr, cfg)
	if err != nil {
		return nil, nil, err
	}
	c, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	t.Cleanup(func() { c.Close(); conn.Close() })
	return c, conn, nil
}

func names(t *testing.T, c *sftp.Client, dir string) []string {
	t.Helper()
	infos, err := c.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	var out []string
	for _, i := range infos {
		out = append(out, i.Name())
	}
	sort.Strings(out)
	return out
}

func TestSFTPMultiServer(t *testing.T) {
	e := startServer(t)
	if _, _, err := e.dial(t, "cypek", "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
	c, conn, err := e.dial(t, "cypek", "secret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(t, c, "/"), "|"); got != "Alpha MC (aaaa1111)|Beta_Bot (bbbb2222)" {
		t.Fatalf("root = %s", got)
	}
	alpha := "/Alpha MC (aaaa1111)"
	if got := strings.Join(names(t, c, alpha), "|"); got != "escape|meta-link|server.properties" {
		t.Fatalf("alpha listing = %s (.homepanel must be hidden)", got)
	}
	f, err := c.Open(alpha + "/server.properties")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "motd=hi\n" {
		t.Fatalf("read %q", b)
	}

	// Escapes and the meta folder are refused.
	for _, p := range []string{alpha + "/.homepanel/run.json", alpha + "/../beta/readme.txt", alpha + "/escape/secret.txt", alpha + "/meta-link/run.json", "/aaaa1111/.homepanel/run.json"} {
		if f, err := c.Open(p); err == nil {
			data, _ := io.ReadAll(f)
			f.Close()
			if !strings.Contains(p, "../beta") || string(data) != "beta" {
				t.Errorf("%s readable: %q", p, data)
			}
		}
	}
	// "../" is cleaned inside the SFTP namespace: /Alpha/../beta is the root
	// level "beta", which does not exist (folders are "Name (id)").
	if _, err := c.Open(alpha + "/../beta/readme.txt"); err == nil {
		t.Error("../ resolved to a server folder by raw name")
	}

	// Writes: allowed on alpha, refused on beta (files.read only) and at the root.
	w, err := c.Create(alpha + "/plugins.txt")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("hello"))
	w.Close()
	if b, _ := os.ReadFile(filepath.Join(e.alpha, "plugins.txt")); string(b) != "hello" {
		t.Fatalf("upload = %q", b)
	}
	if err := c.Mkdir(alpha + "/world"); err != nil {
		t.Fatal(err)
	}
	if err := c.Rename(alpha+"/plugins.txt", alpha+"/world/p.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(alpha + "/world/p.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveDirectory(alpha + "/world"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create("/Beta_Bot (bbbb2222)/x.txt"); err == nil {
		t.Error("write without files.write")
	}
	if err := c.Mkdir("/newserver"); err == nil {
		t.Error("mkdir at the root")
	}
	if err := c.Symlink("/etc/passwd", alpha+"/pw"); err == nil {
		t.Error("symlink created")
	}
	if _, err := c.Create(alpha + "/.homepanel/run.json"); err == nil {
		t.Error("wrote into .homepanel")
	}
	if _, err := c.Create(alpha + "/escape/new.txt"); err == nil {
		t.Error("wrote through a symlink to outside")
	}
	if _, err := os.Stat(filepath.Join(e.outside, "new.txt")); err == nil {
		t.Error("file created outside")
	}
	if err := c.Remove("/Alpha MC (aaaa1111)"); err == nil {
		t.Error("removed a server folder")
	}

	// No shell.
	sess, err := conn.NewSession()
	if err == nil {
		out, err := sess.CombinedOutput("id")
		if err == nil || strings.Contains(string(out), "uid=") {
			t.Errorf("exec worked: %q %v", out, err)
		}
	}
}

func TestSFTPSingleServerLogin(t *testing.T) {
	e := startServer(t)
	c, _, err := e.dial(t, "cypek.aaaa1111", "secret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(t, c, "/"), "|"); got != "escape|meta-link|server.properties" {
		t.Fatalf("single root = %s", got)
	}
	if _, _, err := e.dial(t, "cypek.zzzz9999", "secret-pass"); err == nil {
		t.Error("login to a server without access")
	}
	if _, err := c.Open("/../outside/secret.txt"); err == nil {
		t.Error("escaped the server folder")
	}
	time.Sleep(50 * time.Millisecond)
	e.backend.mu.Lock()
	defer e.backend.mu.Unlock()
	if len(e.backend.audit) == 0 || e.backend.audit[0] != "auth.sftp_login" {
		t.Errorf("audit: %v", e.backend.audit)
	}
}

func TestSplitLogin(t *testing.T) {
	cases := map[string][2]string{
		"cypek":          {"cypek", ""},
		"cypek.abc12345": {"cypek", "abc12345"},
		"jan.kowalski":   {"jan", "kowalski"},
		"a.B1":           {"a.B1", ""},
		".abc12345":      {".abc12345", ""},
	}
	for in, want := range cases {
		n, id := splitLogin(in)
		if n != want[0] || id != want[1] {
			t.Errorf("%s: %s %s", in, n, id)
		}
	}
}

func TestSFTPRemoveLinkKeepsTarget(t *testing.T) {
	e := startServer(t)
	c, _, err := e.dial(t, "cypek.aaaa1111", "secret-pass")
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(e.alpha, "world"), 0o755)
	os.WriteFile(filepath.Join(e.alpha, "world", "level.dat"), []byte("x"), 0o644)
	os.Symlink("world", filepath.Join(e.alpha, "wlink"))
	if err := c.Remove("/wlink"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.alpha, "world", "level.dat")); err != nil {
		t.Fatal("removing a link deleted its target")
	}
	if _, err := os.Lstat(filepath.Join(e.alpha, "wlink")); err == nil {
		t.Fatal("link still there")
	}
}

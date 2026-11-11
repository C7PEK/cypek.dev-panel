package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyConfigFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "settings.yml"), []byte("server:\n  port: 1\n  name: x\nother: true\n"), 0o640)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"net":{"port":1},"keep":"y"}`), 0o640)
	_ = os.WriteFile(filepath.Join(dir, "serverconfig.txt"), []byte("worldname=old\nport=7777\n#maxplayers=8\n"), 0o640)
	_ = os.WriteFile(filepath.Join(dir, "start.txt"), []byte("Port: 1\nName: a\n"), 0o640)
	tpl := &Template{ConfigFiles: []ConfigFile{
		{Path: "server.properties", Parser: "properties", Set: map[string]string{"server-port": "{{SERVER_PORT}}", "server-ip": "0.0.0.0"}},
		{Path: "settings.yml", Parser: "yaml", Set: map[string]string{"server.port": "{{SERVER_PORT}}", "server.motd": "Hi {{NAME}}"}},
		{Path: "config.json", Parser: "json", Set: map[string]string{"net.port": "{{SERVER_PORT}}", "net.host": "0.0.0.0"}},
		{Path: "serverconfig.txt", Parser: "properties", Set: map[string]string{"worldname": "{{NAME}}"}},
		{Path: "start.txt", Parser: "file", Set: map[string]string{"Port:": "Port: {{SERVER_PORT}}"}},
		{Path: "missing.yml", Parser: "yaml", Set: map[string]string{"a": "b"}},
	}}
	vars := map[string]string{"SERVER_PORT": "25570", "NAME": "World"}
	changed, err := ApplyConfigFiles(dir, tpl, vars)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, ",") != "server.properties,settings.yml,config.json,serverconfig.txt,start.txt" {
		t.Errorf("changed = %v", changed)
	}
	read := func(n string) string { b, _ := os.ReadFile(filepath.Join(dir, n)); return string(b) }
	if p := read("server.properties"); !strings.Contains(p, "server-port=25570") || !strings.Contains(p, "server-ip=0.0.0.0") {
		t.Errorf("properties = %q", p)
	}
	if y := read("settings.yml"); !strings.Contains(y, "port: 25570") || !strings.Contains(y, `motd: "Hi World"`) || !strings.Contains(y, "other: true") {
		t.Errorf("yaml = %q", y)
	}
	if j := read("config.json"); !strings.Contains(j, `"port": 25570`) || !strings.Contains(j, `"keep": "y"`) || !strings.Contains(j, `"host": "0.0.0.0"`) {
		t.Errorf("json = %q", j)
	}
	if s := read("serverconfig.txt"); !strings.HasPrefix(s, "worldname=World\nport=7777\n#maxplayers=8") {
		t.Errorf("txt = %q", s)
	}
	if s := read("start.txt"); s != "Port: 25570\nName: a\n" {
		t.Errorf("file = %q", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing.yml")); err == nil {
		t.Error("missing yaml created")
	}
	if changed, _ := ApplyConfigFiles(dir, tpl, vars); len(changed) != 0 {
		t.Errorf("second run changed %v", changed)
	}
}

func TestShellStartIsInjectionSafe(t *testing.T) {
	tpl, err := Parse([]byte("id: sh\nname: S\ncategory: other\nvariables: [{key: JAR, type: string, default: server.jar}]\nstart:\n  shell: java -Xmx{{SERVER_MEMORY}}M -jar {{JAR}} {{EXTRA...}}\n"))
	if err == nil {
		t.Fatal("unknown placeholders accepted")
	}
	tpl, err = Parse([]byte("id: sh\nname: S\ncategory: other\nvariables: [{key: JAR, type: string, default: server.jar}, {key: SERVER_MEMORY, type: number, default: '1024'}]\nstart:\n  shell: java -Xmx{{SERVER_MEMORY}}M -jar {{JAR}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := BuildRun(tpl, map[string]string{"JAR": "x.jar; rm -rf /", "SERVER_MEMORY": "1024", KeyRuntimeBin: "/rt/java-21/bin", KeyServerDir: "/srv/a"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Command != "/bin/bash" || run.Args[1] != `java -Xmx"${SERVER_MEMORY}"M -jar "${JAR}"` {
		t.Errorf("run = %+v", run)
	}
	if run.Env["JAR"] != "x.jar; rm -rf /" || !strings.HasPrefix(run.Env["PATH"], "/rt/java-21/bin:/srv/a/.venv/bin:") || run.Env["HOME"] != "/srv/a" {
		t.Errorf("env = %v", run.Env)
	}
}

func TestShellStartKeepsUnusedSecretsOut(t *testing.T) {
	tpl, err := Parse([]byte("id: sec\nname: S\ncategory: other\nvariables:\n  - {key: TOKEN, type: secret}\n  - {key: GIT_TOKEN, type: secret}\n  - {key: NAME, type: string}\ninstall: []\nstart:\n  shell: 'run --token {{TOKEN}}'\n  env: {EXTRA: \"{{GIT_TOKEN}}x\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := BuildRun(tpl, map[string]string{"TOKEN": "a", "GIT_TOKEN": "b", "NAME": "n", KeyServerDir: "/srv"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Env["TOKEN"] != "a" || run.Env["NAME"] != "n" {
		t.Errorf("used values missing: %v", run.Env)
	}
	if _, ok := run.Env["GIT_TOKEN"]; ok {
		t.Errorf("unused secret exported: %v", run.Env)
	}
	if run.Env["EXTRA"] != "bx" {
		t.Errorf("start.env still applies: %v", run.Env)
	}
}

func TestConfigWritesNeverLeaveTheServerFolder(t *testing.T) {
	base := t.TempDir()
	dir, outside := filepath.Join(base, "srv"), filepath.Join(base, "panel.db")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(outside, []byte("DB"), 0o600)
	tpl, err := Parse([]byte("id: cfgsec\nname: C\ncategory: other\nvariables:\n  - {key: PORT, type: number, default: 1}\ninstall: []\nstart: {command: /bin/true}\n" +
		"configFiles:\n  - {path: server.properties, parser: properties, set: {port: \"{{PORT}}\"}}\n  - {path: dangling.properties, parser: properties, set: {a: b}}\n" +
		"configDefaults:\n  - {files: [server.cfg], key: sv_x, line: set sv_x 1}\n"))
	if err != nil {
		t.Fatal(err)
	}
	os.Symlink(outside, filepath.Join(dir, "server.properties"))
	os.Symlink(filepath.Join(base, "new-outside"), filepath.Join(dir, "dangling.properties"))
	os.WriteFile(filepath.Join(dir, "server.cfg"), []byte("ensure x\n"), 0o640)
	os.Symlink(outside, filepath.Join(dir, "server.cfg.panel-tmp"))
	if _, err := ApplyConfigFiles(dir, tpl, map[string]string{"PORT": "25565"}); err != nil {
		t.Log(err)
	}
	if _, err := ApplyConfigDefaults(dir, tpl); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(outside); string(b) != "DB" {
		t.Fatalf("outside file modified: %q", b)
	}
	if _, err := os.Stat(filepath.Join(base, "new-outside")); err == nil {
		t.Fatal("file created through a dangling symlink")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "server.cfg")); !strings.Contains(string(b), "set sv_x 1") {
		t.Fatalf("config default not applied: %q", b)
	}
}

package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runStart(t *testing.T, tplFile, dir string, vars map[string]string) (string, string, int) {
	t.Helper()
	b, err := os.ReadFile(tplFile)
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	vals, err := tpl.ValidateValues(vars)
	if err != nil {
		t.Fatal(err)
	}
	vals[KeyServerDir] = dir
	run, _, err := BuildRun(tpl, vals)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	for _, tool := range []string{"npm", "node", "pip", "python"} {
		os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\necho \""+tool+" $* token=${DISCORD_TOKEN-unset}\" >> "+calls+"\n[ \"$1 $2\" = \"ci --no-fund\" ] || [ \"$1\" = install ] && mkdir -p node_modules\nexit 0\n"), 0o755)
	}
	cmd := exec.Command(run.Command, run.Args...)
	cmd.Dir = dir
	for k, v := range run.Env {
		if k == "PATH" {
			v = bin + ":/usr/bin:/bin"
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, _ := cmd.CombinedOutput()
	log, _ := os.ReadFile(calls)
	return string(out), string(log), cmd.ProcessState.ExitCode()
}

func TestNodeBotFromFiles(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	tpl := "../../templates/discord-node.yaml"
	dir := t.TempDir()

	out, _, code := runStart(t, tpl, dir, map[string]string{})
	if code != 78 || !strings.Contains(out, "Files tab") {
		t.Fatalf("empty folder: %d %q", code, out)
	}

	os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "index.js"), []byte(""), 0o644)
	_, calls, code := runStart(t, tpl, dir, map[string]string{})
	if code != 0 || !strings.Contains(calls, "npm ci --no-fund") || !strings.Contains(calls, "node index.js token=unset") {
		t.Fatalf("first start: %d %q", code, calls)
	}

	_, calls, _ = runStart(t, tpl, dir, map[string]string{"DISCORD_TOKEN": "abc.def.ghi"})
	if strings.Contains(calls, "npm ci") || !strings.Contains(calls, "node index.js token=abc.def.ghi") {
		t.Fatalf("second start: %q", calls)
	}

	time.Sleep(1100 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"x":1}`), 0o644)
	_, calls, _ = runStart(t, tpl, dir, map[string]string{})
	if !strings.Contains(calls, "npm ci") {
		t.Fatalf("after package.json change: %q", calls)
	}

	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts": {"start": "node index.js"}}`), 0o644)
	_, calls, _ = runStart(t, tpl, dir, map[string]string{})
	if !strings.Contains(calls, "npm start") {
		t.Fatalf("npm start: %q", calls)
	}
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "bot.js"), nil, 0o644)
	_, calls, _ = runStart(t, tpl, dir, map[string]string{"START_FILE": "src/bot.js"})
	if !strings.Contains(calls, "node src/bot.js") {
		t.Fatalf("explicit file: %q", calls)
	}
}

func TestPythonBotFromFiles(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	tpl := "../../templates/discord-python.yaml"
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".venv"), 0o755)
	out, _, code := runStart(t, tpl, dir, map[string]string{})
	if code != 78 || !strings.Contains(out, "Files tab") {
		t.Fatalf("empty folder: %d %q", code, out)
	}
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("discord.py\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.py"), []byte(""), 0o644)
	_, calls, code := runStart(t, tpl, dir, map[string]string{})
	if code != 0 || !strings.Contains(calls, "pip install --disable-pip-version-check -r requirements.txt") || !strings.Contains(calls, "python -u main.py token=unset") {
		t.Fatalf("first start: %d %q", code, calls)
	}
	_, calls, _ = runStart(t, tpl, dir, map[string]string{})
	if strings.Contains(calls, "pip install") {
		t.Fatalf("pip ran again: %q", calls)
	}
}

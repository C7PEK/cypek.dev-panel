package installer

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/cypek019/cypek.dev---panel/internal/templates"
)

// makeRepo creates a local repository with one commit on branch main.
func makeRepo(t *testing.T, files map[string]string) (string, func(map[string]string)) {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInitWithOptions(dir, &gogit.PlainInitOptions{InitOptions: gogit.InitOptions{DefaultBranch: "refs/heads/main"}})
	if err != nil {
		t.Fatal(err)
	}
	commit := func(files map[string]string) {
		wt, _ := repo.Worktree()
		for name, content := range files {
			_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750)
			_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o640)
			if _, err := wt.Add(name); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := wt.Commit("c", &gogit.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@x", When: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	commit(files)
	return "file://" + dir, commit
}

func TestGitAndExecSteps(t *testing.T) {
	allowLocalGit = true
	defer func() { allowLocalGit = false }()
	url, commit := makeRepo(t, map[string]string{"index.js": "console.log(1)", "package.json": "{}"})
	tplYAML := "id: bot\nname: B\ncategory: discord-bot\nvariables:\n  - {key: REPO_URL, type: string}\n  - {key: GIT_TOKEN, type: secret}\n" +
		"install:\n" +
		"  - git: {url: \"{{REPO_URL}}\", branch: main, token: GIT_TOKEN, to: app}\n" +
		"  - exec: {command: /bin/echo, args: [\"deps for\", \"{{REPO_URL}}\"], dir: app, ifExists: package.json}\n" +
		"  - exec: {command: /bin/echo, args: [\"never\"], dir: app, ifExists: requirements.txt}\n" +
		"  - exec: {command: /bin/false, args: [], dir: app, ifExists: nope.txt}\n" +
		"start: {command: /bin/true, args: [], dir: app}\n"
	tpl, err := templates.Parse([]byte(tplYAML))
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Log: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))})
	dir := filepath.Join(t.TempDir(), "s")
	run := func() (*Result, []string, error) {
		done := make(chan struct{})
		var res *Result
		var rerr error
		if err := m.Start(context.Background(), "bot00001", dir, tpl, map[string]string{"REPO_URL": url, "GIT_TOKEN": ""}, func(r *Result, e error) { res, rerr = r, e; close(done) }); err != nil {
			t.Fatal(err)
		}
		<-done
		job, _ := m.Get("bot00001")
		return res, job.Lines, rerr
	}
	res, lines, err := run()
	if err != nil {
		t.Fatalf("install: %v\n%s", err, strings.Join(lines, "\n"))
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "app", "index.js")); string(b) != "console.log(1)" {
		t.Errorf("clone content = %q", b)
	}
	log := strings.Join(lines, "\n")
	if !strings.Contains(log, "deps for file://") || strings.Contains(log, "never") || !strings.Contains(log, "Skipping") {
		t.Errorf("exec log:\n%s", log)
	}
	if res.Run.Dir != "app" || res.Run.Command != "/bin/true" {
		t.Errorf("run = %+v", res.Run)
	}

	// Update from git: new commit lands after a reinstall; local changes are discarded.
	commit(map[string]string{"index.js": "console.log(2)"})
	_ = os.WriteFile(filepath.Join(dir, "app", "index.js"), []byte("local edit"), 0o640)
	if _, lines, err = run(); err != nil {
		t.Fatalf("update: %v\n%s", err, strings.Join(lines, "\n"))
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "app", "index.js")); string(b) != "console.log(2)" {
		t.Errorf("after update = %q", b)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "pulling latest") {
		t.Errorf("update log:\n%s", strings.Join(lines, "\n"))
	}

	// A failing exec fails the install with its output in the log.
	tplBad, _ := templates.Parse([]byte("id: bad\nname: B\ncategory: other\nvariables: []\ninstall:\n  - exec: {command: /bin/false}\nstart: {command: /bin/true}\n"))
	done := make(chan error, 1)
	_ = m.Start(context.Background(), "bad00001", filepath.Join(t.TempDir(), "b"), tplBad, map[string]string{}, func(_ *Result, e error) { done <- e })
	if err := <-done; err == nil || !strings.Contains(err.Error(), "false failed") {
		t.Errorf("failing exec: %v", err)
	}
	// https-only when not in test mode.
	allowLocalGit = false
	done = make(chan error, 1)
	_ = m.Start(context.Background(), "bot00002", filepath.Join(t.TempDir(), "c"), tpl, map[string]string{"REPO_URL": url, "GIT_TOKEN": ""}, func(_ *Result, e error) { done <- e })
	if err := <-done; err == nil || !strings.Contains(err.Error(), "https://") {
		t.Errorf("file:// accepted outside tests: %v", err)
	}
}

func TestDiscordTemplatesParse(t *testing.T) {
	for _, f := range []string{"../../templates/discord-node.yaml", "../../templates/discord-python.yaml"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		tpl, err := templates.Parse(b)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if _, err := tpl.ValidateValues(map[string]string{"REPO_URL": "https://github.com/x/y.git", "DISCORD_TOKEN": "abc"}); err != nil {
			t.Errorf("%s defaults: %v", f, err)
		}
		if _, err := tpl.ValidateValues(map[string]string{"REPO_URL": "git@github.com:x/y.git", "DISCORD_TOKEN": "abc"}); err == nil {
			t.Errorf("%s accepted an ssh url", f)
		}
		if _, err := tpl.ValidateValues(map[string]string{"REPO_URL": "https://x/y", "DISCORD_TOKEN": ""}); err == nil {
			t.Errorf("%s accepted an empty token", f)
		}
	}
}

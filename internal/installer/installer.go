// Package installer runs a template's install steps for a server: resolves
// and downloads artifacts, prepares the runtime, writes config files and
// finally produces the run config. Steps are executed by Go code, never a
// shell. Progress is kept in memory per server for the UI to poll.
package installer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"errors"
	"fmt"
	"github.com/C7PEK/cypek.dev-panel/internal/safefs"
	"io"
	"log/slog"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/C7PEK/cypek.dev-panel/internal/archive"
	"github.com/C7PEK/cypek.dev-panel/internal/files"
	"github.com/C7PEK/cypek.dev-panel/internal/providers"
	"github.com/C7PEK/cypek.dev-panel/internal/runtimes"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

// Deps are the collaborators steps need.
type Deps struct {
	Paper  *providers.Paper
	FiveM  *providers.FiveM
	Mojang *providers.Mojang
	Fabric *providers.Fabric
	Purpur *providers.Purpur
	// Mod loaders and Bedrock; nil means the public default.
	Forge    *providers.Forge
	NeoForge *providers.NeoForge
	Quilt    *providers.Quilt
	Bedrock  *providers.Bedrock
	Java     *runtimes.Java
	Node     *runtimes.Node
	Python   *runtimes.Python
	// Scripts runs `script` steps inside the server's sandbox.
	Scripts ScriptRunner
	Log     *slog.Logger
}

// ScriptRunner executes an install script for a server in its sandbox and
// streams output lines to logf. It returns an error for a non-zero exit.
type ScriptRunner interface {
	RunScript(ctx context.Context, serverID, serverDir, script string, env map[string]string, timeout time.Duration, logf func(string, ...any)) error
}

// JobState is the lifecycle of one installation.
type JobState string

const (
	JobRunning JobState = "running"
	JobDone    JobState = "done"
	JobFailed  JobState = "failed"
)

// Job is the observable progress of an installation.
type Job struct {
	ServerID   string    `json:"serverId"`
	State      JobState  `json:"state"`
	Step       int       `json:"step"`
	Steps      int       `json:"steps"`
	StepTitle  string    `json:"stepTitle"`
	Percent    int       `json:"percent"` // of the current download, -1 when unknown
	Lines      []string  `json:"lines"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}

// Result is what a successful installation produced.
type Result struct {
	Run   shim.RunConfig
	Ports []servers.Port
	// Builtins are the resolved internal variables (JAVA, JVM_FLAGS, ...),
	// stored with the server so a reinstall is not needed to rebuild run.json.
	Builtins map[string]string
}

// Manager runs at most one job per server and remembers the last one.
type Manager struct {
	deps Deps
	mu   sync.Mutex
	jobs map[string]*Job
}

// New creates a Manager.
func New(d Deps) *Manager {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Manager{deps: d, jobs: map[string]*Job{}}
}

const maxLines = 500

// ErrInstallRunning means a job is already in progress for this server.
var ErrInstallRunning = errors.New("installation already running")

// SetScriptRunner wires the sandboxed script executor (it needs the
// supervisor, which lives in the API layer).
func (m *Manager) SetScriptRunner(r ScriptRunner) {
	m.mu.Lock()
	m.deps.Scripts = r
	m.mu.Unlock()
}

// Get returns the last job for a server.
func (m *Manager) Get(serverID string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[serverID]
	if !ok {
		return nil, false
	}
	cp := *j
	cp.Lines = append([]string(nil), j.Lines...)
	return &cp, true
}

// Running reports whether an installation is in progress for serverID.
func (m *Manager) Running(serverID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[serverID]
	return ok && j.State == JobRunning
}

// Forget drops the job record (after the server was deleted).
func (m *Manager) Forget(serverID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, serverID)
}

// Start launches the installation in the background. vars are the validated
// user variables; serverDir is the server's working directory. done is
// called exactly once from the job goroutine with the outcome.
func (m *Manager) Start(ctx context.Context, serverID, serverDir string, tpl *templates.Template, vars map[string]string, done func(*Result, error)) error {
	return m.StartWith(ctx, serverID, serverDir, tpl, vars, Hooks{}, done)
}

// Hooks customise a job: Before runs first inside the job (its log lines
// show up in the install log), e.g. stopping the server for a git update.
type Hooks struct {
	Before   func(ctx context.Context, logf func(string, ...any)) error
	Finished string // last log line on success (default "Installation finished.")
}

// StartWith is Start with hooks.
func (m *Manager) StartWith(ctx context.Context, serverID, serverDir string, tpl *templates.Template, vars map[string]string, hooks Hooks, done func(*Result, error)) error {
	m.mu.Lock()
	if j, ok := m.jobs[serverID]; ok && j.State == JobRunning {
		m.mu.Unlock()
		return ErrInstallRunning
	}
	job := &Job{ServerID: serverID, State: JobRunning, Steps: len(tpl.Install) + 2, StartedAt: time.Now(), Percent: -1}
	m.jobs[serverID] = job
	m.mu.Unlock()

	go func() {
		var (
			res *Result
			err error
		)
		if hooks.Before != nil {
			c := &stepCtx{m: m, job: job, dir: serverDir}
			err = hooks.Before(ctx, c.logf)
		}
		if err == nil {
			res, err = m.run(ctx, job, serverDir, tpl, vars)
		}
		finished := hooks.Finished
		if finished == "" {
			finished = "Installation finished."
		}
		m.mu.Lock()
		job.FinishedAt = time.Now()
		if err != nil {
			job.State = JobFailed
			job.Error = err.Error()
			job.Lines = appendLine(job.Lines, "ERROR: "+err.Error())
		} else {
			job.State = JobDone
			job.Percent = 100
			job.Lines = appendLine(job.Lines, finished)
		}
		m.mu.Unlock()
		done(res, err)
	}()
	return nil
}

func appendLine(lines []string, l string) []string {
	lines = append(lines, time.Now().Format("15:04:05")+" "+l)
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

type stepCtx struct {
	m    *Manager
	job  *Job
	dir  string
	vars map[string]string
	tpl  *templates.Template // for masking secret values in the log
}

func (c *stepCtx) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if c.tpl != nil {
		// The log is shown to console.read users and goes to the journal.
		line = maskSecrets(c.tpl, c.vars, line)
	}
	c.m.deps.Log.Info("install", "server", c.job.ServerID, "msg", line)
	c.m.mu.Lock()
	c.job.Lines = appendLine(c.job.Lines, line)
	c.m.mu.Unlock()
}

func (c *stepCtx) begin(step int, title string) {
	c.m.mu.Lock()
	c.job.Step, c.job.StepTitle, c.job.Percent = step, title, -1
	c.m.mu.Unlock()
	c.logf("[%d/%d] %s", step, c.job.Steps, title)
}

func (c *stepCtx) progress(done, total int64) {
	c.m.mu.Lock()
	if total > 0 {
		c.job.Percent = int(done * 100 / total)
	}
	c.m.mu.Unlock()
}

func (m *Manager) run(ctx context.Context, job *Job, dir string, tpl *templates.Template, userVars map[string]string) (*Result, error) {
	vars := map[string]string{}
	for k, v := range userVars {
		vars[k] = v
	}
	c := &stepCtx{m: m, job: job, dir: dir, vars: vars, tpl: tpl}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	if abs, err := filepath.Abs(dir); err == nil {
		vars[templates.KeyServerDir] = abs
	}
	// The runtime is prepared before the install steps when they may need it
	// (npm, pip, java -jar installer.jar in egg scripts). Only Java "auto"
	// waits: it depends on what the download step reports as the minimum.
	runtimeReady := runtimeBeforeSteps(tpl.Runtime)
	if runtimeReady {
		if err := c.runtime(ctx, 0, tpl.Runtime); err != nil {
			return nil, err
		}
	}
	for i, st := range tpl.Install {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Java "auto" is prepared once the download steps reported the
		// minimum, but before the first program that may need it (a mod
		// loader installer: java -jar installer.jar --installServer).
		if !runtimeReady && (st.Exec != nil || st.Script != nil) {
			if err := c.runtime(ctx, i+1, tpl.Runtime); err != nil {
				return nil, err
			}
			runtimeReady = true
		}
		var err error
		switch {
		case st.Download != nil:
			err = c.download(ctx, i+1, st.Download)
		case st.Extract != nil:
			err = c.extract(i+1, st.Extract)
		case st.Git != nil:
			err = c.git(ctx, i+1, st.Git)
		case st.Exec != nil:
			err = c.exec(ctx, i+1, st.Exec)
		case st.Script != nil:
			err = c.script(ctx, i+1, st.Script)
		case st.Write != nil:
			err = c.write(i+1, st.Write)
		case st.Properties != nil:
			err = c.properties(i+1, st.Properties)
		}
		if err != nil {
			return nil, err
		}
	}
	if !runtimeReady {
		if err := c.runtime(ctx, len(tpl.Install)+1, tpl.Runtime); err != nil {
			return nil, err
		}
	} else {
		c.begin(len(tpl.Install)+1, "Runtime ready")
	}
	c.begin(job.Steps, "Preparing start configuration")
	if changed, err := templates.ApplyConfigFiles(dir, tpl, vars); err != nil {
		c.logf("Warning: could not update config files: %v", err)
	} else {
		for _, f := range changed {
			c.logf("Updated %s", f)
		}
	}
	run, ports, err := templates.BuildRun(tpl, vars)
	if err != nil {
		return nil, fmt.Errorf("build run config: %w", err)
	}
	builtins := map[string]string{}
	for k, v := range vars {
		if _, user := userVars[k]; !user {
			builtins[k] = v
		}
	}
	c.logf("Start command: %s", maskSecrets(tpl, vars, run.Command+" "+strings.Join(run.Args, " ")))
	return &Result{Run: run, Ports: ports, Builtins: builtins}, nil
}

func (c *stepCtx) download(ctx context.Context, n int, st *templates.DownloadStep) error {
	version, err := templates.Render(st.Version, c.vars)
	if err != nil {
		return err
	}
	to, err := templates.Render(st.To, c.vars)
	if err != nil {
		return err
	}
	dest, err := insideDir(c.dir, to)
	if err != nil {
		return err
	}
	switch st.Provider {
	case "paper", "folia", "velocity":
		return c.downloadPaper(ctx, n, st.Provider, version, dest)
	case "forge", "neoforge", "quilt":
		return c.downloadLoader(ctx, n, st.Provider, version, dest)
	case "bedrock":
		c.begin(n, "Downloading the Bedrock Dedicated Server")
		bd := c.m.deps.Bedrock
		if bd == nil {
			bd = providers.NewBedrock()
		}
		srv, err := bd.Resolve(ctx, version)
		if err != nil {
			return err
		}
		c.logf("Bedrock Dedicated Server %s", srv.Version)
		c.logf("Mojang publishes no checksum for it; downloading over HTTPS only")
		// minecraft.net's CDN resets requests whose agent carries a URL.
		// TODO(verify): checked 2026-11-11; the plain product name passes.
		if err := providers.Download(providers.WithUserAgent(ctx, "homepanel/0.1"), srv.URL, dest, "", c.progress); err != nil {
			return err
		}
		c.vars["BEDROCK_VERSION"] = srv.Version
		return nil
	case "mojang":
		if version == "latest" && c.vars["MC_VERSION_RESOLVED"] != "" {
			version = c.vars["MC_VERSION_RESOLVED"] // e.g. picked by the Fabric step
		}
		c.begin(n, "Downloading the Minecraft server")
		mj := c.m.deps.Mojang
		if mj == nil {
			mj = providers.NewMojang()
		}
		srv, err := mj.Resolve(ctx, version)
		if err != nil {
			return err
		}
		c.logf("Minecraft %s, needs Java %d", srv.Version, srv.JavaMajor)
		if err := providers.DownloadVerified(ctx, srv.URL, dest, sha1.New(), srv.SHA1, 0, c.progress); err != nil {
			return err
		}
		c.logf("Downloaded and verified %s (SHA-1 from Mojang)", filepath.Base(dest))
		c.vars["MC_VERSION_RESOLVED"] = srv.Version
		if srv.JavaMajor > 0 {
			c.vars["JAVA_MIN"] = strconv.Itoa(srv.JavaMajor)
		}
		return nil
	case "fabric":
		c.begin(n, "Downloading the Fabric server launcher")
		fb := c.m.deps.Fabric
		if fb == nil {
			fb = providers.NewFabric()
		}
		if version == "latest" || version == "" {
			list, err := fb.GameVersions(ctx)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				return errors.New("fabric: no supported Minecraft versions")
			}
			version = list[0]
		}
		srv, err := fb.Server(ctx, version)
		if err != nil {
			return err
		}
		c.logf("Fabric loader %s for Minecraft %s (installer %s)", srv.Loader, version, srv.Installer)
		c.logf("Fabric publishes no checksum for the launcher; downloading over HTTPS only")
		if err := providers.Download(ctx, srv.URL, dest, "", c.progress); err != nil {
			return err
		}
		c.vars["MC_VERSION_RESOLVED"] = version
		c.vars["FABRIC_LOADER"] = srv.Loader
		return nil
	case "purpur":
		c.begin(n, "Downloading Purpur")
		pp := c.m.deps.Purpur
		if pp == nil {
			pp = providers.NewPurpur()
		}
		if version == "latest" || version == "" {
			list, err := pp.Versions(ctx)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				return errors.New("purpur: no versions published")
			}
			version = list[0]
		}
		b, err := pp.Latest(ctx, version)
		if err != nil {
			return err
		}
		c.logf("Purpur %s build %d", b.Version, b.Build)
		if err := providers.DownloadVerified(ctx, b.URL, dest, md5.New(), b.MD5, 0, c.progress); err != nil {
			return err
		}
		c.logf("Downloaded and verified %s (MD5 from Purpur)", filepath.Base(dest))
		c.vars["MC_VERSION_RESOLVED"] = b.Version
		c.vars["MC_BUILD"] = strconv.Itoa(b.Build)
		mj := c.m.deps.Mojang
		if mj == nil {
			mj = providers.NewMojang()
		}
		if srv, err := mj.Resolve(ctx, b.Version); err == nil && srv.JavaMajor > 0 {
			c.vars["JAVA_MIN"] = strconv.Itoa(srv.JavaMajor)
		}
		return nil
	case "fivem":
		c.begin(n, "Downloading FXServer")
		if c.m.deps.FiveM == nil {
			return errors.New("fivem provider not configured")
		}
		art, err := c.m.deps.FiveM.Resolve(ctx, version)
		if err != nil {
			return err
		}
		c.logf("FXServer build %d (%s channel, txAdmin %s)", art.Build, art.Channel, art.TxAdmin)
		if art.SupportedTo != "" && !strings.HasPrefix(art.SupportedTo, "9999") {
			c.logf("Note: Cfx.re supports this build until %s", art.SupportedTo)
		}
		c.logf("Cfx.re publishes no checksums; downloading over HTTPS only")
		if err := providers.Download(ctx, art.URL, dest, "", c.progress); err != nil {
			return err
		}
		c.logf("Downloaded %s", filepath.Base(dest))
		c.vars["FX_BUILD"] = strconv.Itoa(art.Build)
		c.vars["TXADMIN_VERSION"] = art.TxAdmin
		return nil
	case "url":
		rawURL, err := templates.Render(st.URL, c.vars)
		if err != nil {
			return err
		}
		c.begin(n, "Downloading "+filepath.Base(dest))
		c.logf("From %s", rawURL)
		if err := providers.Download(ctx, rawURL, dest, st.Sha256, c.progress); err != nil {
			return err
		}
		if st.Sha256 != "" {
			c.logf("Checksum OK")
		}
		return nil
	default:
		return fmt.Errorf("unknown download provider %q", st.Provider)
	}
}

func (c *stepCtx) extract(n int, st *templates.ExtractStep) error {
	from, err := templates.Render(st.From, c.vars)
	if err != nil {
		return err
	}
	to, err := templates.Render(st.To, c.vars)
	if err != nil {
		return err
	}
	src, err := insideDir(c.dir, from)
	if err != nil {
		return err
	}
	dest := c.dir
	if to != "." && to != "" {
		if dest, err = insideDir(c.dir, to); err != nil {
			return err
		}
	}
	c.begin(n, "Extracting "+filepath.Base(from))
	last := time.Now()
	progress := func(entries int) {
		if time.Since(last) > 5*time.Second {
			c.logf("... %d entries extracted", entries)
			last = time.Now()
		}
	}
	opts := archive.Options{Strip: st.Strip, Progress: progress, Resolve: files.ResolveSafe}
	if len(st.Keep) > 0 {
		opts.Keep = func(rel string) bool { return st.Keeps(rel) }
	}
	if err := archive.Extract(src, dest, opts); err != nil {
		return fmt.Errorf("extract %s: %w", from, err)
	}
	if st.Delete == nil || *st.Delete {
		_ = os.Remove(src)
	}
	c.logf("Extracted %s to %s", from, to)
	return nil
}

func (c *stepCtx) write(n int, st *templates.WriteStep) error {
	path, err := templates.Render(st.Path, c.vars)
	if err != nil {
		return err
	}
	c.begin(n, "Writing "+path)
	if st.RequireConsent != "" {
		if c.vars[st.RequireConsent] != "true" {
			return fmt.Errorf("%s must be accepted before %s can be written", st.RequireConsent, path)
		}
		c.logf("Consent %s given by the user", st.RequireConsent)
	}
	content, err := templates.Render(st.Content, c.vars)
	if err != nil {
		return err
	}
	if _, err := insideDir(c.dir, path); err != nil {
		return err
	}
	return safefs.WriteFile(c.dir, filepath.Clean(path), []byte(content), 0o640)
}

// properties sets keys in a Java .properties file, keeping other lines
// (comments included) and their order.
func (c *stepCtx) properties(n int, st *templates.PropertiesStep) error {
	path, err := templates.Render(st.Path, c.vars)
	if err != nil {
		return err
	}
	c.begin(n, "Updating "+path)
	if _, err := insideDir(c.dir, path); err != nil {
		return err
	}
	want := map[string]string{}
	for k, v := range st.Set {
		if want[k], err = templates.Render(v, c.vars); err != nil {
			return err
		}
	}
	var lines []string
	if b, err := safefs.ReadFile(c.dir, filepath.Clean(path), 16<<20); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(b))
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
	}
	seen := map[string]bool{}
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "!") {
			continue
		}
		eq := strings.IndexAny(l, "=:")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(l[:eq])
		if v, ok := want[key]; ok {
			lines[i] = key + "=" + escapeProp(v)
			seen[key] = true
		}
	}
	keys := make([]string, 0, len(want))
	for k := range want {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, k+"="+escapeProp(want[k]))
		c.logf("%s: set %s=%s", path, k, want[k])
	}
	return safefs.WriteFile(c.dir, filepath.Clean(path), []byte(strings.Join(lines, "\n")+"\n"), 0o640)
}

// escapeProp escapes what Java's Properties loader would otherwise interpret.
func escapeProp(v string) string {
	r := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "\t", "\\t")
	return r.Replace(v)
}

// runtimeBeforeSteps: node/python always, java when the version is fixed
// or chosen by a variable (imported eggs), not for "auto".
func runtimeBeforeSteps(rt templates.Runtime) bool {
	switch rt.Kind {
	case "node", "python":
		return true
	case "java":
		return rt.Version != "" && rt.Version != "auto"
	}
	return false
}

func (c *stepCtx) runtime(ctx context.Context, n int, rt templates.Runtime) error {
	if strings.Contains(rt.Version, "{{") {
		v, err := templates.Render(rt.Version, c.vars)
		if err != nil {
			return fmt.Errorf("runtime.version: %w", err)
		}
		rt.Version = strings.TrimSpace(v)
	}
	switch rt.Kind {
	case "", "none":
		c.begin(n, "No runtime needed")
		return nil
	case "java":
		c.begin(n, "Preparing Java")
		if c.m.deps.Java == nil {
			return errors.New("java runtime provider not configured")
		}
		feature := 0
		if rt.Version != "" && rt.Version != "auto" {
			f, err := strconv.Atoi(rt.Version)
			if err != nil {
				return fmt.Errorf("runtime.version %q is not a Java version", rt.Version)
			}
			feature = f
		} else {
			minimum, _ := strconv.Atoi(c.vars["JAVA_MIN"])
			if minimum == 0 {
				minimum = 21
			}
			lts, err := c.m.deps.Java.Adoptium.LTSReleases(ctx)
			if err != nil {
				c.logf("Could not list Java releases (%v); using Java %d", err, minimum)
				lts = nil
			}
			feature = providers.PickFeature(minimum, lts)
			c.logf("Server needs Java %d or newer; using Java %d", minimum, feature)
		}
		path, err := c.m.deps.Java.Ensure(ctx, feature, c.logf, c.progress)
		if err != nil {
			return err
		}
		c.vars[templates.KeyJava] = path
		c.vars[templates.KeyRuntimeBin] = filepath.Dir(path)
		c.vars[templates.KeyJavaHome] = filepath.Dir(filepath.Dir(path))
		c.vars["JAVA_FEATURE"] = strconv.Itoa(feature)
		if _, ok := c.vars[templates.KeyJVMFlags]; !ok {
			c.vars[templates.KeyJVMFlags] = "[]"
		}
		return nil
	case "node":
		c.begin(n, "Preparing Node.js")
		if c.m.deps.Node == nil {
			return errors.New("node runtime provider not configured")
		}
		sel := rt.Version
		if sel == "" || sel == "auto" {
			sel = "lts"
		}
		if v, ok := c.vars["NODE_VERSION"]; ok && v != "" {
			sel = v
		}
		node, err := c.m.deps.Node.Ensure(ctx, sel, c.logf, c.progress)
		if err != nil {
			return err
		}
		bin := filepath.Dir(node)
		c.vars[templates.KeyNode] = node
		c.vars[templates.KeyRuntimeBin] = bin
		c.vars[templates.KeyNPM] = filepath.Join(bin, "npm")
		c.vars[templates.KeyNPX] = filepath.Join(bin, "npx")
		return nil
	case "python":
		c.begin(n, "Preparing Python")
		if c.m.deps.Python == nil {
			return errors.New("python runtime provider not configured")
		}
		minor := rt.Version
		if minor == "" || minor == "auto" {
			minor = "3.12"
		}
		if v, ok := c.vars["PY_VERSION"]; ok && v != "" {
			minor = v
		}
		py, err := c.m.deps.Python.Ensure(ctx, minor, c.logf, c.progress)
		if err != nil {
			return err
		}
		venv := filepath.Join(c.dir, ".venv")
		if _, err := os.Stat(filepath.Join(venv, "bin", "python")); err != nil {
			c.logf("Creating virtual environment .venv")
			// -I: isolated mode, so files planted in the server folder
			// (venv.py, PYTHON* variables) cannot hook the interpreter.
			if err := c.runSandboxed(ctx, py, []string{"-I", "-m", "venv", venv}, c.dir, nil, 300); err != nil {
				return fmt.Errorf("create venv: %w", err)
			}
		}
		c.vars[templates.KeyPython] = ".venv/bin/python"
		c.vars[templates.KeyPip] = ".venv/bin/pip"
		c.vars[templates.KeyRuntimeBin] = filepath.Join(c.dir, ".venv", "bin")
		if abs, err := filepath.Abs(c.vars[templates.KeyRuntimeBin]); err == nil {
			c.vars[templates.KeyRuntimeBin] = abs
		}
		return nil
	default:
		return fmt.Errorf("runtime %q is not supported yet", rt.Kind)
	}
}

// git clones the repository into To, or fetches and resets to the branch
// when it already exists (that is what "Update from git" does).
func (c *stepCtx) git(ctx context.Context, n int, st *templates.GitStep) error {
	rawURL, err := templates.Render(st.URL, c.vars)
	if err != nil {
		return err
	}
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "https://") && !(allowLocalGit && strings.HasPrefix(rawURL, "file://")) {
		return fmt.Errorf("git url must start with https://")
	}
	branch, err := templates.Render(st.Branch, c.vars)
	if err != nil {
		return err
	}
	branch = strings.TrimSpace(branch)
	to, err := templates.Render(st.To, c.vars)
	if err != nil {
		return err
	}
	dest, err := insideDir(c.dir, to)
	if err != nil {
		return err
	}
	var auth transport.AuthMethod
	if st.Token != "" {
		if tok := c.vars[st.Token]; tok != "" {
			auth = &githttp.BasicAuth{Username: "x-access-token", Password: tok}
		}
	}
	c.begin(n, "Fetching code from git")
	c.logf("Repository %s (branch %s)", rawURL, orDefault(branch, "default"))
	var ref plumbing.ReferenceName
	if branch != "" {
		ref = plumbing.NewBranchReferenceName(branch)
	}
	if gst, err := os.Lstat(filepath.Join(dest, ".git")); err == nil {
		// .git is writable by the bot: a ".git" file (gitdir: ...) or link
		// could send the panel's object writes anywhere.
		if !gst.IsDir() {
			return errors.New("app/.git is not a normal folder (modified by the bot?); reinstall the server")
		}
		repo, err := gogit.PlainOpen(dest)
		if err != nil {
			return fmt.Errorf("open repository: %w", err)
		}
		// Always fetch from the URL in the server's settings, never from
		// .git/config, so the access token cannot be sent elsewhere.
		_ = repo.DeleteRemote("origin")
		if _, err := repo.CreateRemote(&gitconfig.RemoteConfig{Name: "origin", URLs: []string{rawURL}}); err != nil {
			return fmt.Errorf("set remote: %w", err)
		}
		var before plumbing.Hash
		if head, err := repo.Head(); err == nil {
			before = head.Hash()
		}
		c.logf("Repository exists, pulling latest changes")
		if err := repo.FetchContext(ctx, &gogit.FetchOptions{Auth: auth, Force: true, Depth: 0}); err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
			return fmt.Errorf("git fetch: %w", err)
		}
		wt, err := repo.Worktree()
		if err != nil {
			return err
		}
		remoteRef := plumbing.NewRemoteReferenceName("origin", branch)
		if branch == "" {
			head, err := repo.Head()
			if err != nil {
				return err
			}
			remoteRef = plumbing.NewRemoteReferenceName("origin", head.Name().Short())
		}
		target, err := repo.Reference(remoteRef, true)
		if err != nil {
			return fmt.Errorf("branch %s not found on origin: %w", branch, err)
		}
		if err := wt.Reset(&gogit.ResetOptions{Commit: target.Hash(), Mode: gogit.HardReset}); err != nil {
			return fmt.Errorf("git reset: %w", err)
		}
		after := target.Hash()
		switch {
		case after == before:
			c.logf("Already up to date (%s)", after.String()[:10])
		case before.IsZero():
			c.logf("Now at %s", describeCommit(repo, after))
		default:
			c.logf("Updated %s → %s", before.String()[:10], describeCommit(repo, after))
		}
		return nil
	}
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return err
	}
	repo, err := gogit.PlainCloneContext(ctx, dest, false, &gogit.CloneOptions{URL: rawURL, Auth: auth, ReferenceName: ref, SingleBranch: branch != "", Depth: 1})
	if err != nil {
		os.RemoveAll(filepath.Join(dest, ".git"))
		return fmt.Errorf("git clone: %w", err)
	}
	if head, err := repo.Head(); err == nil {
		c.logf("Cloned at %s", head.Hash().String()[:10])
	}
	return nil
}

// describeCommit is "abc1234567: first line of the message (author)".
func describeCommit(repo *gogit.Repository, h plumbing.Hash) string {
	out := h.String()[:10]
	c, err := repo.CommitObject(h)
	if err != nil {
		return out
	}
	msg, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
	if len(msg) > 100 {
		msg = msg[:100] + "…"
	}
	return fmt.Sprintf("%s: %s (%s)", out, msg, c.Author.Name)
}

// allowLocalGit lets tests clone from file:// repositories.
var allowLocalGit = false

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// script runs a template's install script in the server sandbox.
func (c *stepCtx) script(ctx context.Context, n int, st *templates.ScriptStep) error {
	c.m.mu.Lock()
	runner := c.m.deps.Scripts
	c.m.mu.Unlock()
	if runner == nil {
		return errors.New("install scripts are not available in this panel build")
	}
	c.begin(n, "Running the install script")
	timeout := time.Duration(st.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	dir, _ := filepath.Abs(c.dir)
	script := PrepareScript(st.Content, dir)
	env := templates.ShellEnv(c.vars)
	if exe, err := os.Executable(); err == nil {
		env["HOMEPANEL_BIN"] = exe
	}
	return runner.RunScript(ctx, c.job.ServerID, c.dir, script, env, timeout, c.logf)
}

// exec runs a program without a shell, streaming its output to the log.
func (c *stepCtx) exec(ctx context.Context, n int, st *templates.ExecStep) error {
	cmd, err := templates.Render(st.Command, c.vars)
	if err != nil {
		return err
	}
	args, err := templates.RenderArgs(st.Args, c.vars)
	if err != nil {
		return err
	}
	dir := c.dir
	if st.Dir != "" {
		rel, err := templates.Render(st.Dir, c.vars)
		if err != nil {
			return err
		}
		if dir, err = insideDir(c.dir, rel); err != nil {
			return err
		}
	}
	if st.IfExists != "" {
		probe, err := templates.Render(st.IfExists, c.vars)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(probe))); err != nil {
			c.begin(n, "Skipping "+filepath.Base(cmd)+" ("+probe+" not found)")
			return nil
		}
	}
	if st.IfMissing != "" {
		probe, err := templates.Render(st.IfMissing, c.vars)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(probe))); err == nil {
			c.begin(n, "Skipping "+filepath.Base(cmd)+" ("+probe+" exists)")
			return nil
		}
	}
	env := map[string]string{}
	for k, v := range st.Env {
		if env[k], err = templates.Render(v, c.vars); err != nil {
			return err
		}
	}
	// Relative commands (.venv/bin/pip) are relative to the server dir.
	if strings.Contains(cmd, "/") && !filepath.IsAbs(cmd) {
		cmd = filepath.Join(c.dir, cmd)
	}
	timeout := st.TimeoutSec
	if timeout <= 0 {
		timeout = 900
	}
	c.begin(n, "Running "+filepath.Base(cmd)+" "+strings.Join(args, " "))
	return c.runSandboxed(ctx, cmd, args, dir, env, timeout)
}

// runSandboxed runs a program inside the server's own systemd unit (the
// same sandbox as the game and as install scripts), never in the panel:
// npm/pip execute code from the server folder (lifecycle scripts, .npmrc,
// .venv/bin/*), which the game or a files.write user controls. Without a
// script runner (unit tests, no supervisor) it falls back to a child
// process.
func (c *stepCtx) runSandboxed(ctx context.Context, cmd string, args []string, dir string, env map[string]string, timeoutSec int) error {
	c.m.mu.Lock()
	runner := c.m.deps.Scripts
	c.m.mu.Unlock()
	if runner == nil || c.job == nil || c.job.ServerID == "" {
		return c.runProgram(ctx, cmd, args, dir, env, timeoutSec)
	}
	full := c.programEnv(env)
	var b strings.Builder
	b.WriteString("cd " + shellQuote(dir) + " || exit 1\nexec " + shellQuote(cmd))
	for _, a := range args {
		b.WriteString(" " + shellQuote(a))
	}
	b.WriteString("\n")
	return runner.RunScript(ctx, c.job.ServerID, c.dir, b.String(), full, time.Duration(timeoutSec)*time.Second, c.logf)
}

// shellQuote makes s one literal bash word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// programEnv is the minimal environment for install programs.
func (c *stepCtx) programEnv(env map[string]string) map[string]string {
	path := "/usr/local/bin:/usr/bin:/bin"
	for _, k := range []string{templates.KeyNode, templates.KeyJava} {
		if v := c.vars[k]; v != "" {
			path = filepath.Dir(v) + ":" + path
		}
	}
	if v := c.vars[templates.KeyPython]; v != "" {
		path = filepath.Join(c.dir, filepath.Dir(v)) + ":" + path
	}
	full := map[string]string{"PATH": path, "HOME": c.dir, "LANG": "C.UTF-8", "CI": "true", "npm_config_update_notifier": "false", "npm_config_fund": "false", "npm_config_audit": "false", "PIP_DISABLE_PIP_VERSION_CHECK": "1"}
	for k, v := range env {
		full[k] = v
	}
	return full
}

// runProgram executes cmd with a minimal environment plus the runtime bin
// dirs on PATH, piping output lines into the install log.
func (c *stepCtx) runProgram(ctx context.Context, cmd string, args []string, dir string, env map[string]string, timeoutSec int) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	p := osexec.CommandContext(ctx, cmd, args...)
	p.Dir = dir
	for k, v := range c.programEnv(env) {
		p.Env = append(p.Env, k+"="+v)
	}
	pr, pw := io.Pipe()
	p.Stdout = pw
	p.Stderr = pw
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				c.logf("  %s", line)
			}
		}
		close(done)
	}()
	err := p.Run()
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("%s failed: %w", filepath.Base(cmd), err)
	}
	return nil
}

// insideDir resolves rel under dir and refuses anything that escapes it.
func insideDir(dir, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be relative", rel)
	}
	p := filepath.Join(dir, rel)
	if p != dir && !strings.HasPrefix(p, filepath.Clean(dir)+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the server directory", rel)
	}
	// Also resolve symlinks: a folder in the path may have been replaced
	// by a link pointing outside the server.
	if _, err := os.Stat(dir); err == nil {
		real, err := files.ResolveSafe(dir, filepath.ToSlash(rel))
		if err != nil {
			return "", fmt.Errorf("path %q escapes the server directory", rel)
		}
		return real, nil
	}
	return p, nil
}

// Reconfigure re-applies the template's write/properties steps and rebuilds
// the run config after the user changed variables, reusing the builtins
// (Java path, JVM flags) resolved by the original installation. It runs
// synchronously: no downloads happen.
func (m *Manager) Reconfigure(serverDir string, tpl *templates.Template, userVars, builtins map[string]string) (*Result, error) {
	vars := map[string]string{}
	for k, v := range builtins {
		vars[k] = v
	}
	for k, v := range userVars {
		vars[k] = v
	}
	if _, ok := vars[templates.KeyJVMFlags]; !ok {
		vars[templates.KeyJVMFlags] = "[]"
	}
	job := &Job{ServerID: filepath.Base(serverDir), State: JobRunning, Steps: len(tpl.Install) + 1, StartedAt: time.Now(), Percent: -1}
	c := &stepCtx{m: m, job: job, dir: serverDir, vars: vars, tpl: tpl}
	for i, st := range tpl.Install {
		var err error
		switch {
		case st.Write != nil:
			err = c.write(i+1, st.Write)
		case st.Properties != nil:
			err = c.properties(i+1, st.Properties)
		}
		if err != nil {
			return nil, err
		}
	}
	run, ports, err := templates.BuildRun(tpl, vars)
	if err != nil {
		return nil, fmt.Errorf("build run config: %w", err)
	}
	out := map[string]string{}
	for k, v := range vars {
		if _, user := userVars[k]; !user {
			out[k] = v
		}
	}
	return &Result{Run: run, Ports: ports, Builtins: out}, nil
}

// maskSecrets hides values of secret variables in text meant for logs.
func maskSecrets(tpl *templates.Template, vars map[string]string, text string) string {
	for _, v := range tpl.Variables {
		if v.Type != templates.TypeSecret {
			continue
		}
		if val := vars[v.Key]; len(val) >= 4 {
			text = strings.ReplaceAll(text, val, "••••")
		}
	}
	return text
}

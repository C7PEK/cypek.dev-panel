// Package installer runs a template's install steps for a server: resolves
// and downloads artifacts, prepares the runtime, writes config files and
// finally produces the run config. Steps are executed by Go code, never a
// shell. Progress is kept in memory per server for the UI to poll.
package installer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cypek019/cypek.dev---panel/internal/providers"
	"github.com/cypek019/cypek.dev---panel/internal/runtimes"
	"github.com/cypek019/cypek.dev---panel/internal/servers"
	"github.com/cypek019/cypek.dev---panel/internal/shim"
	"github.com/cypek019/cypek.dev---panel/internal/templates"
)

// Deps are the collaborators steps need.
type Deps struct {
	Paper *providers.Paper
	Java  *runtimes.Java
	Log   *slog.Logger
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
	m.mu.Lock()
	if j, ok := m.jobs[serverID]; ok && j.State == JobRunning {
		m.mu.Unlock()
		return ErrInstallRunning
	}
	job := &Job{ServerID: serverID, State: JobRunning, Steps: len(tpl.Install) + 2, StartedAt: time.Now(), Percent: -1}
	m.jobs[serverID] = job
	m.mu.Unlock()

	go func() {
		res, err := m.run(ctx, job, serverDir, tpl, vars)
		m.mu.Lock()
		job.FinishedAt = time.Now()
		if err != nil {
			job.State = JobFailed
			job.Error = err.Error()
			job.Lines = appendLine(job.Lines, "ERROR: "+err.Error())
		} else {
			job.State = JobDone
			job.Percent = 100
			job.Lines = appendLine(job.Lines, "Installation finished.")
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
}

func (c *stepCtx) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
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
	c := &stepCtx{m: m, job: job, dir: dir, vars: vars}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	for i, st := range tpl.Install {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var err error
		switch {
		case st.Download != nil:
			err = c.download(ctx, i+1, st.Download)
		case st.Write != nil:
			err = c.write(i+1, st.Write)
		case st.Properties != nil:
			err = c.properties(i+1, st.Properties)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := c.runtime(ctx, len(tpl.Install)+1, tpl.Runtime); err != nil {
		return nil, err
	}
	c.begin(job.Steps, "Preparing start configuration")
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
	c.logf("Start command: %s %s", run.Command, strings.Join(run.Args, " "))
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
	case "paper":
		c.begin(n, "Downloading Paper")
		if c.m.deps.Paper == nil {
			return errors.New("paper provider not configured")
		}
		info, err := c.m.deps.Paper.Resolve(ctx, version)
		if err != nil {
			return fmt.Errorf("resolve Paper %s: %w", version, err)
		}
		c.logf("Paper %s build %d (%s), needs Java %d", info.Version, info.LatestBuild, info.DownloadName, info.JavaMinimum)
		if err := providers.Download(ctx, info.DownloadURL, dest, info.SHA256, c.progress); err != nil {
			return err
		}
		c.logf("Downloaded and verified %s", filepath.Base(dest))
		c.vars["MC_VERSION_RESOLVED"] = info.Version
		c.vars["MC_BUILD"] = strconv.Itoa(info.LatestBuild)
		c.vars["JAVA_MIN"] = strconv.Itoa(info.JavaMinimum)
		flags, _ := json.Marshal(info.JVMFlags)
		c.vars[templates.KeyJVMFlags] = string(flags)
		return nil
	default:
		return fmt.Errorf("unknown download provider %q", st.Provider)
	}
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
	dest, err := insideDir(c.dir, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	return os.WriteFile(dest, []byte(content), 0o640)
}

// properties sets keys in a Java .properties file, keeping other lines
// (comments included) and their order.
func (c *stepCtx) properties(n int, st *templates.PropertiesStep) error {
	path, err := templates.Render(st.Path, c.vars)
	if err != nil {
		return err
	}
	c.begin(n, "Updating "+path)
	dest, err := insideDir(c.dir, path)
	if err != nil {
		return err
	}
	want := map[string]string{}
	for k, v := range st.Set {
		if want[k], err = templates.Render(v, c.vars); err != nil {
			return err
		}
	}
	var lines []string
	if f, err := os.Open(dest); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		f.Close()
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
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	return os.WriteFile(dest, []byte(strings.Join(lines, "\n")+"\n"), 0o640)
}

// escapeProp escapes what Java's Properties loader would otherwise interpret.
func escapeProp(v string) string {
	r := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "\t", "\\t")
	return r.Replace(v)
}

func (c *stepCtx) runtime(ctx context.Context, n int, rt templates.Runtime) error {
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
		c.vars["JAVA_FEATURE"] = strconv.Itoa(feature)
		if _, ok := c.vars[templates.KeyJVMFlags]; !ok {
			c.vars[templates.KeyJVMFlags] = "[]"
		}
		return nil
	default:
		return fmt.Errorf("runtime %q is not supported yet", rt.Kind)
	}
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
	return p, nil
}

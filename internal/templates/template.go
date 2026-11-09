// Package templates loads, validates and renders game templates. A template
// is YAML (built-in from /templates or user-provided) describing variables,
// install steps and how to start/stop the server. Rendering is deliberately
// dumb: {{KEY}} is replaced only with known variables, only in the fields
// listed here, and never through a shell.
package templates

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/C7PEK/cypek.dev-panel/internal/servers"
)

// Text is a translated string; the UI picks the user's language with English as fallback.
type Text map[string]string

// Get returns the text in lang, falling back to en, then any.
func (t Text) Get(lang string) string {
	if v, ok := t[lang]; ok {
		return v
	}
	if v, ok := t["en"]; ok {
		return v
	}
	for _, v := range t {
		return v
	}
	return ""
}

// VarType is a variable's input type.
type VarType string

const (
	TypeString VarType = "string"
	TypeNumber VarType = "number"
	TypeBool   VarType = "bool"
	TypeSelect VarType = "select"
	TypeSecret VarType = "secret"
)

// Variable is a user-facing input shown in the creation wizard.
type Variable struct {
	Key     string  `yaml:"key" json:"key"`
	Label   Text    `yaml:"label" json:"label"`
	Help    Text    `yaml:"help,omitempty" json:"help,omitempty"`
	Type    VarType `yaml:"type" json:"type"`
	Default string  `yaml:"default,omitempty" json:"default,omitempty"`
	// Options for select without a Source.
	Options []string `yaml:"options,omitempty" json:"options,omitempty"`
	// Source names a dynamic option provider (e.g. paper-versions).
	Source    string   `yaml:"source,omitempty" json:"source,omitempty"`
	Min       *float64 `yaml:"min,omitempty" json:"min,omitempty"`
	Max       *float64 `yaml:"max,omitempty" json:"max,omitempty"`
	MaxLength int      `yaml:"maxLength,omitempty" json:"maxLength,omitempty"`
	Pattern   string   `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	Required  bool     `yaml:"required,omitempty" json:"required,omitempty"`
	// Consent marks a bool the user must switch on (EULA, license terms).
	Consent bool `yaml:"consent,omitempty" json:"consent,omitempty"`
	// Hidden variables are not shown in forms; their default is used.
	Hidden bool `yaml:"hidden,omitempty" json:"hidden,omitempty"`
}

// Runtime names the interpreter the server needs.
type Runtime struct {
	Kind    string `yaml:"kind" json:"kind"`                           // java | node | python | none
	Version string `yaml:"version,omitempty" json:"version,omitempty"` // auto or a major version
}

// Step is one install step; exactly one field is set.
type Step struct {
	Download   *DownloadStep   `yaml:"download,omitempty" json:"download,omitempty"`
	Extract    *ExtractStep    `yaml:"extract,omitempty" json:"extract,omitempty"`
	Write      *WriteStep      `yaml:"write,omitempty" json:"write,omitempty"`
	Properties *PropertiesStep `yaml:"properties,omitempty" json:"properties,omitempty"`
	Git        *GitStep        `yaml:"git,omitempty" json:"git,omitempty"`
	Exec       *ExecStep       `yaml:"exec,omitempty" json:"exec,omitempty"`
	Script     *ScriptStep     `yaml:"script,omitempty" json:"script,omitempty"`
}

// ScriptStep runs a bash script inside the server's own sandbox (the same
// systemd unit the game runs in: write access to the server directory only,
// as the panel user, no root). Template variables are passed as environment
// variables, never spliced into the script text. Used by imported eggs.
type ScriptStep struct {
	Content    string `yaml:"content" json:"content"`
	TimeoutSec int    `yaml:"timeoutSec,omitempty" json:"timeoutSec,omitempty"`
}

// GitStep clones a repository (or updates it when already present).
type GitStep struct {
	URL    string `yaml:"url" json:"url"`
	Branch string `yaml:"branch,omitempty" json:"branch,omitempty"`
	// Token is an optional secret variable reference for private repos.
	Token string `yaml:"token,omitempty" json:"token,omitempty"`
	To    string `yaml:"to" json:"to"`
}

// ExecStep runs a program (never a shell) inside the server directory,
// e.g. npm install or pip install. The command is rendered, so it can be a
// runtime builtin like {{NPM}}.
type ExecStep struct {
	Command string            `yaml:"command" json:"command"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Dir     string            `yaml:"dir,omitempty" json:"dir,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	// IfExists skips the step unless this file (relative to Dir) exists.
	IfExists string `yaml:"ifExists,omitempty" json:"ifExists,omitempty"`
	// TimeoutSec bounds the run (default 900).
	TimeoutSec int `yaml:"timeoutSec,omitempty" json:"timeoutSec,omitempty"`
}

// DownloadStep fetches an artifact through a named provider:
//
//	paper  - PaperMC build for Version (checksummed)
//	mojang - vanilla server jar for Version (SHA-1 from Mojang)
//	fabric - Fabric server launcher for Version (latest = newest supported)
//	purpur - newest Purpur build for Version (MD5 from Purpur)
//	fivem  - FXServer Linux artifact for Version (recommended|latest|build)
//	url    - a fixed https URL (optional Sha256)
type DownloadStep struct {
	Provider string `yaml:"provider" json:"provider"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty"`
	URL      string `yaml:"url,omitempty" json:"url,omitempty"`
	Sha256   string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	To       string `yaml:"to" json:"to"`
}

// ExtractStep unpacks an archive (tar.gz, tar.xz, zip) inside the server dir.
type ExtractStep struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"` // "." for the server directory itself
	// Strip drops leading path components (GitHub zips have one top folder).
	Strip int `yaml:"strip,omitempty" json:"strip,omitempty"`
	// Delete removes the archive afterwards (default true).
	Delete *bool `yaml:"delete,omitempty" json:"delete,omitempty"`
}

// WriteStep writes a small text file; with RequireConsent naming a bool
// variable that must be true (the user, not the panel, accepts the terms).
type WriteStep struct {
	Path           string `yaml:"path" json:"path"`
	Content        string `yaml:"content" json:"content"`
	RequireConsent string `yaml:"requireConsent,omitempty" json:"requireConsent,omitempty"`
}

// PropertiesStep sets key=value lines in a Java-properties style file,
// keeping everything else in it.
type PropertiesStep struct {
	Path string            `yaml:"path" json:"path"`
	Set  map[string]string `yaml:"set" json:"set"`
}

// Start describes the process; command and args are rendered.
type Start struct {
	Command string            `yaml:"command,omitempty" json:"command"`
	Args    []string          `yaml:"args,omitempty" json:"args"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	// Dir is the working directory relative to the server dir.
	Dir string `yaml:"dir,omitempty" json:"dir,omitempty"`
	// Shell is an alternative to Command/Args: a bash command line, as used
	// by Pterodactyl/PufferPanel. {{KEY}} becomes "${KEY}" and every variable
	// is exported to the environment, so values can never become code.
	Shell string `yaml:"shell,omitempty" json:"shell,omitempty"`
}

// ConfigFile sets values in a config file before every start (Pterodactyl's
// "config.files"). Parsers: properties, ini (key=value lines), file (lines
// starting with the key are replaced by the value), yaml and json (dotted
// keys). Values are rendered with the server's variables.
type ConfigFile struct {
	Path   string            `yaml:"path" json:"path"`
	Parser string            `yaml:"parser" json:"parser"`
	Set    map[string]string `yaml:"set" json:"set"`
}

// Source records where a template came from (imports from other panels).
type Source struct {
	Kind       string   `yaml:"kind" json:"kind"` // custom | pterodactyl | pelican | pufferpanel
	Original   string   `yaml:"original,omitempty" json:"original,omitempty"`
	Author     string   `yaml:"author,omitempty" json:"author,omitempty"`
	URL        string   `yaml:"url,omitempty" json:"url,omitempty"`
	ImportedAt string   `yaml:"importedAt,omitempty" json:"importedAt,omitempty"`
	Warnings   []string `yaml:"warnings,omitempty" json:"warnings,omitempty"`
}

// Stop describes graceful shutdown.
type Stop struct {
	Command    string `yaml:"command,omitempty" json:"command,omitempty"`
	TimeoutSec int    `yaml:"timeoutSec,omitempty" json:"timeoutSec,omitempty"`
}

// Port is a listening port; Port may be a template.
type Port struct {
	Name     string `yaml:"name" json:"name"`
	Port     string `yaml:"port" json:"port"`
	Protocol string `yaml:"protocol" json:"protocol"`
}

// Query tells internal/query how to count players.
type Query struct {
	Kind string `yaml:"kind" json:"kind"`
	Port string `yaml:"port" json:"port"`
}

// Command is a console hint: the UI suggests it while typing and completes
// its arguments. Args uses a tiny grammar: <required>, [optional], <a|b|c>
// for choices, <player>/[player] for an online player, <text...> for the rest
// of the line.
type Command struct {
	Name string `yaml:"name" json:"name"`
	Args string `yaml:"args,omitempty" json:"args,omitempty"`
	Help Text   `yaml:"help,omitempty" json:"help,omitempty"`
}

// ConfigDefault is a line the panel keeps present in the server's config
// files: before every start, each file matching Files that has no
// uncommented line containing Key gets Line prepended. Existing settings are
// never changed, so the user can always override the default.
type ConfigDefault struct {
	Files []string `yaml:"files" json:"files"` // globs relative to the server dir
	Key   string   `yaml:"key" json:"key"`
	Line  string   `yaml:"line" json:"line"`
}

// Console groups console-related hints.
type Console struct {
	Commands []Command `yaml:"commands,omitempty" json:"commands,omitempty"`
}

// Template is a parsed template file.
type Template struct {
	ID          string           `yaml:"id" json:"id"`
	Name        string           `yaml:"name" json:"name"`
	Category    servers.Category `yaml:"category" json:"category"`
	Icon        string           `yaml:"icon,omitempty" json:"icon,omitempty"`
	Description Text             `yaml:"description,omitempty" json:"description,omitempty"`
	Variables   []Variable       `yaml:"variables" json:"variables"`
	Runtime     Runtime          `yaml:"runtime" json:"runtime"`
	Install     []Step           `yaml:"install" json:"install"`
	Start       Start            `yaml:"start" json:"start"`
	Stop        Stop             `yaml:"stop" json:"stop"`
	ReadyRegex  string           `yaml:"readyRegex,omitempty" json:"readyRegex,omitempty"`
	Ports       []Port           `yaml:"ports,omitempty" json:"ports,omitempty"`
	WarnCommand string           `yaml:"warnCommand,omitempty" json:"warnCommand,omitempty"`
	Query       *Query           `yaml:"query,omitempty" json:"query,omitempty"`
	Console     Console          `yaml:"console,omitempty" json:"console"`
	// ConfigDefaults are applied before every start (see ConfigDefault).
	ConfigDefaults []ConfigDefault `yaml:"configDefaults,omitempty" json:"configDefaults,omitempty"`
	// ConfigFiles are applied before every start (see ConfigFile).
	ConfigFiles []ConfigFile `yaml:"configFiles,omitempty" json:"configFiles,omitempty"`
	// Source describes imported templates.
	Source *Source `yaml:"source,omitempty" json:"source,omitempty"`
	// Builtin is true for templates embedded in the binary.
	Builtin bool `yaml:"-" json:"builtin"`
	// Raw is the YAML the template was parsed from.
	Raw []byte `yaml:"-" json:"-"`
}

var (
	idRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
	keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
)

// Parse decodes and validates one template.
func Parse(data []byte) (*Template, error) {
	var t Template
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	t.Raw = append([]byte(nil), data...)
	return &t, nil
}

// Validate checks structure; it does not resolve dynamic sources.
func (t *Template) Validate() error {
	if !idRe.MatchString(t.ID) {
		return fmt.Errorf("template id %q must be lowercase letters, digits and dashes", t.ID)
	}
	if t.Name == "" {
		return fmt.Errorf("template %s: name is required", t.ID)
	}
	if !t.Category.Valid() {
		return fmt.Errorf("template %s: invalid category %q", t.ID, t.Category)
	}
	seen := map[string]bool{}
	for i := range t.Variables {
		v := &t.Variables[i]
		if !keyRe.MatchString(v.Key) {
			return fmt.Errorf("template %s: variable key %q must be a valid environment variable name (letters, digits, underscore)", t.ID, v.Key)
		}
		if seen[v.Key] || isBuiltinKey(v.Key) {
			return fmt.Errorf("template %s: variable %s is duplicated or reserved", t.ID, v.Key)
		}
		seen[v.Key] = true
		switch v.Type {
		case TypeString, TypeNumber, TypeBool, TypeSecret:
		case TypeSelect:
			if len(v.Options) == 0 && v.Source == "" {
				return fmt.Errorf("template %s: select %s needs options or source", t.ID, v.Key)
			}
		default:
			return fmt.Errorf("template %s: variable %s has unknown type %q", t.ID, v.Key, v.Type)
		}
		if v.Pattern != "" {
			if _, err := regexp.Compile(v.Pattern); err != nil {
				return fmt.Errorf("template %s: variable %s pattern: %w", t.ID, v.Key, err)
			}
		}
		if v.Consent && v.Type != TypeBool {
			return fmt.Errorf("template %s: consent variable %s must be bool", t.ID, v.Key)
		}
	}
	switch t.Runtime.Kind {
	case "", "none", "java", "node", "python":
	default:
		return fmt.Errorf("template %s: unknown runtime %q", t.ID, t.Runtime.Kind)
	}
	for i, s := range t.Install {
		n := 0
		if s.Download != nil {
			n++
			if s.Download.Provider == "" || s.Download.To == "" {
				return fmt.Errorf("template %s: install step %d: download needs provider and to", t.ID, i+1)
			}
			if s.Download.Provider == "url" && !strings.HasPrefix(s.Download.URL, "https://") {
				return fmt.Errorf("template %s: install step %d: url downloads must use https", t.ID, i+1)
			}
			if err := safeRelPath(s.Download.To); err != nil {
				return fmt.Errorf("template %s: install step %d: %w", t.ID, i+1, err)
			}
		}
		if s.Extract != nil {
			n++
			if err := safeRelPath(s.Extract.From); err != nil {
				return fmt.Errorf("template %s: install step %d: %w", t.ID, i+1, err)
			}
			if s.Extract.To != "." {
				if err := safeRelPath(s.Extract.To); err != nil {
					return fmt.Errorf("template %s: install step %d: %w", t.ID, i+1, err)
				}
			}
			if s.Extract.Strip < 0 || s.Extract.Strip > 8 {
				return fmt.Errorf("template %s: install step %d: strip must be 0-8", t.ID, i+1)
			}
		}
		if s.Write != nil {
			n++
			if err := safeRelPath(s.Write.Path); err != nil {
				return fmt.Errorf("template %s: install step %d: %w", t.ID, i+1, err)
			}
			if s.Write.RequireConsent != "" && !seen[s.Write.RequireConsent] {
				return fmt.Errorf("template %s: install step %d: requireConsent names unknown variable %s", t.ID, i+1, s.Write.RequireConsent)
			}
		}
		if s.Properties != nil {
			n++
			if err := safeRelPath(s.Properties.Path); err != nil {
				return fmt.Errorf("template %s: install step %d: %w", t.ID, i+1, err)
			}
		}
		if s.Git != nil {
			n++
			if s.Git.URL == "" || s.Git.To == "" {
				return fmt.Errorf("template %s: install step %d: git needs url and to", t.ID, i+1)
			}
			if err := safeRelPath(s.Git.To); err != nil {
				return fmt.Errorf("template %s: install step %d: %w", t.ID, i+1, err)
			}
			if s.Git.Token != "" && !seen[s.Git.Token] {
				return fmt.Errorf("template %s: install step %d: token names unknown variable %s", t.ID, i+1, s.Git.Token)
			}
		}
		if s.Exec != nil {
			n++
			if s.Exec.Command == "" {
				return fmt.Errorf("template %s: install step %d: exec needs a command", t.ID, i+1)
			}
			if s.Exec.Dir != "" {
				if err := safeRelPath(s.Exec.Dir); err != nil {
					return fmt.Errorf("template %s: install step %d: %w", t.ID, i+1, err)
				}
			}
			if s.Exec.IfExists != "" {
				if err := safeRelPath(s.Exec.IfExists); err != nil {
					return fmt.Errorf("template %s: install step %d: %w", t.ID, i+1, err)
				}
			}
		}
		if s.Script != nil {
			n++
			if strings.TrimSpace(s.Script.Content) == "" {
				return fmt.Errorf("template %s: install step %d: script is empty", t.ID, i+1)
			}
			if s.Script.TimeoutSec < 0 || s.Script.TimeoutSec > 4*3600 {
				return fmt.Errorf("template %s: install step %d: timeoutSec must be 0-14400", t.ID, i+1)
			}
		}
		if n != 1 {
			return fmt.Errorf("template %s: install step %d must have exactly one of download, extract, write, properties, git, exec, script", t.ID, i+1)
		}
	}
	if (t.Start.Command == "") == (t.Start.Shell == "") {
		return fmt.Errorf("template %s: start needs exactly one of command or shell", t.ID)
	}
	for i, cf := range t.ConfigFiles {
		if err := safeRelPath(cf.Path); err != nil {
			return fmt.Errorf("template %s: configFiles %d: %w", t.ID, i+1, err)
		}
		switch cf.Parser {
		case "properties", "ini", "file", "yaml", "json":
		default:
			return fmt.Errorf("template %s: configFiles %d: unknown parser %q", t.ID, i+1, cf.Parser)
		}
	}
	if t.Start.Dir != "" {
		if err := safeRelPath(t.Start.Dir); err != nil {
			return fmt.Errorf("template %s: start.dir: %w", t.ID, err)
		}
	}
	if t.ReadyRegex != "" {
		if _, err := regexp.Compile(t.ReadyRegex); err != nil {
			return fmt.Errorf("template %s: readyRegex: %w", t.ID, err)
		}
	}
	for _, p := range t.Ports {
		if p.Protocol != "tcp" && p.Protocol != "udp" && p.Protocol != "tcp+udp" {
			return fmt.Errorf("template %s: port %s: protocol must be tcp, udp or tcp+udp", t.ID, p.Name)
		}
	}
	for i, d := range t.ConfigDefaults {
		if d.Key == "" || d.Line == "" || strings.ContainsAny(d.Line, "\n\r") || len(d.Files) == 0 {
			return fmt.Errorf("template %s: configDefaults %d needs files, key and a single-line line", t.ID, i+1)
		}
		for _, f := range d.Files {
			if err := safeRelPath(f); err != nil {
				return fmt.Errorf("template %s: configDefaults %d: %w", t.ID, i+1, err)
			}
		}
	}
	for _, c := range t.Console.Commands {
		if c.Name == "" || strings.ContainsAny(c.Name, " \t\n") {
			return fmt.Errorf("template %s: console command %q must be a single word", t.ID, c.Name)
		}
	}
	if t.Query != nil && t.Query.Kind != "minecraft-ping" && t.Query.Kind != "fivem-http" {
		return fmt.Errorf("template %s: unknown query kind %q", t.ID, t.Query.Kind)
	}
	// Every {{KEY}} used anywhere must be a variable or a builtin.
	for _, s := range t.allTemplatedStrings() {
		for _, k := range placeholders(s) {
			if !seen[k] && !isBuiltinKey(k) {
				return fmt.Errorf("template %s: unknown placeholder {{%s}}", t.ID, k)
			}
		}
	}
	return nil
}

// safeRelPath accepts simple relative paths inside the server directory.
func safeRelPath(p string) error {
	if p == "" || filepath.IsAbs(p) || strings.Contains(p, "..") || strings.ContainsAny(p, "\x00\n") {
		return fmt.Errorf("path %q must be relative and inside the server directory", p)
	}
	return nil
}

func (t *Template) allTemplatedStrings() []string {
	out := []string{t.Start.Command, t.Start.Shell, t.WarnCommand, t.Runtime.Version}
	for _, cf := range t.ConfigFiles {
		out = append(out, cf.Path)
		for _, v := range cf.Set {
			out = append(out, v)
		}
	}
	out = append(out, t.Start.Args...)
	for _, v := range t.Start.Env {
		out = append(out, v)
	}
	for _, s := range t.Install {
		if s.Download != nil {
			out = append(out, s.Download.Version, s.Download.To, s.Download.URL)
		}
		if s.Extract != nil {
			out = append(out, s.Extract.From, s.Extract.To)
		}
		if s.Git != nil {
			out = append(out, s.Git.URL, s.Git.Branch, s.Git.To)
		}
		if s.Exec != nil {
			out = append(out, s.Exec.Command, s.Exec.Dir, s.Exec.IfExists)
			out = append(out, s.Exec.Args...)
			for _, v := range s.Exec.Env {
				out = append(out, v)
			}
		}
		if s.Write != nil {
			out = append(out, s.Write.Path, s.Write.Content)
		}
		if s.Properties != nil {
			out = append(out, s.Properties.Path)
			for _, v := range s.Properties.Set {
				out = append(out, v)
			}
		}
	}
	for _, p := range t.Ports {
		out = append(out, p.Port)
	}
	if t.Query != nil {
		out = append(out, t.Query.Port)
	}
	return out
}

// Builtin keys are provided by the installer/runtime, never by the user.
const (
	KeyJava     = "JAVA"      // path to the java binary
	KeyJVMFlags = "JVM_FLAGS" // list (JSON array) of recommended JVM flags
	KeyNode     = "NODE"
	KeyNPM      = "NPM"
	KeyNPX      = "NPX"
	KeyPython   = "PYTHON" // the venv interpreter once created
	KeyPip      = "PIP"
	KeyMessage  = "message" // warnCommand only
	// KeyServerDir is the absolute server directory; KeyRuntimeBin the bin
	// directory of the prepared runtime (java/node/python), for PATH.
	KeyServerDir  = "SERVER_DIR"
	KeyRuntimeBin = "RUNTIME_BIN"
	KeyJavaHome   = "JAVA_HOME"
)

func isBuiltinKey(k string) bool {
	switch k {
	case KeyJava, KeyJVMFlags, KeyNode, KeyNPM, KeyNPX, KeyPython, KeyPip, KeyMessage, KeyQueryKind, KeyQueryPort, KeyServerDir, KeyRuntimeBin, KeyJavaHome:
		return true
	}
	return false
}

var placeholderRe = regexp.MustCompile(`\{\{([A-Za-z][A-Za-z0-9_]*)(\.\.\.)?\}\}`)

func placeholders(s string) []string {
	var out []string
	for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// ErrMissingVariable is returned when rendering hits an unset placeholder.
var ErrMissingVariable = errors.New("missing variable")

// Render substitutes {{KEY}} with vars[KEY]. Unknown keys are an error, so a
// typo never silently produces an empty string in a command line.
func Render(s string, vars map[string]string) (string, error) {
	var err error
	out := placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := placeholderRe.FindStringSubmatch(m)
		v, ok := vars[sub[1]]
		if !ok {
			err = fmt.Errorf("%w: %s", ErrMissingVariable, sub[1])
			return m
		}
		return v
	})
	return out, err
}

// RenderArgs renders each argument; an argument that is exactly "{{KEY...}}"
// expands to the JSON array stored in vars[KEY] (used for JVM flag lists).
func RenderArgs(args []string, vars map[string]string) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if m := spreadRe.FindStringSubmatch(a); m != nil {
			raw, ok := vars[m[1]]
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrMissingVariable, m[1])
			}
			list, err := decodeList(raw)
			if err != nil {
				return nil, fmt.Errorf("variable %s is not a list: %w", m[1], err)
			}
			out = append(out, list...)
			continue
		}
		r, err := Render(a, vars)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

var spreadRe = regexp.MustCompile(`^\{\{([A-Za-z][A-Za-z0-9_]*)\.\.\.\}\}$`)

// ValidateValues checks user-supplied values against the variable schema and
// returns the normalised map (defaults filled in). Select values backed by a
// source are checked by the caller, which knows the live option list.
func (t *Template) ValidateValues(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range t.Variables {
		raw, given := in[v.Key]
		if !given || raw == "" {
			raw = v.Default
		}
		switch v.Type {
		case TypeNumber:
			if raw == "" {
				if v.Required || v.Default == "" {
					return nil, &ValueError{Key: v.Key, Message: "a number is required"}
				}
			}
			n, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, &ValueError{Key: v.Key, Message: "must be a number"}
			}
			if v.Min != nil && n < *v.Min {
				return nil, &ValueError{Key: v.Key, Message: fmt.Sprintf("must be at least %s", trimFloat(*v.Min))}
			}
			if v.Max != nil && n > *v.Max {
				return nil, &ValueError{Key: v.Key, Message: fmt.Sprintf("must be at most %s", trimFloat(*v.Max))}
			}
			raw = trimFloat(n)
		case TypeBool:
			b, err := strconv.ParseBool(strings.ToLower(raw))
			if err != nil {
				if raw == "" {
					b = false
				} else {
					return nil, &ValueError{Key: v.Key, Message: "must be true or false"}
				}
			}
			if v.Consent && !b {
				return nil, &ValueError{Key: v.Key, Message: "you have to accept this to continue", Consent: true}
			}
			raw = strconv.FormatBool(b)
		case TypeSelect:
			if raw == "" {
				return nil, &ValueError{Key: v.Key, Message: "choose a value"}
			}
			if len(v.Options) > 0 && !contains(v.Options, raw) {
				return nil, &ValueError{Key: v.Key, Message: "is not one of the allowed options"}
			}
		case TypeString, TypeSecret:
			if raw == "" && v.Required {
				return nil, &ValueError{Key: v.Key, Message: "is required"}
			}
			if v.MaxLength > 0 && len([]rune(raw)) > v.MaxLength {
				return nil, &ValueError{Key: v.Key, Message: fmt.Sprintf("must be at most %d characters", v.MaxLength)}
			}
			if v.Pattern != "" && raw != "" && !regexp.MustCompile(v.Pattern).MatchString(raw) {
				return nil, &ValueError{Key: v.Key, Message: "has an invalid format"}
			}
		}
		if strings.ContainsAny(raw, "\x00\n\r") {
			return nil, &ValueError{Key: v.Key, Message: "must be a single line"}
		}
		out[v.Key] = raw
	}
	for k := range in {
		if _, ok := out[k]; !ok {
			return nil, &ValueError{Key: k, Message: "is not a variable of this template"}
		}
	}
	return out, nil
}

// ValueError reports which variable failed validation.
type ValueError struct {
	Key     string
	Message string
	Consent bool
}

func (e *ValueError) Error() string { return e.Key + " " + e.Message }

// UsesScript reports whether installation runs a shell script.
// UsesGit reports whether the install fetches code from git (bots), which
// enables the "update from git" action.
func (t *Template) UsesGit() bool {
	for _, st := range t.Install {
		if st.Git != nil {
			return true
		}
	}
	return false
}

func (t *Template) UsesScript() bool {
	for _, s := range t.Install {
		if s.Script != nil {
			return true
		}
	}
	return false
}

// Variable returns the variable with key, or nil.
func (t *Template) Variable(key string) *Variable {
	for i := range t.Variables {
		if t.Variables[i].Key == key {
			return &t.Variables[i]
		}
	}
	return nil
}

// SecretKeys lists variables of type secret.
func (t *Template) SecretKeys() []string {
	var out []string
	for _, v := range t.Variables {
		if v.Type == TypeSecret {
			out = append(out, v.Key)
		}
	}
	return out
}

func trimFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Registry holds the built-in and user templates. It is safe for concurrent
// use; user templates can be added, replaced and removed at runtime.
type Registry struct {
	mu        sync.RWMutex
	builtinFS fs.FS
	userDir   string
	templates map[string]*Template
	builtins  map[string]*Template
	// overrides marks user templates that replace a built-in id.
	overrides map[string]bool
}

// Load reads built-in templates from builtin and user templates (*.yaml)
// from userDir, which may not exist. A user template with the same id as a
// built-in one overrides it. Broken user files are skipped (and reported by
// LoadErrors) so one bad file cannot keep the panel from starting.
func Load(builtin fs.FS, userDir string) (*Registry, error) {
	r := &Registry{builtinFS: builtin, userDir: userDir}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// loadErrors holds per-file problems of the last reload.
var loadErrors = map[string]string{}

// LoadErrors returns problems with user template files (file -> error).
func (r *Registry) LoadErrors() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string]string{}
	for k, v := range loadErrors {
		out[k] = v
	}
	return out
}

func (r *Registry) reload() error {
	templates := map[string]*Template{}
	builtins := map[string]*Template{}
	overrides := map[string]bool{}
	errs := map[string]string{}
	entries, err := fs.ReadDir(r.builtinFS, ".")
	if err != nil {
		return fmt.Errorf("read built-in templates: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := fs.ReadFile(r.builtinFS, e.Name())
		if err != nil {
			return err
		}
		t, err := Parse(b)
		if err != nil {
			return fmt.Errorf("built-in %s: %w", e.Name(), err)
		}
		t.Builtin = true
		templates[t.ID] = t
		builtins[t.ID] = t
	}
	if r.userDir != "" {
		files, _ := filepath.Glob(filepath.Join(r.userDir, "*.yaml"))
		sort.Strings(files)
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				errs[filepath.Base(f)] = err.Error()
				continue
			}
			t, err := Parse(b)
			if err != nil {
				errs[filepath.Base(f)] = err.Error()
				continue
			}
			if _, ok := builtins[t.ID]; ok {
				overrides[t.ID] = true
			}
			templates[t.ID] = t
		}
	}
	r.mu.Lock()
	r.templates, r.builtins, r.overrides = templates, builtins, overrides
	loadErrors = errs
	r.mu.Unlock()
	return nil
}

// Get returns a template by id.
func (r *Registry) Get(id string) (*Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.templates[id]
	return t, ok
}

// Builtin returns the embedded template with id, even when overridden.
func (r *Registry) Builtin(id string) (*Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.builtins[id]
	return t, ok
}

// Overrides reports whether a user template replaces the built-in id.
func (r *Registry) Overrides(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.overrides[id]
}

// List returns templates sorted by name.
func (r *Registry) List() []*Template {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Template, 0, len(r.templates))
	for _, t := range r.templates {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ErrNoUserDir means templates cannot be saved (no writable directory).
var ErrNoUserDir = errors.New("no directory for user templates is configured")

// Save validates yaml and writes it as <userDir>/<id>.yaml, then reloads.
func (r *Registry) Save(data []byte) (*Template, error) {
	if r.userDir == "" {
		return nil, ErrNoUserDir
	}
	t, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(r.userDir, 0o750); err != nil {
		return nil, err
	}
	path := filepath.Join(r.userDir, t.ID+".yaml")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	if err := r.reload(); err != nil {
		return nil, err
	}
	saved, _ := r.Get(t.ID)
	return saved, nil
}

// IsUser reports whether id is backed by a user file.
func (r *Registry) IsUser(id string) bool {
	if r.userDir == "" || !idRe.MatchString(id) {
		return false
	}
	_, err := os.Stat(filepath.Join(r.userDir, id+".yaml"))
	return err == nil
}

// Delete removes the user file for id (a built-in override reverts to the built-in).
func (r *Registry) Delete(id string) error {
	if !r.IsUser(id) {
		return os.ErrNotExist
	}
	if err := os.Remove(filepath.Join(r.userDir, id+".yaml")); err != nil {
		return err
	}
	return r.reload()
}

// decodeList parses a JSON array of strings (how list builtins are stored).
func decodeList(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var list []string
	if err := yaml.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	return list, nil
}

package templates

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/C7PEK/cypek.dev-panel/internal/servers"
)

type Text map[string]string

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

type VarType string

const (
	TypeString VarType = "string"
	TypeNumber VarType = "number"
	TypeBool   VarType = "bool"
	TypeSelect VarType = "select"
	TypeSecret VarType = "secret"
)

type Variable struct {
	Key     string  `yaml:"key" json:"key"`
	Label   Text    `yaml:"label" json:"label"`
	Help    Text    `yaml:"help,omitempty" json:"help,omitempty"`
	Type    VarType `yaml:"type" json:"type"`
	Default string  `yaml:"default,omitempty" json:"default,omitempty"`

	Options []string `yaml:"options,omitempty" json:"options,omitempty"`

	Source    string   `yaml:"source,omitempty" json:"source,omitempty"`
	Min       *float64 `yaml:"min,omitempty" json:"min,omitempty"`
	Max       *float64 `yaml:"max,omitempty" json:"max,omitempty"`
	MaxLength int      `yaml:"maxLength,omitempty" json:"maxLength,omitempty"`
	Pattern   string   `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	Required  bool     `yaml:"required,omitempty" json:"required,omitempty"`

	Consent bool `yaml:"consent,omitempty" json:"consent,omitempty"`

	Hidden bool `yaml:"hidden,omitempty" json:"hidden,omitempty"`
}

type Runtime struct {
	Kind    string `yaml:"kind" json:"kind"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

type Step struct {
	Download   *DownloadStep   `yaml:"download,omitempty" json:"download,omitempty"`
	Extract    *ExtractStep    `yaml:"extract,omitempty" json:"extract,omitempty"`
	Write      *WriteStep      `yaml:"write,omitempty" json:"write,omitempty"`
	Properties *PropertiesStep `yaml:"properties,omitempty" json:"properties,omitempty"`
	Git        *GitStep        `yaml:"git,omitempty" json:"git,omitempty"`
	Exec       *ExecStep       `yaml:"exec,omitempty" json:"exec,omitempty"`
	Script     *ScriptStep     `yaml:"script,omitempty" json:"script,omitempty"`
}

type ScriptStep struct {
	Content    string `yaml:"content" json:"content"`
	TimeoutSec int    `yaml:"timeoutSec,omitempty" json:"timeoutSec,omitempty"`
}

type GitStep struct {
	URL    string `yaml:"url" json:"url"`
	Branch string `yaml:"branch,omitempty" json:"branch,omitempty"`

	Token string `yaml:"token,omitempty" json:"token,omitempty"`
	To    string `yaml:"to" json:"to"`
}

type ExecStep struct {
	Command string            `yaml:"command" json:"command"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Dir     string            `yaml:"dir,omitempty" json:"dir,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	IfExists string `yaml:"ifExists,omitempty" json:"ifExists,omitempty"`

	IfMissing string `yaml:"ifMissing,omitempty" json:"ifMissing,omitempty"`

	TimeoutSec int `yaml:"timeoutSec,omitempty" json:"timeoutSec,omitempty"`
}

type DownloadStep struct {
	Provider string `yaml:"provider" json:"provider"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty"`
	URL      string `yaml:"url,omitempty" json:"url,omitempty"`
	Sha256   string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	To       string `yaml:"to" json:"to"`
}

type ExtractStep struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`

	Strip int `yaml:"strip,omitempty" json:"strip,omitempty"`

	Delete *bool `yaml:"delete,omitempty" json:"delete,omitempty"`

	Keep []string `yaml:"keep,omitempty" json:"keep,omitempty"`
}

func (e *ExtractStep) Keeps(rel string) bool {
	for _, k := range e.Keep {
		if ok, _ := path.Match(k, rel); ok {
			return true
		}
	}
	return false
}

type WriteStep struct {
	Path           string `yaml:"path" json:"path"`
	Content        string `yaml:"content" json:"content"`
	RequireConsent string `yaml:"requireConsent,omitempty" json:"requireConsent,omitempty"`
}

type PropertiesStep struct {
	Path string            `yaml:"path" json:"path"`
	Set  map[string]string `yaml:"set" json:"set"`
}

type Start struct {
	Command string            `yaml:"command,omitempty" json:"command"`
	Args    []string          `yaml:"args,omitempty" json:"args"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	Dir string `yaml:"dir,omitempty" json:"dir,omitempty"`

	Shell string `yaml:"shell,omitempty" json:"shell,omitempty"`
}

type ConfigFile struct {
	Path   string            `yaml:"path" json:"path"`
	Parser string            `yaml:"parser" json:"parser"`
	Set    map[string]string `yaml:"set" json:"set"`
}

type Source struct {
	Kind       string   `yaml:"kind" json:"kind"`
	Original   string   `yaml:"original,omitempty" json:"original,omitempty"`
	Author     string   `yaml:"author,omitempty" json:"author,omitempty"`
	URL        string   `yaml:"url,omitempty" json:"url,omitempty"`
	ImportedAt string   `yaml:"importedAt,omitempty" json:"importedAt,omitempty"`
	Warnings   []string `yaml:"warnings,omitempty" json:"warnings,omitempty"`
}

type Stop struct {
	Command    string `yaml:"command,omitempty" json:"command,omitempty"`
	TimeoutSec int    `yaml:"timeoutSec,omitempty" json:"timeoutSec,omitempty"`
}

type Port struct {
	Name     string `yaml:"name" json:"name"`
	Port     string `yaml:"port" json:"port"`
	Protocol string `yaml:"protocol" json:"protocol"`
}

type Query struct {
	Kind string `yaml:"kind" json:"kind"`
	Port string `yaml:"port" json:"port"`
}

type Command struct {
	Name string `yaml:"name" json:"name"`
	Args string `yaml:"args,omitempty" json:"args,omitempty"`
	Help Text   `yaml:"help,omitempty" json:"help,omitempty"`
}

type ConfigDefault struct {
	Files []string `yaml:"files" json:"files"`
	Key   string   `yaml:"key" json:"key"`
	Line  string   `yaml:"line" json:"line"`
}

type Console struct {
	Commands []Command `yaml:"commands,omitempty" json:"commands,omitempty"`
}

type Template struct {
	ID       string           `yaml:"id" json:"id"`
	Name     string           `yaml:"name" json:"name"`
	Category servers.Category `yaml:"category" json:"category"`
	Icon     string           `yaml:"icon,omitempty" json:"icon,omitempty"`

	Group string `yaml:"group,omitempty" json:"group,omitempty"`

	Order       int        `yaml:"order,omitempty" json:"order,omitempty"`
	Description Text       `yaml:"description,omitempty" json:"description,omitempty"`
	Variables   []Variable `yaml:"variables" json:"variables"`
	Runtime     Runtime    `yaml:"runtime" json:"runtime"`
	Install     []Step     `yaml:"install" json:"install"`
	Start       Start      `yaml:"start" json:"start"`
	Stop        Stop       `yaml:"stop" json:"stop"`
	ReadyRegex  string     `yaml:"readyRegex,omitempty" json:"readyRegex,omitempty"`
	Ports       []Port     `yaml:"ports,omitempty" json:"ports,omitempty"`
	WarnCommand string     `yaml:"warnCommand,omitempty" json:"warnCommand,omitempty"`
	Query       *Query     `yaml:"query,omitempty" json:"query,omitempty"`
	Console     Console    `yaml:"console,omitempty" json:"console"`

	ConfigDefaults []ConfigDefault `yaml:"configDefaults,omitempty" json:"configDefaults,omitempty"`

	ConfigFiles []ConfigFile `yaml:"configFiles,omitempty" json:"configFiles,omitempty"`

	Source *Source `yaml:"source,omitempty" json:"source,omitempty"`

	Builtin bool `yaml:"-" json:"builtin"`

	Raw []byte `yaml:"-" json:"-"`
}

var (
	idRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
	keyRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	groupRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

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
	if t.Group != "" && !groupRe.MatchString(t.Group) {
		return fmt.Errorf("template %s: group %q must be lowercase letters, digits and dashes", t.ID, t.Group)
	}
	if t.Order < 0 || t.Order > 10000 {
		return fmt.Errorf("template %s: order must be 0-10000", t.ID)
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
			for _, k := range s.Extract.Keep {
				if _, err := path.Match(k, ""); err != nil || safeRelPath(k) != nil {
					return fmt.Errorf("template %s: install step %d: keep pattern %q is not a relative glob", t.ID, i+1, k)
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
			if s.Exec.IfMissing != "" {
				if err := safeRelPath(s.Exec.IfMissing); err != nil {
					return fmt.Errorf("template %s: install step %d: ifMissing: %w", t.ID, i+1, err)
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

	for _, s := range t.allTemplatedStrings() {
		for _, k := range placeholders(s) {
			if !seen[k] && !isBuiltinKey(k) {
				return fmt.Errorf("template %s: unknown placeholder {{%s}}", t.ID, k)
			}
		}
	}
	return nil
}

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
			out = append(out, s.Exec.Command, s.Exec.Dir, s.Exec.IfExists, s.Exec.IfMissing)
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

const (
	KeyJava     = "JAVA"
	KeyJVMFlags = "JVM_FLAGS"
	KeyNode     = "NODE"
	KeyNPM      = "NPM"
	KeyNPX      = "NPX"
	KeyPython   = "PYTHON"
	KeyPip      = "PIP"
	KeyMessage  = "message"

	KeyServerDir  = "SERVER_DIR"
	KeyRuntimeBin = "RUNTIME_BIN"
	KeyJavaHome   = "JAVA_HOME"

	KeyMCVersion     = "MC_VERSION_RESOLVED"
	KeyLoaderVersion = "LOADER_VERSION"
)

func isBuiltinKey(k string) bool {
	switch k {
	case KeyJava, KeyJVMFlags, KeyNode, KeyNPM, KeyNPX, KeyPython, KeyPip, KeyMessage, KeyQueryKind, KeyQueryPort, KeyServerDir, KeyRuntimeBin, KeyJavaHome, KeyMCVersion, KeyLoaderVersion:
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

var ErrMissingVariable = errors.New("missing variable")

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

type ValueError struct {
	Key     string
	Message string
	Consent bool
}

func (e *ValueError) Error() string { return e.Key + " " + e.Message }

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

func (t *Template) Variable(key string) *Variable {
	for i := range t.Variables {
		if t.Variables[i].Key == key {
			return &t.Variables[i]
		}
	}
	return nil
}

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

type Registry struct {
	mu        sync.RWMutex
	builtinFS fs.FS
	userDir   string
	templates map[string]*Template
	builtins  map[string]*Template

	overrides map[string]bool
}

func Load(builtin fs.FS, userDir string) (*Registry, error) {
	r := &Registry{builtinFS: builtin, userDir: userDir}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

var loadErrors = map[string]string{}

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

func (r *Registry) Get(id string) (*Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.templates[id]
	return t, ok
}

func (r *Registry) Builtin(id string) (*Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.builtins[id]
	return t, ok
}

func (r *Registry) Overrides(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.overrides[id]
}

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

var ErrNoUserDir = errors.New("no directory for user templates is configured")

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

func (r *Registry) IsUser(id string) bool {
	if r.userDir == "" || !idRe.MatchString(id) {
		return false
	}
	_, err := os.Stat(filepath.Join(r.userDir, id+".yaml"))
	return err == nil
}

func (r *Registry) Delete(id string) error {
	if !r.IsUser(id) {
		return os.ErrNotExist
	}
	if err := os.Remove(filepath.Join(r.userDir, id+".yaml")); err != nil {
		return err
	}
	return r.reload()
}

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

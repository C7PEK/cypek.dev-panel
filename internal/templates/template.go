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

	"gopkg.in/yaml.v3"

	"github.com/cypek019/cypek.dev---panel/internal/servers"
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
}

// Runtime names the interpreter the server needs.
type Runtime struct {
	Kind    string `yaml:"kind" json:"kind"`                           // java | node | python | none
	Version string `yaml:"version,omitempty" json:"version,omitempty"` // auto or a major version
}

// Step is one install step; exactly one field is set.
type Step struct {
	Download   *DownloadStep   `yaml:"download,omitempty" json:"download,omitempty"`
	Write      *WriteStep      `yaml:"write,omitempty" json:"write,omitempty"`
	Properties *PropertiesStep `yaml:"properties,omitempty" json:"properties,omitempty"`
}

// DownloadStep fetches an artifact through a named provider.
type DownloadStep struct {
	Provider string `yaml:"provider" json:"provider"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty"`
	To       string `yaml:"to" json:"to"`
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
	Command string            `yaml:"command" json:"command"`
	Args    []string          `yaml:"args" json:"args"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
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
	// Builtin is true for templates embedded in the binary.
	Builtin bool `yaml:"-" json:"builtin"`
}

var (
	idRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
	keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
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
			return fmt.Errorf("template %s: variable key %q must look like MY_VAR", t.ID, v.Key)
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
			if err := safeRelPath(s.Download.To); err != nil {
				return fmt.Errorf("template %s: install step %d: %w", t.ID, i+1, err)
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
		if n != 1 {
			return fmt.Errorf("template %s: install step %d must have exactly one of download, write, properties", t.ID, i+1)
		}
	}
	if t.Start.Command == "" {
		return fmt.Errorf("template %s: start.command is required", t.ID)
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
	out := []string{t.Start.Command, t.WarnCommand}
	out = append(out, t.Start.Args...)
	for _, v := range t.Start.Env {
		out = append(out, v)
	}
	for _, s := range t.Install {
		if s.Download != nil {
			out = append(out, s.Download.Version, s.Download.To)
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
	KeyPython   = "PYTHON"
	KeyMessage  = "message" // warnCommand only
)

func isBuiltinKey(k string) bool {
	switch k {
	case KeyJava, KeyJVMFlags, KeyNode, KeyPython, KeyMessage:
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

// Registry holds the built-in and user templates.
type Registry struct {
	templates map[string]*Template
}

// Load reads built-in templates from builtin and user templates (*.yaml)
// from userDir, which may not exist. A user template with the same id as a
// built-in one overrides it.
func Load(builtin fs.FS, userDir string) (*Registry, error) {
	r := &Registry{templates: map[string]*Template{}}
	entries, err := fs.ReadDir(builtin, ".")
	if err != nil {
		return nil, fmt.Errorf("read built-in templates: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := fs.ReadFile(builtin, e.Name())
		if err != nil {
			return nil, err
		}
		t, err := Parse(b)
		if err != nil {
			return nil, fmt.Errorf("built-in %s: %w", e.Name(), err)
		}
		t.Builtin = true
		r.templates[t.ID] = t
	}
	if userDir != "" {
		files, _ := filepath.Glob(filepath.Join(userDir, "*.yaml"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			t, err := Parse(b)
			if err != nil {
				return nil, fmt.Errorf("user template %s: %w", filepath.Base(f), err)
			}
			r.templates[t.ID] = t
		}
	}
	return r, nil
}

// Get returns a template by id.
func (r *Registry) Get(id string) (*Template, bool) {
	t, ok := r.templates[id]
	return t, ok
}

// List returns templates sorted by name.
func (r *Registry) List() []*Template {
	out := make([]*Template, 0, len(r.templates))
	for _, t := range r.templates {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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

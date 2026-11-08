// Package importer converts templates from other panels (Pterodactyl and
// Pelican eggs, PufferPanel templates) into this panel's YAML template
// format. The conversion is best-effort: anything that cannot be mapped is
// reported as a warning so the admin can review the result before saving.
package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cypek019/cypek.dev---panel/internal/servers"
	"github.com/cypek019/cypek.dev---panel/internal/templates"
)

// Formats reported in Result.Format.
const (
	FormatPterodactyl = "pterodactyl"
	FormatPelican     = "pelican"
	FormatPuffer      = "pufferpanel"
	FormatNative      = "homepanel"
)

// MaxSize bounds imported documents (real eggs are a few KB).
const MaxSize = 2 << 20

// Result is a converted template ready for review.
type Result struct {
	YAML     []byte
	Format   string
	Warnings []string
	Template *templates.Template
}

// ErrUnknownFormat means the document is none of the supported formats.
var ErrUnknownFormat = errors.New("this file is not a Pterodactyl/Pelican egg, a PufferPanel template or a panel template")

// Options carries metadata that is not part of the document itself.
type Options struct {
	SourceURL string
	Now       time.Time
}

// Import detects the format of data and converts it.
func Import(data []byte, opts Options) (*Result, error) {
	if len(data) > MaxSize {
		return nil, fmt.Errorf("the file is larger than %d MB", MaxSize>>20)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	doc, err := decodeDoc(data)
	if err != nil {
		return nil, err
	}
	format := detect(doc)
	if format == FormatPterodactyl || format == FormatPelican {
		if imgs := orderedField(data, "docker_images"); imgs != nil {
			doc["docker_images"] = imgs
		}
	}
	var c *conv
	switch format {
	case FormatPterodactyl:
		c, err = fromEgg(doc, FormatPterodactyl)
	case FormatPelican:
		c, err = fromEgg(doc, FormatPelican)
	case FormatPuffer:
		c, err = fromPuffer(doc)
	case FormatNative:
		t, perr := templates.Parse(data)
		if perr != nil {
			return nil, perr
		}
		return &Result{YAML: data, Format: FormatNative, Template: t}, nil
	default:
		return nil, ErrUnknownFormat
	}
	if err != nil {
		return nil, err
	}
	return c.finish(opts)
}

// decodeDoc reads JSON (eggs, PufferPanel) or YAML (newer Pelican exports,
// native templates) into generic maps.
func decodeDoc(data []byte) (map[string]any, error) {
	trimmed := bytes.TrimSpace(data)
	var doc map[string]any
	if len(trimmed) > 0 && trimmed[0] == '{' {
		d := json.NewDecoder(bytes.NewReader(trimmed))
		d.UseNumber()
		if err := d.Decode(&doc); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return doc, nil
	}
	if err := yaml.Unmarshal(trimmed, &doc); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if doc == nil {
		return nil, ErrUnknownFormat
	}
	return doc, nil
}

func detect(doc map[string]any) string {
	if meta, ok := doc["meta"].(map[string]any); ok {
		v := str(meta["version"])
		comment := strings.ToUpper(str(doc["_comment"]))
		switch {
		case strings.HasPrefix(v, "PLCN"), strings.Contains(comment, "PELICAN"):
			return FormatPelican
		case strings.HasPrefix(v, "PTDL"):
			return FormatPterodactyl
		}
	}
	if _, ok := doc["pufferd"]; ok {
		return FormatPuffer
	}
	if _, ok := doc["run"]; ok {
		if _, ok := doc["install"]; ok {
			return FormatPuffer
		}
		if _, ok := doc["data"]; ok {
			return FormatPuffer
		}
	}
	if _, ok := doc["id"]; ok {
		if _, ok := doc["start"]; ok {
			return FormatNative
		}
	}
	return ""
}

// conv accumulates one conversion.
type conv struct {
	t        templates.Template
	kind     string
	original string
	author   string
	warnings []string
}

func (c *conv) warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, w := range c.warnings {
		if w == msg {
			return
		}
	}
	c.warnings = append(c.warnings, msg)
}

func (c *conv) hasVar(key string) bool {
	for _, v := range c.t.Variables {
		if v.Key == key {
			return true
		}
	}
	return false
}

var anyPlaceholder = regexp.MustCompile(`\{\{([A-Za-z][A-Za-z0-9_]*)(\.\.\.)?\}\}`)

// finish adds hidden variables for leftover placeholders, stamps the source
// and renders + re-parses the YAML so the result is known to be loadable.
func (c *conv) finish(opts Options) (*Result, error) {
	c.t.Source = &templates.Source{
		Kind:       c.kind,
		Original:   c.original,
		Author:     c.author,
		URL:        opts.SourceURL,
		ImportedAt: opts.Now.UTC().Format(time.RFC3339),
	}
	for _, s := range c.strings() {
		for _, m := range anyPlaceholder.FindAllStringSubmatch(s, -1) {
			k := m[1]
			if c.hasVar(k) || isBuiltin(k) {
				continue
			}
			c.t.Variables = append(c.t.Variables, templates.Variable{
				Key: k, Label: templates.Text{"en": k}, Type: templates.TypeString, Hidden: true,
			})
			c.warn("{{%s}} is used but not defined in the original; it was added as an empty hidden variable", k)
		}
	}
	if len(c.warnings) > 0 {
		c.t.Source.Warnings = c.warnings
	}
	if !c.t.Category.Valid() {
		c.t.Category = servers.CategoryOther
	}
	body, err := yaml.Marshal(&c.t)
	if err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}
	var head strings.Builder
	fmt.Fprintf(&head, "# Imported from %s: %q", formatName(c.kind), c.original)
	if c.author != "" {
		fmt.Fprintf(&head, " by %s", c.author)
	}
	head.WriteString("\n# Review the install script and start command before using it.\n")
	out := append([]byte(head.String()), body...)
	t, err := templates.Parse(out)
	if err != nil {
		return nil, fmt.Errorf("the converted template is not valid (%w); edit it by hand after import", err)
	}
	return &Result{YAML: out, Format: c.kind, Warnings: c.warnings, Template: t}, nil
}

func (c *conv) strings() []string {
	out := []string{c.t.Start.Command, c.t.Start.Shell, c.t.WarnCommand, c.t.Runtime.Version}
	out = append(out, c.t.Start.Args...)
	for _, p := range c.t.Ports {
		out = append(out, p.Port)
	}
	if c.t.Query != nil {
		out = append(out, c.t.Query.Port)
	}
	for _, f := range c.t.ConfigFiles {
		for k, v := range f.Set {
			out = append(out, k, v)
		}
	}
	for _, s := range c.t.Install {
		if s.Write != nil {
			out = append(out, s.Write.Path, s.Write.Content)
		}
	}
	return out
}

func isBuiltin(k string) bool {
	switch k {
	case templates.KeyJava, templates.KeyJVMFlags, templates.KeyNode, templates.KeyNPM, templates.KeyNPX,
		templates.KeyPython, templates.KeyPip, templates.KeyServerDir, templates.KeyRuntimeBin, templates.KeyJavaHome:
		return true
	}
	return false
}

func formatName(kind string) string {
	switch kind {
	case FormatPterodactyl:
		return "Pterodactyl"
	case FormatPelican:
		return "Pelican"
	case FormatPuffer:
		return "PufferPanel"
	}
	return kind
}

// --- shared helpers ---------------------------------------------------------

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprint(x)
	case float64:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%f", x), "0"), ".")
	}
	return fmt.Sprint(v)
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "1" || strings.EqualFold(x, "true")
	case json.Number:
		return x.String() != "0"
	case int:
		return x != 0
	case float64:
		return x != 0
	}
	return false
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugID(prefix, name string) string {
	s := nonSlug.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "template"
	}
	id := prefix + "-" + s
	if len(id) > 60 {
		id = strings.TrimRight(id[:60], "-")
	}
	return id
}

var (
	tagRe   = regexp.MustCompile(`<[^>]*>`)
	spaceRe = regexp.MustCompile(`[ \t]+`)
)

// plainText strips HTML (PufferPanel descriptions) and CRLFs.
func plainText(s string) string {
	s = tagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// tidyScript normalises line endings and trailing blanks so the YAML uses a
// readable block scalar.
func tidyScript(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

var secretKey = regexp.MustCompile(`(?i)(token|password|passwd|secret|api_?key|licen[sc]e_?key|_pass$|^pass)`)

func looksSecret(key string) bool { return secretKey.MatchString(key) }

func guessCategory(texts ...string) servers.Category {
	all := strings.ToLower(strings.Join(texts, " "))
	switch {
	case strings.Contains(all, "fivem") || strings.Contains(all, "fxserver") || strings.Contains(all, "redm"):
		return servers.CategoryFiveM
	case regexp.MustCompile(`minecraft|paper|spigot|bukkit|fabric|forge|purpur|velocity|bungee|waterfall|folia`).MatchString(all):
		return servers.CategoryMinecraft
	case strings.Contains(all, "discord") || regexp.MustCompile(`\bbots?\b`).MatchString(all):
		return servers.CategoryDiscord
	}
	return servers.CategoryOther
}

// javaVersions extracts the Java feature version from container image names
// such as ghcr.io/ptero-eggs/yolks:java_21 or eclipse-temurin:17.
var (
	javaImage   = regexp.MustCompile(`(?i)(?:java|jdk|jre|temurin|openjdk|graalvm)[_:-]?(\d{1,2})\b`)
	nodeImage   = regexp.MustCompile(`(?i)(?:nodejs|node)[_:-]?(\d{2})\b`)
	pythonImage = regexp.MustCompile(`(?i)python[_:-]?(3\.\d{1,2})\b`)
)

type imageRuntime struct {
	kind    string
	version string
}

func runtimeFromImage(image string) (imageRuntime, bool) {
	if m := javaImage.FindStringSubmatch(image); m != nil {
		return imageRuntime{"java", m[1]}, true
	}
	if m := nodeImage.FindStringSubmatch(image); m != nil {
		return imageRuntime{"node", m[1]}, true
	}
	if m := pythonImage.FindStringSubmatch(image); m != nil {
		return imageRuntime{"python", m[1]}, true
	}
	return imageRuntime{}, false
}

// sortVersionsDesc orders "25", "21", "8" newest first.
func sortVersionsDesc(vs []string) {
	sort.SliceStable(vs, func(i, j int) bool { return versionLess(vs[j], vs[i]) })
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		var x, y int
		fmt.Sscan(pa[i], &x)
		fmt.Sscan(pb[i], &y)
		if x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

func f64(v float64) *float64 { return &v }

// orderedField reads one top-level object field keeping key order, which a
// generic map decode loses (an egg's first docker image is its default).
func orderedField(data []byte, field string) orderedMap {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var top map[string]json.RawMessage
		if json.Unmarshal(trimmed, &top) != nil || top[field] == nil {
			return nil
		}
		d := json.NewDecoder(bytes.NewReader(top[field]))
		if t, err := d.Token(); err != nil || t != json.Delim('{') {
			return nil
		}
		var out orderedMap
		for d.More() {
			kt, err := d.Token()
			if err != nil {
				return nil
			}
			var v any
			if err := d.Decode(&v); err != nil {
				return nil
			}
			out = append(out, kv{str(kt), str(v)})
		}
		return out
	}
	var root yaml.Node
	if yaml.Unmarshal(trimmed, &root) != nil || len(root.Content) == 0 {
		return nil
	}
	m := root.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != field || m.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		var out orderedMap
		n := m.Content[i+1]
		for j := 0; j+1 < len(n.Content); j += 2 {
			out = append(out, kv{n.Content[j].Value, n.Content[j+1].Value})
		}
		return out
	}
	return nil
}

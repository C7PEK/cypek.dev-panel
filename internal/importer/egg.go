package importer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cypek019/cypek.dev---panel/internal/servers"
	"github.com/cypek019/cypek.dev---panel/internal/templates"
)

// Synthetic variables standing in for what Wings injects from the server's
// allocation and limits.
const (
	keyPort   = "SERVER_PORT"
	keyMemory = "SERVER_MEMORY"
	keyIP     = "SERVER_IP"
)

// eggPlaceholders maps Wings' dotted placeholders to plain variables.
var eggPlaceholders = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`\{\{\s*server\.(?:build\.default|allocations\.default)\.port\s*\}\}`), "{{" + keyPort + "}}"},
	{regexp.MustCompile(`\{\{\s*server\.(?:build\.default|allocations\.default)\.ip\s*\}\}`), "{{" + keyIP + "}}"},
	{regexp.MustCompile(`\{\{\s*server\.build\.memory\s*\}\}`), "{{" + keyMemory + "}}"},
	{regexp.MustCompile(`\{\{\s*(?:server\.build\.env|env)\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`), "{{$1}}"},
	{regexp.MustCompile(`\{\{\s*config\.docker\.interface\s*\}\}`), "127.0.0.1"},
	{regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`), "{{$1}}"},
}

var leftoverDotted = regexp.MustCompile(`\{\{\s*[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+\s*\}\}`)

func (c *conv) eggText(s string) string {
	for _, p := range eggPlaceholders {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return leftoverDotted.ReplaceAllStringFunc(s, func(m string) string {
		c.warn("the placeholder %s has no equivalent here and was removed", m)
		return ""
	})
}

// Container paths and binaries of the Pterodactyl "yolks" images.
var yolkPaths = strings.NewReplacer(
	"/home/container", "{{SERVER_DIR}}",
	"/usr/local/bin/python3", "python3",
	"/usr/local/bin/python", "python",
	"/usr/local/bin/pip3", "pip3",
	"/usr/local/bin/pip", "pip",
	"/usr/local/bin/node", "node",
	"/usr/local/bin/npm", "npm",
)

func fromEgg(doc map[string]any, kind string) (*conv, error) {
	c := &conv{kind: kind}
	name := strings.TrimSpace(str(doc["name"]))
	if name == "" {
		return nil, fmt.Errorf("the egg has no name")
	}
	c.original, c.author = name, str(doc["author"])
	prefix := "egg"
	if kind == FormatPelican {
		prefix = "pelican"
	}
	c.t.ID = slugID(prefix, name)
	c.t.Name = name
	if d := plainText(str(doc["description"])); d != "" {
		c.t.Description = templates.Text{"en": d}
	}

	// Variables first: everything else may reference them.
	vars, _ := doc["variables"].([]any)
	for _, raw := range vars {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := c.eggVariable(m); ok {
			c.t.Variables = append(c.t.Variables, v)
		}
	}

	startup := str(doc["startup"])
	if startup == "" {
		// Pelican: startup_commands is a name -> command map; take the first.
		if sc, ok := doc["startup_commands"].(map[string]any); ok && len(sc) > 0 {
			names := make([]string, 0, len(sc))
			for k := range sc {
				names = append(names, k)
			}
			sort.Strings(names)
			pick := names[0]
			for _, n := range names {
				if strings.EqualFold(n, "default") {
					pick = n
				}
			}
			startup = str(sc[pick])
			if len(names) > 1 {
				c.warn("the egg offers several start commands; %q was used (others: edit start.shell)", pick)
			}
		}
	}
	if strings.TrimSpace(startup) == "" {
		return nil, fmt.Errorf("the egg has no startup command")
	}
	images := orderedImages(doc["docker_images"])
	c.t.Category = guessCategory(name, startup, strings.Join(imageValues(images), " "))
	c.eggRuntime(images)

	shell := yolkPaths.Replace(c.eggText(strings.ReplaceAll(startup, "\r\n", "\n")))
	c.t.Start.Shell = shell
	if strings.Contains(shell, "MaxRAMPercentage") {
		c.warn("the start command sizes Java by MaxRAMPercentage: set a RAM limit for the server (Settings), otherwise Java takes a share of the whole host's memory")
	}

	cfg, _ := doc["config"].(map[string]any)
	c.eggStop(str(cfg["stop"]))
	c.eggReady(jsonish(cfg["startup"]))
	c.eggConfigFiles(jsonish(cfg["files"]))
	if logs := jsonish(cfg["logs"]); logs != nil {
		if m, ok := logs.(map[string]any); ok && len(m) > 0 {
			c.warn("custom log settings of the egg are ignored (the console log is kept by the panel)")
		}
	}

	// Install script.
	if scripts, ok := doc["scripts"].(map[string]any); ok {
		if inst, ok := scripts["installation"].(map[string]any); ok {
			if body := strings.TrimSpace(str(inst["script"])); body != "" {
				c.t.Install = append(c.t.Install, templates.Step{Script: &templates.ScriptStep{Content: tidyScript(body)}})
				if ep := str(inst["entrypoint"]); ep != "" && ep != "bash" && ep != "ash" && ep != "sh" && ep != "/bin/bash" && ep != "/bin/ash" && ep != "/bin/sh" {
					c.warn("the install script was written for %q; it runs with bash here", ep)
				}
				c.warn("the install script runs on this host as the panel user (no root, sandboxed to the server folder); package installs (apt/apk) are skipped, so tools it needs (e.g. curl, jq, unzip) must already be installed")
			}
		}
	}

	features := map[string]bool{}
	if fs, ok := doc["features"].([]any); ok {
		for _, f := range fs {
			features[strings.ToLower(str(f))] = true
		}
	}
	if features["eula"] || c.t.Category == servers.CategoryMinecraft && strings.Contains(strings.ToLower(shell), ".jar") {
		c.addEULA()
	}

	c.addSyntheticVars()
	return c, nil
}

func (c *conv) addEULA() {
	if c.hasVar("EULA") {
		return
	}
	c.t.Variables = append(c.t.Variables, templates.Variable{
		Key:      "EULA",
		Label:    templates.Text{"pl": "Akceptuję EULA Minecrafta (aka.ms/MinecraftEULA)", "en": "I accept the Minecraft EULA (aka.ms/MinecraftEULA)"},
		Type:     templates.TypeBool,
		Default:  "false",
		Required: true,
		Consent:  true,
	})
	c.t.Install = append(c.t.Install, templates.Step{Write: &templates.WriteStep{Path: "eula.txt", Content: "eula=true\n", RequireConsent: "EULA"}})
}

// addSyntheticVars provides the port / memory / IP Wings would inject, but
// only when the egg actually uses them.
func (c *conv) addSyntheticVars() {
	used := func(key string) bool {
		for _, s := range append(c.strings(), c.scriptText()) {
			if strings.Contains(s, key) {
				return true
			}
		}
		return false
	}
	var extra []templates.Variable
	if used(keyPort) && !c.hasVar(keyPort) {
		v := templates.Variable{Key: keyPort, Label: templates.Text{"pl": "Port serwera", "en": "Server port"}, Type: templates.TypeNumber, Required: true, Min: f64(1), Max: f64(65535)}
		if c.t.Category == servers.CategoryMinecraft {
			v.Default = "25565"
		} else if c.t.Category == servers.CategoryFiveM {
			v.Default = "30120"
		}
		extra = append(extra, v)
	}
	if used(keyMemory) && !c.hasVar(keyMemory) {
		extra = append(extra, templates.Variable{Key: keyMemory, Label: templates.Text{"pl": "Pamięć (MB)", "en": "Memory (MB)"}, Type: templates.TypeNumber, Default: "2048", Min: f64(128), Required: true})
	}
	if used(keyIP) && !c.hasVar(keyIP) {
		extra = append(extra, templates.Variable{Key: keyIP, Label: templates.Text{"en": "Bind address"}, Type: templates.TypeString, Default: "0.0.0.0", Hidden: true})
	}
	c.t.Variables = append(extra, c.t.Variables...)
	if c.hasVar(keyPort) {
		proto := "tcp"
		if c.t.Category != servers.CategoryMinecraft {
			proto = "tcp+udp"
		}
		c.t.Ports = []templates.Port{{Name: "game", Port: "{{" + keyPort + "}}", Protocol: proto}}
		if c.t.Category == servers.CategoryMinecraft {
			c.t.Query = &templates.Query{Kind: "minecraft-ping", Port: "{{" + keyPort + "}}"}
		}
	}
}

func (c *conv) scriptText() string {
	var b strings.Builder
	for _, s := range c.t.Install {
		if s.Script != nil {
			b.WriteString(s.Script.Content)
		}
	}
	return b.String()
}

func (c *conv) eggVariable(m map[string]any) (templates.Variable, bool) {
	key := strings.TrimSpace(str(m["env_variable"]))
	if !validKey(key) {
		c.warn("variable %q has an unusable name and was skipped", key)
		return templates.Variable{}, false
	}
	v := templates.Variable{Key: key, Type: templates.TypeString}
	label := strings.TrimSpace(str(m["name"]))
	if label == "" {
		label = key
	}
	v.Label = templates.Text{"en": label}
	if d := plainText(str(m["description"])); d != "" {
		v.Help = templates.Text{"en": d}
	}
	v.Default = str(m["default_value"])
	if vis, ok := m["user_viewable"]; ok && !truthy(vis) {
		v.Hidden = true
	}
	applyRules(c, &v, parseRules(m["rules"]))
	if v.Type == templates.TypeString && looksSecret(key) {
		v.Type = templates.TypeSecret
	}
	if v.Hidden && v.Required && v.Default == "" {
		v.Required = false
	}
	return v, true
}

var keyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

func validKey(k string) bool { return keyRe.MatchString(k) }

var knownRule = regexp.MustCompile(`^(required|nullable|sometimes|string|integer|numeric|boolean|bool|alpha_dash|alpha_num|alpha|url|ip|email|digits|min|max|between|in|not_in|size|regex|not_regex|digits_between)(:|$)`)

// parseRules splits Laravel validation rules, keeping "|" inside regex rules.
func parseRules(raw any) []string {
	var parts []string
	switch r := raw.(type) {
	case []any:
		for _, x := range r {
			parts = append(parts, str(x))
		}
		return parts
	case string:
		tokens := strings.Split(r, "|")
		for i := 0; i < len(tokens); i++ {
			t := tokens[i]
			if strings.HasPrefix(t, "regex:") || strings.HasPrefix(t, "not_regex:") {
				for i+1 < len(tokens) && !knownRule.MatchString(tokens[i+1]) {
					i++
					t += "|" + tokens[i]
				}
			}
			parts = append(parts, t)
		}
	}
	return parts
}

func applyRules(c *conv, v *templates.Variable, rules []string) {
	nullable := false
	numeric := false
	var minV, maxV *float64
	for _, r := range rules {
		name, arg, _ := strings.Cut(strings.TrimSpace(r), ":")
		switch name {
		case "required":
			v.Required = true
		case "nullable", "sometimes":
			nullable = true
		case "integer", "numeric", "digits":
			numeric = true
		case "boolean", "bool":
			if v.Default == "true" || v.Default == "false" {
				v.Type = templates.TypeBool
			} else {
				v.Type = templates.TypeSelect
				v.Options = []string{"0", "1"}
			}
		case "in":
			opts := strings.Split(arg, ",")
			for i := range opts {
				opts[i] = strings.Trim(strings.TrimSpace(opts[i]), `"'`)
			}
			v.Type = templates.TypeSelect
			v.Options = opts
			if v.Default != "" && !contains(opts, v.Default) {
				v.Options = append([]string{v.Default}, opts...)
			}
		case "min":
			if f, err := strconv.ParseFloat(arg, 64); err == nil {
				minV = f64(f)
			}
		case "max":
			if f, err := strconv.ParseFloat(arg, 64); err == nil {
				maxV = f64(f)
			}
		case "between":
			a, b, _ := strings.Cut(arg, ",")
			if fa, err := strconv.ParseFloat(a, 64); err == nil {
				minV = f64(fa)
			}
			if fb, err := strconv.ParseFloat(b, 64); err == nil {
				maxV = f64(fb)
			}
		case "regex":
			if p, ok := phpRegex(arg); ok {
				v.Pattern = p
			} else {
				c.warn("variable %s: its validation pattern uses syntax this panel does not support and was dropped", v.Key)
			}
		}
	}
	if nullable {
		v.Required = false
	}
	if numeric && v.Type == templates.TypeString {
		v.Type = templates.TypeNumber
		if v.Default != "" {
			if _, err := strconv.ParseFloat(v.Default, 64); err != nil {
				v.Type = templates.TypeString
			}
		}
	}
	switch v.Type {
	case templates.TypeNumber:
		v.Min, v.Max = minV, maxV
	case templates.TypeString, templates.TypeSecret:
		if maxV != nil && *maxV > 0 {
			v.MaxLength = int(*maxV)
		}
	}
	// A pattern on a select/bool is redundant and may conflict.
	if v.Type == templates.TypeSelect || v.Type == templates.TypeBool {
		v.Pattern = ""
	}
	// Pterodactyl accepts an empty optional value even if it fails the
	// pattern; keep that by allowing empty.
	if v.Pattern != "" && !v.Required {
		v.Pattern = "^$|" + "(?:" + v.Pattern + ")"
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// phpRegex converts "/body/flags" to a Go regexp.
func phpRegex(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return "", false
	}
	delim := s[0]
	end := strings.LastIndexByte(s, delim)
	if end <= 0 {
		return "", false
	}
	body, flags := s[1:end], s[end+1:]
	prefix := ""
	for _, f := range flags {
		switch f {
		case 'i':
			prefix += "i"
		case 'm':
			prefix += "m"
		case 's':
			prefix += "s"
		case 'u', 'D':
		default:
			return "", false
		}
	}
	if prefix != "" {
		body = "(?" + prefix + ")" + body
	}
	if _, err := regexp.Compile(body); err != nil {
		return "", false
	}
	return body, true
}

func (c *conv) eggStop(stop string) {
	stop = strings.TrimSpace(stop)
	switch stop {
	case "":
		c.t.Stop.Command = ""
	case "^C", "^^C":
		c.t.Stop.Command = "^C"
	default:
		if strings.HasPrefix(stop, "^") {
			c.warn("stop signal %q is not supported; the server gets SIGTERM", stop)
			c.t.Stop.Command = ""
		} else {
			c.t.Stop.Command = stop
		}
	}
	c.t.Stop.TimeoutSec = 60
}

func (c *conv) eggReady(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	var parts []string
	switch d := m["done"].(type) {
	case string:
		if d != "" {
			parts = append(parts, regexp.QuoteMeta(d))
		}
	case []any:
		for _, x := range d {
			if s := str(x); s != "" {
				parts = append(parts, regexp.QuoteMeta(s))
			}
		}
	}
	c.t.ReadyRegex = strings.Join(parts, "|")
	if _, ok := m["userInteraction"]; ok {
		c.warn("the egg's startup userInteraction hints are ignored")
	}
}

var parserMap = map[string]string{"properties": "properties", "ini": "ini", "yaml": "yaml", "yml": "yaml", "json": "json", "file": "file"}

func (c *conv) eggConfigFiles(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	paths := make([]string, 0, len(m))
	for p := range m {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		spec, _ := m[path].(map[string]any)
		parser := parserMap[strings.ToLower(str(spec["parser"]))]
		if parser == "" {
			c.warn("%s: the %q config parser is not supported; edit that file by hand", path, str(spec["parser"]))
			continue
		}
		if strings.ContainsAny(path, "*?[") || strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			c.warn("%s: wildcard or absolute config paths are not supported", path)
			continue
		}
		find, _ := spec["find"].(map[string]any)
		set := map[string]string{}
		for k, val := range find {
			if strings.ContainsAny(k, "*[]") {
				c.warn("%s: key %q uses wildcards/array indexes, which are not supported", path, k)
				continue
			}
			sv, isStr := val.(string)
			if !isStr {
				if _, isMap := val.(map[string]any); isMap {
					c.warn("%s: conditional replacement for %q is not supported", path, k)
					continue
				}
				sv = str(val)
			}
			set[c.eggText(k)] = strings.ReplaceAll(c.eggText(sv), "/home/container", "{{SERVER_DIR}}")
		}
		if len(set) > 0 {
			c.t.ConfigFiles = append(c.t.ConfigFiles, templates.ConfigFile{Path: path, Parser: parser, Set: set})
		}
	}
}

// eggRuntime maps the egg's container images to a host runtime.
func (c *conv) eggRuntime(images []kv) {
	var kind string
	versions := []string{}
	var unknown []string
	for _, img := range images {
		rt, ok := runtimeFromImage(img.value)
		if !ok {
			unknown = append(unknown, img.value)
			continue
		}
		if kind == "" {
			kind = rt.kind
		}
		if rt.kind == kind && !contains(versions, rt.version) {
			versions = append(versions, rt.version)
		}
	}
	if kind == "" {
		c.t.Runtime = templates.Runtime{Kind: "none"}
		if len(unknown) > 0 {
			c.warn("the egg runs in the container image %s; here it runs directly on the host, so programs that image provides (wine, mono, steamcmd, ...) must be installed on the host", unknown[0])
		}
		return
	}
	c.t.Runtime.Kind = kind
	defaultVersion := versions[0] // Pterodactyl's default image is the first one
	if len(versions) == 1 {
		c.t.Runtime.Version = defaultVersion
		return
	}
	key := map[string]string{"java": "JAVA_VERSION", "node": "NODE_VERSION", "python": "PYTHON_VERSION"}[kind]
	label := map[string]string{"java": "Java", "node": "Node.js", "python": "Python"}[kind]
	opts := append([]string(nil), versions...)
	sortVersionsDesc(opts)
	if !c.hasVar(key) {
		c.t.Variables = append([]templates.Variable{{
			Key: key, Label: templates.Text{"pl": "Wersja " + label, "en": label + " version"},
			Type: templates.TypeSelect, Options: opts, Default: defaultVersion, Required: true,
		}}, c.t.Variables...)
	}
	c.t.Runtime.Version = "{{" + key + "}}"
}

type kv struct{ key, value string }

// orderedImages keeps the images in file order (the first one is the
// egg's default). Pterodactyl v1 eggs used a plain list.
func orderedImages(v any) []kv {
	switch x := v.(type) {
	case []any:
		var out []kv
		for _, i := range x {
			out = append(out, kv{str(i), str(i)})
		}
		return out
	case orderedMap:
		return x
	case map[string]any:
		var out []kv
		for k, val := range x {
			out = append(out, kv{k, str(val)})
		}
		// Map order is lost after generic decoding; prefer newer versions.
		sort.SliceStable(out, func(i, j int) bool { return versionLess(imageVersion(out[j].value), imageVersion(out[i].value)) })
		return out
	}
	return nil
}

func imageVersion(img string) string {
	if rt, ok := runtimeFromImage(img); ok {
		return rt.version
	}
	return "0"
}

func imageValues(kvs []kv) []string {
	out := make([]string, len(kvs))
	for i, x := range kvs {
		out[i] = x.value
	}
	return out
}

type orderedMap []kv

// jsonish decodes Pterodactyl's JSON-in-a-string config fields; Pelican's
// YAML exports carry them as real objects.
func jsonish(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(s), &out); err == nil {
		return out
	}
	if err := yaml.Unmarshal([]byte(s), &out); err == nil {
		return out
	}
	return nil
}

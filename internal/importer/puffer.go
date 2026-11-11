package importer

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

func fromPuffer(doc map[string]any) (*conv, error) {
	if inner, ok := doc["pufferd"].(map[string]any); ok {
		doc = inner
	}
	c := &conv{kind: FormatPuffer}
	display := strings.TrimSpace(str(doc["display"]))
	typ := str(doc["type"])
	if display == "" {
		display = typ
	}
	if display == "" {
		return nil, fmt.Errorf("the template has no display name")
	}
	c.original = display
	idName := str(doc["name"])
	if idName == "" {
		idName = display
	}
	c.t.ID = slugID("puffer", idName)
	c.t.Name = display
	c.t.Category = guessCategory(typ, display)

	data, _ := doc["data"].(map[string]any)
	order := pufferVarOrder(doc, data)
	vars := map[string]bool{}
	emptyOpts := map[string]string{}
	for _, key := range order {
		m, _ := data[key].(map[string]any)
		if m == nil {
			continue
		}
		if !validKey(key) {
			c.warn("variable %q has an unusable name and was skipped", key)
			continue
		}
		v, stand := c.pufferVariable(key, m)
		if stand != "" {
			emptyOpts[key] = stand
		}
		vars[key] = true
		c.t.Variables = append(c.t.Variables, v)
	}

	var script strings.Builder
	writeEmptyOptionFix(&script, emptyOpts)
	javaVersion := ""
	usesJava := false
	installOps, _ := doc["install"].([]any)
	if len(installOps) == 0 {
		if inner, ok := doc["install"].(map[string]any); ok {
			installOps, _ = inner["commands"].([]any)
		}
	}
	for i, raw := range installOps {
		op, _ := raw.(map[string]any)
		if op == nil {
			continue
		}
		typ := str(op["type"])
		if typ == "javadl" {
			javaVersion = pufferRef(str(op["version"]))
			usesJava = true
			continue
		}
		lines, ok := c.pufferOp(typ, op)
		if !ok {
			lines = []string{fmt.Sprintf(`echo '[panel] PufferPanel step %s is not supported by this panel' >&2`, nonSlug.ReplaceAllString(strings.ToLower(typ), "")), "exit 1"}
			c.warn("install step %d (%s) is not supported; if the template reaches it the install stops with an error", i+1, typ)
		}
		if strings.Contains(strings.Join(lines, "\n"), "java") {
			usesJava = true
		}
		c.emitGuarded(&script, str(op["if"]), lines, vars, fmt.Sprintf("install step %d", i+1))
	}
	if body := strings.TrimSpace(script.String()); body != "" && body != strings.TrimSpace(emptyFix(emptyOpts)) {
		c.t.Install = append(c.t.Install, templates.Step{Script: &templates.ScriptStep{Content: tidyScript(script.String())}})
	}

	run, _ := doc["run"].(map[string]any)
	if run == nil {
		return nil, fmt.Errorf("the template has no run section")
	}
	shell := c.pufferRun(run, vars, emptyOpts)
	if strings.Contains(shell, "java") {
		usesJava = true
	}
	c.t.Start.Shell = shell

	switch {
	case usesJava:
		c.t.Runtime.Kind = "java"
		c.t.Runtime.Version = javaVersion
		if c.t.Runtime.Version == "" {
			c.t.Runtime.Version = "21"
			c.warn("the template does not say which Java it needs; Java 21 was assumed (change runtime.version if needed)")
		}
	case regexp.MustCompile(`\b(node|npm)\b`).MatchString(shell):
		c.t.Runtime = templates.Runtime{Kind: "node", Version: "lts"}
	case regexp.MustCompile(`\bpython3?\b`).MatchString(shell):
		c.t.Runtime = templates.Runtime{Kind: "python", Version: "3.12"}
	default:
		c.t.Runtime = templates.Runtime{Kind: "none"}
	}

	if vars["port"] {
		proto := "tcp"
		if c.t.Category != servers.CategoryMinecraft {
			proto = "tcp+udp"
		}
		c.t.Ports = []templates.Port{{Name: "game", Port: "{{port}}", Protocol: proto}}
	}
	if q, ok := doc["query"].(map[string]any); ok && strings.HasPrefix(str(q["type"]), "minecraft") && vars["port"] {
		c.t.Query = &templates.Query{Kind: "minecraft-ping", Port: "{{port}}"}
	}
	if c.t.Category == servers.CategoryMinecraft && !hasDoneRegex(c) {
		c.t.ReadyRegex = `Done \(\d+[.,]\d+s\)!`
	}
	if env, ok := doc["environment"].(map[string]any); ok && str(env["type"]) != "" && str(env["type"]) != "host" && str(env["type"]) != "tty" && str(env["type"]) != "standard" {
		c.warn("the template defaults to the %q environment; it runs directly on the host here", str(env["type"]))
	}
	return c, nil
}

func hasDoneRegex(c *conv) bool { return c.t.ReadyRegex != "" }

func pufferVarOrder(doc map[string]any, data map[string]any) []string {
	var out []string
	seen := map[string]bool{}
	if groups, ok := doc["groups"].([]any); ok {
		type grp struct {
			order int
			vars  []string
		}
		var gs []grp
		for _, g := range groups {
			m, _ := g.(map[string]any)
			if m == nil {
				continue
			}
			var o int
			fmt.Sscan(str(m["order"]), &o)
			var vs []string
			if l, ok := m["variables"].([]any); ok {
				for _, v := range l {
					vs = append(vs, str(v))
				}
			}
			gs = append(gs, grp{o, vs})
		}
		sort.SliceStable(gs, func(i, j int) bool { return gs[i].order < gs[j].order })
		for _, g := range gs {
			for _, v := range g.vars {
				if _, ok := data[v]; ok && !seen[v] {
					seen[v] = true
					out = append(out, v)
				}
			}
		}
	}
	var rest []string
	for k := range data {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func (c *conv) pufferVariable(key string, m map[string]any) (templates.Variable, string) {
	v := templates.Variable{Key: key, Type: templates.TypeString}
	label := strings.TrimSpace(str(m["display"]))
	if label == "" {
		label = key
	}
	v.Label = templates.Text{"en": label}
	if d := plainText(str(m["desc"])); d != "" {
		v.Help = templates.Text{"en": d}
	}
	v.Default = str(m["value"])
	v.Required = truthy(m["required"])
	if truthy(m["internal"]) {
		v.Hidden = true
	}
	stand := ""
	switch str(m["type"]) {
	case "boolean", "bool":
		v.Type = templates.TypeBool
		if v.Default == "" {
			v.Default = "false"
		}
		if strings.Contains(strings.ToLower(key), "eula") {
			v.Consent = true
			v.Required = true
			v.Default = "false"
		}
	case "integer", "number":
		v.Type = templates.TypeNumber
	}
	if opts, ok := m["options"].([]any); ok && len(opts) > 0 {
		v.Type = templates.TypeSelect
		var values []string
		hasEmpty := false
		for _, o := range opts {
			val := str(o)
			if om, ok := o.(map[string]any); ok {
				val = str(om["value"])
			}
			if val == "" {
				hasEmpty = true
				continue
			}
			values = append(values, val)
		}
		if hasEmpty {
			stand = "none"
			if contains(values, stand) {
				stand = "-"
			}
			values = append([]string{stand}, values...)
			if v.Default == "" {
				v.Default = stand
			}
		}
		v.Options = values
		if v.Default != "" && !contains(values, v.Default) {
			v.Options = append([]string{v.Default}, values...)
		}
	}
	if v.Type == templates.TypeString && looksSecret(key) {
		v.Type = templates.TypeSecret
	}
	if v.Hidden && v.Required && v.Default == "" {
		v.Required = false
	}
	return v, stand
}

func emptyFix(opts map[string]string) string {
	var b strings.Builder
	writeEmptyOptionFix(&b, opts)
	return b.String()
}

func writeEmptyOptionFix(b *strings.Builder, opts map[string]string) {
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "[[ \"${%s}\" == %q ]] && %s=\"\"\n", k, opts[k], k)
	}
}

var pufferVarRef = regexp.MustCompile(`\$\{([A-Za-z][A-Za-z0-9_]*)\}`)

func pufferRef(s string) string {
	s = strings.ReplaceAll(s, "${rootDir}", "{{SERVER_DIR}}")
	return pufferVarRef.ReplaceAllString(s, "{{$1}}")
}

func strList(v any) []string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	case []any:
		var out []string
		for _, i := range x {
			if s := str(i); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

var versionedJava = regexp.MustCompile(`\bjava(?:\$\{[A-Za-z][A-Za-z0-9_]*\}|\d{1,2}\b)`)

func commandLine(s string) string {
	s = strings.ReplaceAll(s, "${rootDir}", "${SERVER_DIR}")
	return versionedJava.ReplaceAllString(s, "java")
}

func (c *conv) pufferOp(typ string, op map[string]any) ([]string, bool) {
	fail := func(cmd string) string { return cmd + " || exit 1" }
	switch typ {
	case "command":
		var lines []string
		for _, l := range strList(op["commands"]) {
			lines = append(lines, fail(commandLine(l)))
		}
		return lines, true
	case "download":
		var lines []string
		for _, u := range strList(op["files"]) {
			lines = append(lines,
				"__url="+bashWord(u, false),
				`__file="${__url##*/}"; __file="${__file%%\?*}"`,
				fail(`curl -fL --retry 3 -o "$__file" "$__url"`))
		}
		return lines, true
	case "writefile":
		return []string{fail("printf '%s' " + bashWord(str(op["text"]), false) + " > " + bashWord(str(op["target"]), false))}, true
	case "move":
		return []string{fail("mv -f -- " + bashWord(str(op["source"]), true) + " " + bashWord(str(op["target"]), false))}, true
	case "mkdir":
		return []string{fail("mkdir -p -- " + bashWord(str(op["target"]), false))}, true
	case "sleep":
		return []string{"sleep " + bashWord(str(op["duration"]), false)}, true
	case "extract", "archive":
		src := bashWord(str(op["source"]), true)
		dst := str(op["destination"])
		if dst == "" {
			dst = "."
		}
		d := bashWord(dst, false)
		return []string{
			"mkdir -p -- " + d,
			"case " + src + ` in *.zip) unzip -o -q ` + src + " -d " + d + " || exit 1 ;; *) tar -xf " + src + " -C " + d + " || exit 1 ;; esac",
		}, true
	case "mojangdl":
		target := str(op["target"])
		if target == "" {
			target = "server.jar"
		}
		return []string{fail("homepanel fetch mojang " + bashWord(str(op["version"]), false) + " " + bashWord(target, false))}, true
	case "paperdl":
		target := str(op["target"])
		if target == "" {
			target = "server.jar"
		}
		if b := str(op["build"]); b != "" && b != "latest" && b != "${paperbuild}" {
			c.warn("Paper is always installed from the newest stable build (pinned build %q ignored)", b)
		} else if b != "" && b != "latest" {
			c.warn("the Paper build variable is ignored; the newest stable build of the chosen version is installed")
		}
		ver := str(op["minecraftVersion"])
		if ver == "" {
			ver = str(op["version"])
		}
		return []string{fail("homepanel fetch paper " + bashWord(ver, false) + " " + bashWord(target, false))}, true
	case "fabricdl":
		target := str(op["target"])
		if target == "" {
			target = "fabric-installer.jar"
		}
		return []string{fail("homepanel fetch fabric-installer " + bashWord(target, false))}, true
	}
	return nil, false
}

func (c *conv) emitGuarded(b *strings.Builder, expr string, lines []string, vars map[string]bool, what string) {
	if len(lines) == 0 {
		return
	}
	if strings.TrimSpace(expr) == "" {
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
		return
	}
	cond, err := bashCondition(expr, vars)
	if err != nil {
		c.warn("%s: the condition %q could not be translated (%v); the step is skipped", what, expr, err)
		cond = "false"
	}
	switch cond {
	case "false":
		return
	case "true":
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
		return
	}
	b.WriteString("if " + cond + "; then\n")
	for _, l := range lines {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("fi\n")
}

func (c *conv) pufferRun(run map[string]any, vars map[string]bool, emptyOpts map[string]string) string {
	var b strings.Builder
	writeEmptyOptionFix(&b, emptyOpts)
	if env, ok := run["environmentVars"].(map[string]any); ok {
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !validKey(k) {
				continue
			}
			fmt.Fprintf(&b, "export %s=%s\n", k, bashWord(str(env[k]), false))
		}
	}
	if wd := str(run["workingDirectory"]); wd != "" {
		fmt.Fprintf(&b, "cd %s || exit 1\n", bashWord(wd, false))
	}
	if pre, ok := run["pre"].([]any); ok && len(pre) > 0 {
		c.warn("the template's pre-start steps (%d) are not run", len(pre))
	}
	if post, ok := run["post"].([]any); ok && len(post) > 0 {
		c.warn("the template's post-stop steps (%d) are not run", len(post))
	}

	type cmd struct{ line, cond string }
	var cmds []cmd
	switch x := run["command"].(type) {
	case string:
		line := x
		if args := strList(run["arguments"]); len(args) > 0 {
			line += " " + strings.Join(args, " ")
		}
		cmds = append(cmds, cmd{line: line})
	case []any:
		for _, i := range x {
			switch e := i.(type) {
			case string:
				cmds = append(cmds, cmd{line: e})
			case map[string]any:
				cmds = append(cmds, cmd{line: str(e["command"]), cond: str(e["if"])})
			}
		}
	}
	first := true
	closed := false
	for i, cm := range cmds {
		if strings.TrimSpace(cm.line) == "" {
			continue
		}
		line := "exec " + commandLine(cm.line)
		if cm.cond == "" {
			if first {
				b.WriteString(line + "\n")
			} else {
				b.WriteString("else\n  " + line + "\nfi\n")
			}
			closed = true
			break
		}
		cond, err := bashCondition(cm.cond, vars)
		if err != nil {
			c.warn("run command %d: the condition %q could not be translated (%v); it is skipped", i+1, cm.cond, err)
			continue
		}
		if cond == "false" {
			continue
		}
		if cond == "true" {
			cm.cond = ""
			if first {
				b.WriteString(line + "\n")
			} else {
				b.WriteString("else\n  " + line + "\nfi\n")
			}
			closed = true
			break
		}
		if first {
			b.WriteString("if " + cond + "; then\n  " + line + "\n")
			first = false
		} else {
			b.WriteString("elif " + cond + "; then\n  " + line + "\n")
		}
	}
	if !first && !closed {
		b.WriteString("fi\n")
	}
	if !closed {
		b.WriteString(`echo "[panel] no start command of this template matches the installed files; reinstall the server" >&2` + "\nexit 1\n")
	}

	switch stop := strings.TrimSpace(str(run["stop"])); {
	case stop != "":
		c.t.Stop.Command = stop
	default:
		switch str(run["stopCode"]) {
		case "2":
			c.t.Stop.Command = "^C"
		case "9":
			c.t.Stop.Command = "SIGKILL"
		}
	}
	c.t.Stop.TimeoutSec = 60
	return b.String()
}

package importer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/C7PEK/cypek.dev-panel/internal/installer"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

func TestBashCondition(t *testing.T) {
	vars := map[string]bool{"modlauncher": true, "version": true}
	cases := map[string]string{
		"modlauncher == 'fabric'":                            `[[ "${modlauncher}" == "fabric" ]]`,
		"modlauncher == 'fabric' && env == 'host'":           `[[ "${modlauncher}" == "fabric" ]]`,
		"modlauncher == 'fabric' && env != 'host'":           `false`,
		"env == 'host' || modlauncher == 'x'":                `true`,
		"!file_exists('server.properties')":                  `! [[ -e "server.properties" ]]`,
		"file_exists('forge-' + resolved + '.jar')":          `[[ -e "forge-${resolved}.jar" ]]`,
		"(modlauncher == 'a' || modlauncher == 'b') && eula": `{ ( { [[ "${modlauncher}" == "a" ]] || [[ "${modlauncher}" == "b" ]]; } ) && [[ "${eula}" == "true" || "${eula}" == "1" ]]; }`,
		`version == "1.$(x)"`:                                `[[ "${version}" == "1.\$(x)" ]]`,
	}
	for in, want := range cases {
		got, err := bashCondition(in, vars)
		if err != nil || got != want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"a < b", "foo(1)", "a == ", "'open"} {
		if _, err := bashCondition(bad, vars); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestBashWord(t *testing.T) {
	cases := map[string]string{
		"eula=${eula}":  `"eula=${eula}"`,
		"a\"b`c`$(d)$e": `"a\"b\` + "`c\\`" + `\$(d)\$e"`,
		"${rootDir}/x":  `"${SERVER_DIR}/x"`,
		"${not valid}":  `"\${not valid}"`,
	}
	for in, want := range cases {
		if got := bashWord(in, false); got != want {
			t.Errorf("bashWord(%q) = %s, want %s", in, got, want)
		}
	}
	if got := bashWord("spigot-*.jar", true); got != `"spigot-"*".jar"` {
		t.Errorf("glob: %s", got)
	}
}

func TestParseRulesKeepsRegexPipes(t *testing.T) {
	got := parseRules(`required|regex:/^(true|false)$/|max:5`)
	want := []string{"required", "regex:/^(true|false)$/", "max:5"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q", got)
	}
	if p, ok := phpRegex(`/^[a-z]+$/i`); !ok || p != `(?i)^[a-z]+$` {
		t.Errorf("phpRegex: %q %v", p, ok)
	}
	if _, ok := phpRegex(`/^(?=x)$/`); ok {
		t.Error("lookahead should be rejected")
	}
}

func eggJSON(t *testing.T, mutate func(map[string]any)) []byte {
	egg := map[string]any{
		"meta":    map[string]any{"version": "PTDL_v2"},
		"name":    "Test Bot",
		"author":  "a@b.c",
		"startup": "if [[ -d .git ]] && [[ {{AUTO_UPDATE}} == \"1\" ]]; then git pull; fi; /usr/local/bin/node /home/container/{{BOT_JS_FILE}}",
		"docker_images": map[string]any{
			"Node 22": "ghcr.io/parkervcp/yolks:nodejs_22",
			"Node 20": "ghcr.io/parkervcp/yolks:nodejs_20",
		},
		"config":  map[string]any{"files": "{}", "startup": `{"done": ["ready", "online"]}`, "stop": "^C"},
		"scripts": map[string]any{"installation": map[string]any{"script": "npm install\n", "entrypoint": "bash"}},
		"variables": []any{
			map[string]any{"name": "Auto update", "env_variable": "AUTO_UPDATE", "default_value": "0", "user_viewable": true, "rules": "required|boolean"},
			map[string]any{"name": "Bot file", "env_variable": "BOT_JS_FILE", "default_value": "index.js", "user_viewable": true, "rules": "required|string"},
			map[string]any{"name": "Bot token", "env_variable": "BOT_TOKEN", "default_value": "", "user_viewable": true, "rules": "nullable|string"},
			map[string]any{"name": "Internal", "env_variable": "INTERNAL_X", "default_value": "", "user_viewable": false, "rules": "required|string"},
			map[string]any{"name": "Mode", "env_variable": "MODE", "default_value": "a", "rules": "required|in:a,b,c"},
		},
	}
	if mutate != nil {
		mutate(egg)
	}
	b, _ := json.Marshal(egg)
	return b
}

func TestEggConversion(t *testing.T) {
	res, err := Import(eggJSON(t, nil), Options{SourceURL: "https://example.com/egg.json"})
	if err != nil {
		t.Fatal(err)
	}
	tp := res.Template
	if res.Format != FormatPterodactyl || tp.ID != "egg-test-bot" || tp.Category != "discord-bot" {
		t.Fatalf("meta: %s %s %s", res.Format, tp.ID, tp.Category)
	}
	if tp.Runtime.Kind != "node" || tp.Runtime.Version != "{{NODE_VERSION}}" {
		t.Errorf("runtime: %+v", tp.Runtime)
	}
	if v := tp.Variable("NODE_VERSION"); v == nil || v.Default != "20" || strings.Join(v.Options, ",") != "22,20" {
		t.Errorf("node version var: %+v", v)
	}
	if v := tp.Variable("AUTO_UPDATE"); v == nil || v.Type != templates.TypeSelect || strings.Join(v.Options, ",") != "0,1" {
		t.Errorf("boolean rule: %+v", v)
	}
	if v := tp.Variable("BOT_TOKEN"); v == nil || v.Type != templates.TypeSecret || v.Required {
		t.Errorf("token: %+v", v)
	}
	if v := tp.Variable("INTERNAL_X"); v == nil || !v.Hidden || v.Required {
		t.Errorf("hidden: %+v", v)
	}
	if v := tp.Variable("MODE"); v == nil || v.Type != templates.TypeSelect || len(v.Options) != 3 {
		t.Errorf("in rule: %+v", v)
	}
	if !strings.Contains(tp.Start.Shell, "node {{SERVER_DIR}}/{{BOT_JS_FILE}}") || strings.Contains(tp.Start.Shell, "/usr/local/bin") {
		t.Errorf("shell: %s", tp.Start.Shell)
	}
	if tp.Stop.Command != "^C" || tp.ReadyRegex != "ready|online" {
		t.Errorf("stop/ready: %q %q", tp.Stop.Command, tp.ReadyRegex)
	}
	if tp.Source == nil || tp.Source.URL != "https://example.com/egg.json" || tp.Source.Kind != FormatPterodactyl {
		t.Errorf("source: %+v", tp.Source)
	}
	if !tp.UsesScript() {
		t.Error("script step missing")
	}
}

func TestEggDetectsPelicanAndYAML(t *testing.T) {
	egg := eggJSON(t, func(m map[string]any) { m["meta"] = map[string]any{"version": "PLCN_v1"} })
	res, err := Import(egg, Options{})
	if err != nil || res.Format != FormatPelican || res.Template.ID != "pelican-test-bot" {
		t.Fatalf("pelican: %v %+v", err, res)
	}
	yml := `
meta: {version: PLCN_v2}
name: Yaml Egg
startup_commands:
  Default: ./run --port {{SERVER_PORT}}
docker_images:
  Java 17: ghcr.io/ptero-eggs/yolks:java_17
  Java 21: ghcr.io/ptero-eggs/yolks:java_21
config:
  files:
    config.yml:
      parser: yaml
      find:
        server.port: "{{server.build.default.port}}"
        listeners[0].host: x
  stop: end
variables: []
`
	res, err = Import([]byte(yml), Options{})
	if err != nil {
		t.Fatal(err)
	}
	tp := res.Template
	if tp.Runtime.Kind != "java" || tp.Variable("JAVA_VERSION").Default != "17" {
		t.Errorf("first image is the default: %+v %+v", tp.Runtime, tp.Variable("JAVA_VERSION"))
	}
	if tp.Variable("SERVER_PORT") == nil || len(tp.Ports) != 1 || tp.ConfigFiles[0].Set["server.port"] != "{{SERVER_PORT}}" {
		t.Errorf("port: %+v %+v", tp.Ports, tp.ConfigFiles)
	}
	found := false
	for _, w := range res.Warnings {
		found = found || strings.Contains(w, "listeners[0].host")
	}
	if !found {
		t.Errorf("wildcard key warning missing: %v", res.Warnings)
	}
}

func TestUnknownFormat(t *testing.T) {
	for _, in := range []string{`{"a": 1}`, `[1,2]`, ``, `nope: true`} {
		if _, err := Import([]byte(in), Options{}); err == nil {
			t.Errorf("%q should fail", in)
		}
	}
}

const pufferSample = `{
  "type": "test-java", "display": "Test: Java",
  "data": {
    "eula": {"type": "boolean", "value": false, "display": "EULA", "required": true},
    "loader": {"type": "string", "value": "", "display": "Loader", "options": [{"value": "", "display": "None"}, {"value": "fabric", "display": "Fabric"}]},
    "version": {"type": "string", "value": "1.21", "display": "Version"},
    "javaversion": {"type": "string", "value": "21", "display": "Java"},
    "port": {"type": "integer", "value": 25565, "display": "Port"}
  },
  "install": [
    {"type": "javadl", "version": "${javaversion}"},
    {"if": "loader == ''", "type": "writefile", "text": "vanilla ${version}", "target": "kind.txt"},
    {"if": "loader == 'fabric' && env == 'host'", "type": "writefile", "text": "fabric", "target": "kind.txt"},
    {"if": "loader == 'fabric' && env != 'host'", "type": "command", "commands": ["false"]},
    {"if": "loader == 'forge'", "type": "forgedl"},
    {"type": "writefile", "text": "eula=${eula}", "target": "eula.txt"}
  ],
  "run": {
    "stop": "stop",
    "command": [
      {"if": "file_exists('fabric.jar')", "command": "java${javaversion} -jar fabric.jar"},
      {"command": "java${javaversion} -Xmx1G -jar server.jar --port ${port}"}
    ]
  },
  "query": {"type": "minecraft"}
}`

func TestPufferConversionRuns(t *testing.T) {
	res, err := Import([]byte(pufferSample), Options{})
	if err != nil {
		t.Fatal(err)
	}
	tp := res.Template
	if res.Format != FormatPuffer || tp.ID != "puffer-test-java" || tp.Runtime.Kind != "java" || tp.Runtime.Version != "{{javaversion}}" {
		t.Fatalf("meta: %s %s %+v", res.Format, tp.ID, tp.Runtime)
	}
	if v := tp.Variable("loader"); v == nil || v.Options[0] != "none" || v.Default != "none" {
		t.Errorf("empty option stand-in: %+v", v)
	}
	if v := tp.Variable("eula"); v == nil || !v.Consent {
		t.Errorf("eula consent: %+v", v)
	}
	if strings.Contains(tp.Start.Shell, "java${") || strings.Contains(tp.Start.Shell, "java21") {
		t.Errorf("versioned java left in start: %s", tp.Start.Shell)
	}
	if tp.Query == nil || tp.Query.Port != "{{port}}" || len(tp.Ports) != 1 {
		t.Errorf("query/ports: %+v %+v", tp.Query, tp.Ports)
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	script := tp.Install[0].Script.Content
	for _, c := range []struct {
		loader, want string
	}{{"none", "vanilla 1.21"}, {"fabric", "fabric"}} {
		dir := t.TempDir()
		out, err := runBash(t, dir, installer.PrepareScript(script, dir), map[string]string{"loader": c.loader, "version": "1.21", "eula": "true", "SERVER_DIR": dir})
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.loader, err, out)
		}
		got, _ := os.ReadFile(filepath.Join(dir, "kind.txt"))
		eula, _ := os.ReadFile(filepath.Join(dir, "eula.txt"))
		if string(got) != c.want || string(eula) != "eula=true" {
			t.Errorf("%s: kind=%q eula=%q", c.loader, got, eula)
		}
	}

	dir := t.TempDir()
	if out, err := runBash(t, dir, installer.PrepareScript(script, dir), map[string]string{"loader": "forge", "SERVER_DIR": dir}); err == nil || !strings.Contains(out, "forgedl is not supported") {
		t.Errorf("forge should fail: %v %s", err, out)
	}

	dir = t.TempDir()
	shell := templates.ShellReferences(tp.Start.Shell)
	fake := "java() { echo \"java $*\"; }\n" + strings.ReplaceAll(shell, "exec java", "java")
	out, _ := runBash(t, dir, fake, map[string]string{"loader": "none", "port": "25570"})
	if strings.TrimSpace(out) != "java -Xmx1G -jar server.jar --port 25570" {
		t.Errorf("start fallback: %q", out)
	}
	os.WriteFile(filepath.Join(dir, "fabric.jar"), nil, 0o644)
	out, _ = runBash(t, dir, fake, map[string]string{"loader": "fabric"})
	if strings.TrimSpace(out) != "java -jar fabric.jar" {
		t.Errorf("start fabric: %q", out)
	}
}

func runBash(t *testing.T, dir, script string, env map[string]string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestConvertedScriptsParse(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	inputs := [][]byte{eggJSON(t, nil), []byte(pufferSample)}
	if dir := os.Getenv("HP_EGG_DIR"); dir != "" {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		for _, f := range files {
			b, _ := os.ReadFile(f)
			inputs = append(inputs, b)
		}
	}
	for _, in := range inputs {
		res, err := Import(in, Options{})
		if err != nil {
			continue
		}
		check := func(what, src string) {
			cmd := exec.Command("bash", "-n")
			cmd.Stdin = strings.NewReader(src)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s %s: %v\n%s", res.Template.ID, what, err, out)
			}
		}
		check("start", templates.ShellReferences(res.Template.Start.Shell))
		for _, s := range res.Template.Install {
			if s.Script != nil {
				check("install", installer.PrepareScript(s.Script.Content, "/srv/x"))
			}
		}
	}
}

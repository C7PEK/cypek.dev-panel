package templates

import (
	"errors"
	"strings"
	"testing"

	builtin "github.com/cypek019/cypek.dev---panel/templates"
)

func TestBuiltinTemplatesLoad(t *testing.T) {
	r, err := Load(builtin.Builtin, "")
	if err != nil {
		t.Fatal(err)
	}
	tp, ok := r.Get("minecraft-paper")
	if !ok || !tp.Builtin || tp.Category != "minecraft" {
		t.Fatalf("minecraft-paper = %+v, %v", tp, ok)
	}
	if v := tp.Variable("EULA"); v == nil || !v.Consent || v.Type != TypeBool {
		t.Errorf("EULA variable = %+v", v)
	}
	if len(r.List()) == 0 {
		t.Error("List empty")
	}
}

func TestUserTemplateOverridesAndRejectsBad(t *testing.T) {
	dir := t.TempDir()
	good := "id: minecraft-paper\nname: Mine\ncategory: minecraft\nvariables: []\nstart: {command: /bin/true, args: []}\n"
	if err := writeFile(dir+"/mine.yaml", good); err != nil {
		t.Fatal(err)
	}
	r, err := Load(builtin.Builtin, dir)
	if err != nil {
		t.Fatal(err)
	}
	if tp, _ := r.Get("minecraft-paper"); tp.Name != "Mine" || tp.Builtin {
		t.Errorf("override not applied: %+v", tp)
	}
	bad := []string{
		"id: X\nname: n\ncategory: other\nstart: {command: x}\n",                                                // bad id
		"id: ok\nname: n\ncategory: nope\nstart: {command: x}\n",                                                // bad category
		"id: ok\nname: n\ncategory: other\nstart: {command: '{{NOPE}}'}\n",                                      // unknown placeholder
		"id: ok\nname: n\ncategory: other\ninstall: [{write: {path: ../x, content: y}}]\nstart: {command: x}\n", // traversal
		"id: ok\nname: n\ncategory: other\ninstall: [{write: {path: /etc/x, content: y}}]\nstart: {command: x}\n",
		"id: ok\nname: n\ncategory: other\nvariables: [{key: JAVA, type: string}]\nstart: {command: x}\n", // reserved key
		"id: ok\nname: n\ncategory: other\nvariables: [{key: 1a, type: string}]\nstart: {command: x}\n",   // bad key
		"id: ok\nname: n\ncategory: other\nvariables: [{key: A, type: select}]\nstart: {command: x}\n",    // select w/o options
		"id: ok\nname: n\ncategory: other\nbogus: 1\nstart: {command: x}\n",                               // unknown field
		"id: ok\nname: n\ncategory: other\nstart: {command: x}\nreadyRegex: '('\n",
	}
	for i, b := range bad {
		if _, err := Parse([]byte(b)); err == nil {
			t.Errorf("bad template %d accepted:\n%s", i, b)
		}
	}
}

func TestRender(t *testing.T) {
	vars := map[string]string{"RAM_MB": "2048", "JVM_FLAGS": `["-XX:+UseG1GC","-XX:+AlwaysPreTouch"]`, "JAVA": "/opt/java/bin/java"}
	s, err := Render("-Xmx{{RAM_MB}}M", vars)
	if err != nil || s != "-Xmx2048M" {
		t.Errorf("Render = %q, %v", s, err)
	}
	if _, err := Render("{{MISSING}}", vars); !errors.Is(err, ErrMissingVariable) {
		t.Errorf("missing var: %v", err)
	}
	// Values are inserted verbatim: no shell, so metacharacters are inert.
	s, _ = Render("{{RAM_MB}}", map[string]string{"RAM_MB": "$(rm -rf /); `x`"})
	if s != "$(rm -rf /); `x`" {
		t.Errorf("Render changed the value: %q", s)
	}
	args, err := RenderArgs([]string{"{{JAVA}}", "-Xmx{{RAM_MB}}M", "{{JVM_FLAGS...}}", "-jar"}, vars)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "/opt/java/bin/java -Xmx2048M -XX:+UseG1GC -XX:+AlwaysPreTouch -jar" {
		t.Errorf("RenderArgs = %v", args)
	}
	if _, err := RenderArgs([]string{"{{JVM_FLAGS...}}"}, map[string]string{"JVM_FLAGS": "not a list"}); err == nil {
		t.Error("non-list spread accepted")
	}
}

func TestValidateValues(t *testing.T) {
	r, _ := Load(builtin.Builtin, "")
	tp, _ := r.Get("minecraft-paper")

	_, err := tp.ValidateValues(map[string]string{"MC_VERSION": "1.21.11"})
	var ve *ValueError
	if !errors.As(err, &ve) || ve.Key != "EULA" || !ve.Consent {
		t.Fatalf("missing consent: %v", err)
	}
	vals, err := tp.ValidateValues(map[string]string{"MC_VERSION": "1.21.11", "EULA": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if vals["RAM_MB"] != "2048" || vals["PORT"] != "25565" || vals["MOTD"] != "A Minecraft Server" || vals["EULA"] != "true" {
		t.Errorf("defaults = %v", vals)
	}
	cases := map[string]map[string]string{
		"ram too low": {"MC_VERSION": "x", "EULA": "true", "RAM_MB": "100"},
		"ram not num": {"MC_VERSION": "x", "EULA": "true", "RAM_MB": "lots"},
		"port high":   {"MC_VERSION": "x", "EULA": "true", "PORT": "70000"},
		"motd long":   {"MC_VERSION": "x", "EULA": "true", "MOTD": strings.Repeat("a", 60)},
		"newline":     {"MC_VERSION": "x", "EULA": "true", "MOTD": "a\nb"},
		"unknown key": {"MC_VERSION": "x", "EULA": "true", "HAX": "1"},
	}
	for name, in := range cases {
		if _, err := tp.ValidateValues(in); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func writeFile(path, content string) error {
	return osWriteFile(path, []byte(content))
}

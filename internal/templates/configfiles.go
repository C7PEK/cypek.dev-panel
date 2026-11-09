package templates

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ApplyConfigFiles writes the template's ConfigFiles values (rendered with
// vars) into the server's files. Missing properties/ini files are created;
// missing yaml/json/file targets are skipped (the game creates them on first
// start and the next start fills them in). Returns the files it changed.
func ApplyConfigFiles(serverDir string, t *Template, vars map[string]string) ([]string, error) {
	if t == nil || len(t.ConfigFiles) == 0 {
		return nil, nil
	}
	root, err := filepath.EvalSymlinks(serverDir)
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, cf := range t.ConfigFiles {
		values := map[string]string{}
		for k, v := range cf.Set {
			r, err := Render(v, vars)
			if err != nil {
				return changed, fmt.Errorf("%s: %w", cf.Path, err)
			}
			values[k] = r
		}
		path := filepath.Join(root, filepath.FromSlash(cf.Path))
		if real, err := filepath.EvalSymlinks(path); err == nil {
			if !strings.HasPrefix(real, root+string(filepath.Separator)) {
				continue
			}
			path = real
		} else if !os.IsNotExist(err) {
			continue
		}
		var did bool
		switch cf.Parser {
		case "properties", "ini":
			did, err = setProperties(root, path, values, true)
		case "file":
			did, err = setLines(root, path, values)
		case "yaml":
			did, err = setYAML(root, path, values)
		case "json":
			did, err = setJSON(root, path, values)
		}
		if err != nil {
			return changed, fmt.Errorf("%s: %w", cf.Path, err)
		}
		if did {
			changed = append(changed, cf.Path)
		}
	}
	return changed, nil
}

func readLines(root, path string) ([]string, bool, error) {
	b, err := readUnder(root, path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		lines = append(lines, strings.TrimRight(sc.Text(), "\r"))
	}
	return lines, true, sc.Err()
}

func writeIfChanged(root, path string, before, after string) (bool, error) {
	if before == after {
		return false, nil
	}
	mode := os.FileMode(0o640)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return true, writeUnder(root, path, []byte(after), mode)
}

// setProperties sets key=value lines, keeping comments and order; keys
// that are missing are appended (sorted for stable output).
func setProperties(root, path string, values map[string]string, create bool) (bool, error) {
	lines, exists, err := readLines(root, path)
	if err != nil {
		return false, err
	}
	if !exists && !create {
		return false, nil
	}
	before := strings.Join(lines, "\n")
	seen := map[string]bool{}
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "!") || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "[") {
			continue
		}
		eq := strings.IndexAny(l, "=:")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(l[:eq])
		if v, ok := values[key]; ok {
			lines[i] = key + "=" + v
			seen[key] = true
		}
	}
	var missing []string
	for k := range values {
		if !seen[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	for _, k := range missing {
		lines = append(lines, k+"="+values[k])
	}
	after := strings.Join(lines, "\n")
	if exists {
		return writeIfChanged(root, path, before+"\n", after+"\n")
	}
	return writeIfChanged(root, path, "", after+"\n")
}

// setLines replaces every line that starts with a key by the value
// (Pterodactyl's "file" parser).
func setLines(root, path string, values map[string]string) (bool, error) {
	lines, exists, err := readLines(root, path)
	if err != nil || !exists {
		return false, err
	}
	before := strings.Join(lines, "\n")
	for i, l := range lines {
		for k, v := range values {
			if strings.HasPrefix(l, k) {
				lines[i] = v
				break
			}
		}
	}
	return writeIfChanged(root, path, before+"\n", strings.Join(lines, "\n")+"\n")
}

func setYAML(root, path string, values map[string]string) (bool, error) {
	b, err := readUnder(root, path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return false, fmt.Errorf("parse yaml: %w", err)
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	top := doc.Content[0]
	keys := sortedKeys(values)
	for _, k := range keys {
		setYAMLPath(top, strings.Split(k, "."), values[k])
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return false, err
	}
	return writeIfChanged(root, path, string(b), string(out))
}

func setYAMLPath(n *yaml.Node, path []string, value string) {
	if n.Kind != yaml.MappingNode || len(path) == 0 {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == path[0] {
			if len(path) == 1 {
				n.Content[i+1] = scalarNode(value)
				return
			}
			setYAMLPath(n.Content[i+1], path[1:], value)
			return
		}
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Value: path[0]}
	if len(path) == 1 {
		n.Content = append(n.Content, key, scalarNode(value))
		return
	}
	child := &yaml.Node{Kind: yaml.MappingNode}
	n.Content = append(n.Content, key, child)
	setYAMLPath(child, path[1:], value)
}

// scalarNode keeps numbers and booleans unquoted, like the original file would.
func scalarNode(v string) *yaml.Node {
	var probe any
	if err := yaml.Unmarshal([]byte(v), &probe); err == nil {
		switch probe.(type) {
		case int, float64, bool:
			return &yaml.Node{Kind: yaml.ScalarNode, Value: v}
		}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Value: v, Style: yaml.DoubleQuotedStyle}
}

func setJSON(root, path string, values map[string]string) (bool, error) {
	b, err := readUnder(root, path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return false, fmt.Errorf("parse json: %w", err)
	}
	for _, k := range sortedKeys(values) {
		setJSONPath(doc, strings.Split(k, "."), values[k])
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	return writeIfChanged(root, path, string(b), string(out)+"\n")
}

func setJSONPath(m map[string]any, path []string, value string) {
	if len(path) == 1 {
		var typed any
		if err := json.Unmarshal([]byte(value), &typed); err == nil {
			switch typed.(type) {
			case float64, bool:
				m[path[0]] = typed
				return
			}
		}
		m[path[0]] = value
		return
	}
	child, ok := m[path[0]].(map[string]any)
	if !ok {
		child = map[string]any{}
		m[path[0]] = child
	}
	setJSONPath(child, path[1:], value)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

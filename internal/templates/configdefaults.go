package templates

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// configDefaultMarker documents where an automatically added line came from.
const configDefaultMarker = "# added by panel.cypek.dev (template default; edit or remove freely)"

// ApplyConfigDefaults makes sure every ConfigDefault line is present in the
// matching files under serverDir. It returns the files it changed. Files
// that are symlinks or resolve outside serverDir are skipped.
func ApplyConfigDefaults(serverDir string, t *Template) ([]string, error) {
	if t == nil || len(t.ConfigDefaults) == 0 {
		return nil, nil
	}
	root, err := filepath.EvalSymlinks(serverDir)
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, d := range t.ConfigDefaults {
		for _, pattern := range d.Files {
			matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
			if err != nil {
				return changed, fmt.Errorf("configDefaults glob %q: %w", pattern, err)
			}
			for _, path := range matches {
				st, err := os.Lstat(path)
				if err != nil || !st.Mode().IsRegular() {
					continue
				}
				real, err := filepath.EvalSymlinks(path)
				if err != nil || !strings.HasPrefix(real, root+string(filepath.Separator)) {
					continue
				}
				added, err := ensureLine(real, d.Key, d.Line, st.Mode().Perm())
				if err != nil {
					return changed, fmt.Errorf("update %s: %w", path, err)
				}
				if added {
					rel, _ := filepath.Rel(root, real)
					changed = append(changed, filepath.ToSlash(rel))
				}
			}
		}
	}
	return changed, nil
}

// ensureLine prepends line unless an uncommented line already mentions key.
// Prepending matters for FXServer: convars must be set before `ensure`.
func ensureLine(path, key, line string, mode os.FileMode) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	lowerKey := strings.ToLower(key)
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
			continue
		}
		if strings.Contains(strings.ToLower(t), lowerKey) {
			return false, nil
		}
	}
	out := configDefaultMarker + "\n" + line + "\n\n" + string(b)
	tmp := path + ".panel-tmp"
	if err := os.WriteFile(tmp, []byte(out), mode); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

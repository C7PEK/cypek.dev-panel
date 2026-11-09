package templates

import (
	"bufio"
	"fmt"
	"github.com/C7PEK/cypek.dev-panel/internal/safefs"
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
				added, err := ensureLine(root, real, d.Key, d.Line, st.Mode().Perm())
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
func ensureLine(root, path, key, line string, mode os.FileMode) (bool, error) {
	b, err := readUnder(root, path)
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
	return true, writeUnder(root, path, []byte(out), mode)
}

// maxConfigFile bounds config files the panel edits (game-controlled).
const maxConfigFile = 16 << 20

// readUnder reads abs (inside root) without following links out of root;
// regular files only.
func readUnder(root, abs string) ([]byte, error) {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, fmt.Errorf("%s is outside the server folder", abs)
	}
	return safefs.ReadFile(root, rel, maxConfigFile)
}

// writeUnder replaces abs (inside root) atomically; a planted symlink or
// temp name can never redirect the write.
func writeUnder(root, abs string, data []byte, mode os.FileMode) error {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("%s is outside the server folder", abs)
	}
	return safefs.WriteFile(root, rel, data, mode)
}

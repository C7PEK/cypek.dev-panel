package servers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func CommandProblem(command, serverDir string) string {
	var path string
	switch {
	case filepath.IsAbs(command):
		path = command
	case strings.Contains(command, "/"):
		path = filepath.Join(serverDir, command)
	default:
		if _, err := exec.LookPath(command); err != nil {
			return "Program \"" + command + "\" was not found on PATH. Use an absolute path such as /usr/bin/java."
		}
		return ""
	}
	st, err := os.Stat(path)
	if err != nil {
		return "Command \"" + path + "\" does not exist. Check the path (for example /bin/cat, not bin/cat)."
	}
	if st.IsDir() {
		return "Command \"" + path + "\" is a directory, not a program."
	}
	if st.Mode()&0o111 == 0 {
		return "Command \"" + path + "\" is not executable. Run: chmod +x " + path
	}
	return ""
}

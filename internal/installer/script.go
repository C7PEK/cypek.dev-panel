package installer

import (
	"os/exec"
	"regexp"
	"strings"
)

const scriptPrelude = `#!/bin/bash
# --- panel.cypek.dev install wrapper ---------------------------------------
# Package managers need root; the tools they would install (curl, jq, unzip,
# git, ...) must already be on the host. Missing ones fail with a clear
# "command not found" below.
export DEBIAN_FRONTEND=noninteractive
for __pm in apt apt-get apk yum dnf zypper pacman; do
  eval "${__pm}() { echo \"[panel] skipped (no root): ${__pm} \$*\" >&2; return 0; }"
done
sudo() { "$@"; }
# PufferPanel calls versioned Java binaries (java21 ...); map them to the
# prepared runtime on PATH.
for __v in 8 11 16 17 18 19 20 21 22 23 24 25 26 27; do
  eval "java${__v}() { java \"\$@\"; }"
done
# Download helpers for converted PufferPanel templates (checksums verified).
homepanel() { "${HOMEPANEL_BIN:-homepanel}" "$@"; }
__missing=""
for __tool in curl jq unzip tar git wget file; do
  command -v "$__tool" >/dev/null 2>&1 || __missing="$__missing $__tool"
done
[ -n "$__missing" ] && echo "[panel] note: not installed on this host:$__missing (scripts that need them will fail; install with: sudo apt install$__missing)" >&2
cd "$SERVER_DIR" || exit 1
# --- template script ------------------------------------------------------
`

var crlf = regexp.MustCompile(`\r\n?`)

func PrepareScript(content, serverDir string) string {
	s := crlf.ReplaceAllString(content, "\n")

	if strings.HasPrefix(s, "#!") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	for _, p := range []string{"/mnt/server", "/home/container"} {
		s = strings.ReplaceAll(s, p, serverDir)
	}
	return scriptPrelude + s + "\n"
}

var hostTools = []string{"curl", "wget", "jq", "unzip", "tar", "git", "xz", "file"}

var aptPackage = map[string]string{"xz": "xz-utils"}

func MissingTools(script string) []string {
	var out []string
	for _, tool := range hostTools {
		if !regexp.MustCompile(`(^|[\s;|&(` + "`" + `$])` + regexp.QuoteMeta(tool) + `\s`).MatchString(script) {
			continue
		}
		if _, err := exec.LookPath(tool); err != nil {
			if pkg, ok := aptPackage[tool]; ok {
				tool = pkg
			}
			out = append(out, tool)
		}
	}
	return out
}

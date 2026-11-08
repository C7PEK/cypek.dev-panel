package netinfo

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Firewall describes what the host runs so the UI can print the right
// command for opening a port. The panel never changes firewall rules itself.
type Firewall struct {
	Kind   string `json:"kind"`   // ufw | firewalld | none | unknown
	Active bool   `json:"active"` // best effort; false when it could not be determined
}

// DetectFirewall checks for ufw and firewalld binaries and their state.
func DetectFirewall(ctx context.Context) Firewall {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if path, err := exec.LookPath("ufw"); err == nil {
		fw := Firewall{Kind: "ufw"}
		// `ufw status` needs root; as the panel user it usually fails, so
		// fall back to the config file state.
		if out, err := exec.CommandContext(ctx, path, "status").CombinedOutput(); err == nil {
			fw.Active = strings.Contains(string(out), "Status: active")
		} else if b, err := readFile("/etc/ufw/ufw.conf"); err == nil {
			fw.Active = strings.Contains(b, "ENABLED=yes")
		}
		return fw
	}
	if path, err := exec.LookPath("firewall-cmd"); err == nil {
		fw := Firewall{Kind: "firewalld"}
		if out, err := exec.CommandContext(ctx, path, "--state").CombinedOutput(); err == nil {
			fw.Active = strings.TrimSpace(string(out)) == "running"
		}
		return fw
	}
	return Firewall{Kind: "none"}
}

// OpenPortCommands returns copy-pasteable commands for the detected firewall.
func OpenPortCommands(fw Firewall, port int, protocol string) []string {
	protos := []string{protocol}
	if protocol == "tcp+udp" {
		protos = []string{"tcp", "udp"}
	}
	var cmds []string
	switch fw.Kind {
	case "ufw":
		for _, p := range protos {
			cmds = append(cmds, fmt.Sprintf("sudo ufw allow %d/%s", port, p))
		}
	case "firewalld":
		for _, p := range protos {
			cmds = append(cmds, fmt.Sprintf("sudo firewall-cmd --permanent --add-port=%d/%s", port, p))
		}
		cmds = append(cmds, "sudo firewall-cmd --reload")
	}
	return cmds
}

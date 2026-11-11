package netinfo

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Firewall struct {
	Kind   string `json:"kind"`
	Active bool   `json:"active"`
}

func DetectFirewall(ctx context.Context) Firewall {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if path, err := exec.LookPath("ufw"); err == nil {
		fw := Firewall{Kind: "ufw"}

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

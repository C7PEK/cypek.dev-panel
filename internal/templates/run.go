package templates

import (
	"fmt"
	"strconv"

	"github.com/cypek019/cypek.dev---panel/internal/servers"
	"github.com/cypek019/cypek.dev---panel/internal/shim"
)

// BuildRun renders the template's start/stop section into a shim run config
// and the port list. vars must contain the user variables plus the builtins
// the installer resolved (JAVA, JVM_FLAGS, ...).
func BuildRun(t *Template, vars map[string]string) (shim.RunConfig, []servers.Port, error) {
	var run shim.RunConfig
	var err error
	if run.Command, err = Render(t.Start.Command, vars); err != nil {
		return run, nil, fmt.Errorf("start.command: %w", err)
	}
	if run.Args, err = RenderArgs(t.Start.Args, vars); err != nil {
		return run, nil, fmt.Errorf("start.args: %w", err)
	}
	if run.Args == nil {
		run.Args = []string{}
	}
	if len(t.Start.Env) > 0 {
		run.Env = map[string]string{}
		for k, v := range t.Start.Env {
			if run.Env[k], err = Render(v, vars); err != nil {
				return run, nil, fmt.Errorf("start.env.%s: %w", k, err)
			}
		}
	}
	run.Stop = shim.StopConfig{Command: t.Stop.Command, TimeoutSec: t.Stop.TimeoutSec}
	run.ReadyRegex = t.ReadyRegex
	if err := run.Validate(); err != nil {
		return run, nil, err
	}
	ports := make([]servers.Port, 0, len(t.Ports))
	for _, p := range t.Ports {
		rendered, err := Render(p.Port, vars)
		if err != nil {
			return run, nil, fmt.Errorf("port %s: %w", p.Name, err)
		}
		n, err := strconv.Atoi(rendered)
		if err != nil || n < 1 || n > 65535 {
			return run, nil, fmt.Errorf("port %s: %q is not a valid port", p.Name, rendered)
		}
		ports = append(ports, servers.Port{Name: p.Name, Port: n, Protocol: p.Protocol})
	}
	return run, ports, nil
}

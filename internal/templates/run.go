package templates

import (
	"fmt"
	"strconv"

	"github.com/cypek019/cypek.dev---panel/internal/servers"
	"github.com/cypek019/cypek.dev---panel/internal/shim"
)

// Builtins written by BuildRun so the panel knows how to query players
// without re-reading the template.
const (
	KeyQueryKind = "QUERY_KIND"
	KeyQueryPort = "QUERY_PORT"
)

// RenderQuery renders the template's query spec (kind, port) or returns
// ok=false when the template has none.
func RenderQuery(t *Template, vars map[string]string) (kind string, port int, ok bool, err error) {
	if t.Query == nil {
		return "", 0, false, nil
	}
	rendered, err := Render(t.Query.Port, vars)
	if err != nil {
		return "", 0, false, fmt.Errorf("query.port: %w", err)
	}
	n, err := strconv.Atoi(rendered)
	if err != nil || n < 1 || n > 65535 {
		return "", 0, false, fmt.Errorf("query.port: %q is not a valid port", rendered)
	}
	return t.Query.Kind, n, true, nil
}

// BuildRun renders the template's start/stop section into a shim run config
// and the port list. vars must contain the user variables plus the builtins
// the installer resolved (JAVA, JVM_FLAGS, ...). It also stores the rendered
// query spec into vars (QUERY_KIND / QUERY_PORT) so it is kept as a builtin.
func BuildRun(t *Template, vars map[string]string) (shim.RunConfig, []servers.Port, error) {
	if kind, port, ok, err := RenderQuery(t, vars); err != nil {
		return shim.RunConfig{}, nil, err
	} else if ok {
		vars[KeyQueryKind] = kind
		vars[KeyQueryPort] = strconv.Itoa(port)
	}
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

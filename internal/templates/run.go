package templates

import (
	"fmt"
	"strconv"

	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
)

const (
	KeyQueryKind = "QUERY_KIND"
	KeyQueryPort = "QUERY_PORT"
)

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

func BuildRun(t *Template, vars map[string]string) (shim.RunConfig, []servers.Port, error) {
	if kind, port, ok, err := RenderQuery(t, vars); err != nil {
		return shim.RunConfig{}, nil, err
	} else if ok {
		vars[KeyQueryKind] = kind
		vars[KeyQueryPort] = strconv.Itoa(port)
	}
	var run shim.RunConfig
	var err error
	if t.Start.Shell != "" {
		run.Command = "/bin/bash"
		run.Args = []string{"-c", ShellReferences(t.Start.Shell)}
		run.Env = ShellEnv(vars)

		used := map[string]bool{}
		for _, k := range placeholders(t.Start.Shell) {
			used[k] = true
		}
		for _, v := range t.Variables {
			if v.Type == TypeSecret && !used[v.Key] {
				delete(run.Env, v.Key)
			}
		}
	} else {
		if run.Command, err = Render(t.Start.Command, vars); err != nil {
			return run, nil, fmt.Errorf("start.command: %w", err)
		}
		if run.Args, err = RenderArgs(t.Start.Args, vars); err != nil {
			return run, nil, fmt.Errorf("start.args: %w", err)
		}
	}
	if run.Args == nil {
		run.Args = []string{}
	}
	if len(t.Start.Env) > 0 {
		if run.Env == nil {
			run.Env = map[string]string{}
		}
		for k, v := range t.Start.Env {
			if run.Env[k], err = Render(v, vars); err != nil {
				return run, nil, fmt.Errorf("start.env.%s: %w", k, err)
			}
		}
	}
	run.Stop = shim.StopConfig{Command: t.Stop.Command, TimeoutSec: t.Stop.TimeoutSec}
	run.ReadyRegex = t.ReadyRegex
	run.Dir = t.Start.Dir
	if err := run.Validate(); err != nil {
		return run, nil, err
	}
	ports, err := RenderPorts(t, vars)
	if err != nil {
		return run, nil, err
	}
	return run, ports, nil
}

func RenderPorts(t *Template, vars map[string]string) ([]servers.Port, error) {
	ports := make([]servers.Port, 0, len(t.Ports))
	for _, p := range t.Ports {
		rendered, err := Render(p.Port, vars)
		if err != nil {
			return nil, fmt.Errorf("port %s: %w", p.Name, err)
		}
		n, err := strconv.Atoi(rendered)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("port %s: %q is not a valid port", p.Name, rendered)
		}
		ports = append(ports, servers.Port{Name: p.Name, Port: n, Protocol: p.Protocol})
	}
	return ports, nil
}

func PortVariable(t *Template, name string) string {
	for _, p := range t.Ports {
		if p.Name != name {
			continue
		}
		if m := placeholderRe.FindStringSubmatch(p.Port); m != nil && m[0] == p.Port {
			return m[1]
		}
	}
	return ""
}

func ShellReferences(s string) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := placeholderRe.FindStringSubmatch(m)
		if sub[2] != "" {
			return "${" + sub[1] + "}"
		}
		return `"${` + sub[1] + `}"`
	})
}

func ShellEnv(vars map[string]string) map[string]string {
	env := map[string]string{}
	for k, v := range vars {
		if keyRe.MatchString(k) {
			env[k] = v
		}
	}
	path := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	if dir := vars[KeyServerDir]; dir != "" {
		path = dir + "/.venv/bin:" + path
	}
	if bin := vars[KeyRuntimeBin]; bin != "" {
		path = bin + ":" + path
	}
	env["PATH"] = path
	if dir := vars[KeyServerDir]; dir != "" {
		env["HOME"] = dir
	}
	return env
}

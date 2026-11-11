package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/C7PEK/cypek.dev-panel/internal/auth"
	"github.com/C7PEK/cypek.dev-panel/internal/netinfo"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
)

const (
	CodePortInUse     = "port_in_use"
	CodePortInUseHost = "port_in_use_host"
)

type portConflict struct {
	Port       int    `json:"port"`
	Protocol   string `json:"protocol"`
	Name       string `json:"name"`
	ServerID   string `json:"serverId,omitempty"`
	ServerName string `json:"serverName,omitempty"`
	Running    bool   `json:"running"`
	Host       bool   `json:"host"`
	Suggestion int    `json:"suggestion,omitempty"`
}

func protoOverlap(a, b string) bool {
	return strings.Contains(a, "tcp") && strings.Contains(b, "tcp") || strings.Contains(a, "udp") && strings.Contains(b, "udp")
}

func (s *Server) portConflicts(ctx context.Context, selfID string, ports []servers.Port, runningOnly bool) []portConflict {
	if len(ports) == 0 {
		return nil
	}
	list, err := s.servers.List(ctx, nil)
	if err != nil {
		return nil
	}
	active := map[string]bool{}
	for _, srv := range list {
		active[srv.ID] = s.statusOf(ctx, srv.ID).Active()
	}
	bound, _ := netinfo.BoundPorts()
	panelPort := s.listenPort()
	var out []portConflict
	for _, p := range ports {
		var found *portConflict
		for _, other := range list {
			if other.ID == selfID || runningOnly && !active[other.ID] {
				continue
			}
			for _, op := range other.Ports {
				if op.Port == p.Port && protoOverlap(op.Protocol, p.Protocol) {
					found = &portConflict{Port: p.Port, Protocol: p.Protocol, Name: p.Name, ServerID: other.ID, ServerName: other.Name, Running: active[other.ID]}
					break
				}
			}
			if found != nil {
				break
			}
		}
		if found == nil && (bound.Has(p.Port, p.Protocol) || p.Port == panelPort && strings.Contains(p.Protocol, "tcp")) {
			explained := false
			for _, srv := range list {
				if !active[srv.ID] {
					continue
				}
				for _, op := range srv.Ports {
					if op.Port == p.Port && protoOverlap(op.Protocol, p.Protocol) {
						explained = true
					}
				}
			}
			if !explained {
				found = &portConflict{Port: p.Port, Protocol: p.Protocol, Name: p.Name, Host: true, Running: true}
			}
		}
		if found != nil {
			found.Suggestion = freePort(p, list, bound, panelPort)
			out = append(out, *found)
		}
	}
	return out
}

func freePort(p servers.Port, list []*servers.Server, bound netinfo.Bound, panelPort int) int {
	for cand := p.Port + 1; cand <= 65535 && cand <= p.Port+200; cand++ {
		if bound.Has(cand, p.Protocol) || cand == panelPort {
			continue
		}
		taken := false
		for _, srv := range list {
			for _, op := range srv.Ports {
				if op.Port == cand && protoOverlap(op.Protocol, p.Protocol) {
					taken = true
				}
			}
		}
		if !taken {
			return cand
		}
	}
	return 0
}

func (s *Server) listenPort() int {
	if s.cfg == nil {
		return 0
	}
	addr := s.cfg.Listen
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		var n int
		fmt.Sscan(addr[i+1:], &n)
		return n
	}
	return 0
}

func (s *Server) writePortConflict(w http.ResponseWriter, r *http.Request, c portConflict, field string, forceHint bool) {
	params := map[string]any{"port": c.Port, "protocol": c.Protocol, "suggestion": c.Suggestion, "canForce": forceHint}
	code, msg := CodePortInUse, ""
	if c.Host {
		code = CodePortInUseHost
		msg = fmt.Sprintf("Port %d is already used by another program on this machine.", c.Port)
	} else {
		name := c.ServerName
		if !s.userSeesServer(r, c.ServerID) {
			name = ""
		}
		params["server"] = name
		params["running"] = c.Running
		if name == "" {
			msg = fmt.Sprintf("Port %d is already used by another server.", c.Port)
		} else {
			msg = fmt.Sprintf("Port %d is already used by the server %q.", c.Port, name)
		}
	}
	if c.Suggestion > 0 {
		msg += fmt.Sprintf(" Port %d is free.", c.Suggestion)
	}
	writeJSON(w, http.StatusConflict, errorBody{Error: errorDetail{Code: code, Message: msg, Field: field, Params: params}})
}

func (s *Server) userSeesServer(r *http.Request, id string) bool {
	_, user := sessionFromContext(r.Context())
	if user == nil {
		return false
	}
	if user.Role == auth.RoleOwner || user.Role == auth.RoleAdmin {
		return true
	}
	ids, all, err := s.store.VisibleServerIDs(r.Context(), user)
	if err != nil {
		return false
	}
	if all {
		return true
	}
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func (s *Server) handlePortsCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ports    []servers.Port `json:"ports"`
		ServerID string         `json:"serverId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.Ports) > 64 {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Too many ports.")
		return
	}
	for _, p := range in.Ports {
		if p.Port < 1 || p.Port > 65535 {
			writeFieldError(w, CodeValidation, "ports", "Ports must be between 1 and 65535.")
			return
		}
	}
	conflicts := s.portConflicts(r.Context(), in.ServerID, in.Ports, false)
	if conflicts == nil {
		conflicts = []portConflict{}
	}
	for i := range conflicts {
		if !conflicts[i].Host && !s.userSeesServer(r, conflicts[i].ServerID) {
			conflicts[i].ServerID, conflicts[i].ServerName = "", ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conflicts": conflicts})
}

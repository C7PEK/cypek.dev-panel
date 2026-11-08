package api

import (
	"net/http"
	"os"
	"runtime"

	"github.com/cypek019/cypek.dev---panel/internal/audit"
	"github.com/cypek019/cypek.dev---panel/internal/netinfo"
)

type versionResponse struct {
	Version string `json:"version"`
}

type systemInfoResponse struct {
	Version   string            `json:"version"`
	Hostname  string            `json:"hostname"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	GoVersion string            `json:"goVersion"`
	Dev       bool              `json:"dev"`
	Addresses []netinfo.Address `json:"addresses"`
	URLs      []string          `json:"urls"`
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, versionResponse{Version: s.version})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	addrs, _ := netinfo.LocalAddresses()
	writeJSON(w, http.StatusOK, systemInfoResponse{
		Version:   s.version,
		Hostname:  hostname,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		Dev:       s.cfg.Dev,
		Addresses: addrs,
		URLs:      netinfo.URLs(s.cfg.Listen, s.cfg.TLS.Enabled, addrs),
	})
}

func (s *Server) handleAuditRecent(w http.ResponseWriter, r *http.Request) {
	records, err := s.audit.Recent(r.Context(), 100)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if records == nil {
		records = []audit.Record{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": records})
}

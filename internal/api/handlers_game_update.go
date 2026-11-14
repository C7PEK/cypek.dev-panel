package api

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
	"github.com/C7PEK/cypek.dev-panel/internal/safefs"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

var fivemEnhancedClient = providers.NewFiveMEnhanced()

type gameVersion struct {
	Build   int    `json:"build,omitempty"`
	TxAdmin string `json:"txAdmin,omitempty"`
}

type updateInfo struct {
	Kind       string       `json:"kind"`
	Channel    string       `json:"channel,omitempty"`
	Current    gameVersion  `json:"current"`
	Latest     *gameVersion `json:"latest,omitempty"`
	UpToDate   *bool        `json:"upToDate,omitempty"`
	CheckError string       `json:"checkError,omitempty"`
}

var txAdminManifests = []string{
	"artifact/alpine/opt/cfx-server/citizen/system_resources/monitor/fxmanifest.lua",
	"artifact/alpine/opt/cfx-server/system_resources/txadmin/fxmanifest.lua",
}

var manifestVersionRe = regexp.MustCompile(`(?m)^\s*version\s+['"]([^'"]{1,64})['"]`)

func installedTxAdmin(dir string) string {
	for _, rel := range txAdminManifests {
		b, err := safefs.ReadFile(dir, rel, 64<<10)
		if err != nil {
			continue
		}
		if m := manifestVersionRe.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return ""
}

func updateProvider(tpl *templates.Template) *templates.DownloadStep {
	for _, st := range tpl.Update {
		if st.Download != nil && (st.Download.Provider == "fivem" || st.Download.Provider == "fivem-enhanced") {
			return st.Download
		}
	}
	return nil
}

func (s *Server) handleGameUpdateInfo(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	tpl, ok := s.templates.Get(srv.TemplateID)
	if !ok || !isTemplateServer(srv) || !tpl.HasUpdate() {
		writeError(w, http.StatusConflict, CodeNoUpdate, "This server has no automatic update. Reinstall it in Settings instead.")
		return
	}
	info := updateInfo{Kind: "files"}
	if b, err := strconv.Atoi(srv.Variables["FX_BUILD"]); err == nil {
		info.Current.Build = b
	}
	info.Current.TxAdmin = installedTxAdmin(s.servers.Dir(srv.ID))
	dl := updateProvider(tpl)
	if dl == nil {
		writeJSON(w, http.StatusOK, info)
		return
	}
	info.Kind = dl.Provider
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var latest *providers.FiveMArtifact
	var err error
	if dl.Provider == "fivem-enhanced" {
		latest, err = fivemEnhancedClient.Resolve(ctx)
	} else {
		userVars, _, verr := s.splitVars(tpl, srv.Variables)
		if verr != nil {
			s.internalError(w, r, verr)
			return
		}
		selector, rerr := templates.Render(dl.Version, userVars)
		if rerr != nil || selector == "" {
			selector = "recommended"
		}
		info.Channel = selector
		latest, err = s.fivem.Resolve(ctx, selector)
	}
	if err != nil {
		info.CheckError = err.Error()
		writeJSON(w, http.StatusOK, info)
		return
	}
	info.Latest = &gameVersion{Build: latest.Build, TxAdmin: latest.TxAdmin}
	if info.Current.Build > 0 && latest.Build > 0 {
		up := info.Current.Build >= latest.Build
		info.UpToDate = &up
	}
	writeJSON(w, http.StatusOK, info)
}

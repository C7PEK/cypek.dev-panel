package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/importer"
	"github.com/C7PEK/cypek.dev-panel/internal/installer"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

const (
	CodeTemplateInvalid = "template_invalid"
	CodeTemplateExists  = "template_exists"
	CodeTemplateInUse   = "template_in_use"
	CodeImportFailed    = "import_failed"
)

type templateItem struct {
	*templates.Template
	Source           templates.Source `json:"source"`
	Editable         bool             `json:"editable"`
	OverridesBuiltin bool             `json:"overridesBuiltin"`
	UsesScript       bool             `json:"usesScript"`
}

func (s *Server) templateItem(t *templates.Template) templateItem {
	it := templateItem{
		Template:         t,
		Editable:         s.templates.IsUser(t.ID),
		OverridesBuiltin: s.templates.Overrides(t.ID),
		UsesScript:       t.UsesScript(),
	}
	switch {
	case t.Builtin:
		it.Source = templates.Source{Kind: "builtin"}
	case t.Source != nil && t.Source.Kind != "":
		it.Source = *t.Source
	default:
		it.Source = templates.Source{Kind: "custom"}
	}
	return it
}

func (s *Server) handleTemplateYAML(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, ok := s.templates.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, CodeNotFound, "Unknown template.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"yaml":             string(t.Raw),
		"editable":         s.templates.IsUser(id),
		"builtin":          t.Builtin,
		"overridesBuiltin": s.templates.Overrides(id),
	})
}

type yamlInput struct {
	YAML     string `json:"yaml"`
	Override bool   `json:"override"`
}

func parseTemplateInput(w http.ResponseWriter, r *http.Request) (*yamlInput, *templates.Template, bool) {
	var in yamlInput
	if !decodeJSON(w, r, &in) {
		return nil, nil, false
	}
	t, err := templates.Parse([]byte(in.YAML))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, CodeTemplateInvalid, err.Error())
		return nil, nil, false
	}
	if t.ID == customTemplateID {
		writeError(w, http.StatusUnprocessableEntity, CodeTemplateInvalid, `The id "custom" is reserved; choose another id.`)
		return nil, nil, false
	}
	return &in, t, true
}

func (s *Server) handleValidateTemplate(w http.ResponseWriter, r *http.Request) {
	_, t, ok := parseTemplateInput(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"template": s.templateItem(t), "warnings": hostToolWarnings(t)})
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	in, t, ok := parseTemplateInput(w, r)
	if !ok {
		return
	}
	if s.templates.IsUser(t.ID) {
		writeError(w, http.StatusConflict, CodeTemplateExists, fmt.Sprintf("A template with the id %q already exists; change the id or edit that template.", t.ID))
		return
	}
	if _, isBuiltin := s.templates.Builtin(t.ID); isBuiltin && !in.Override {
		writeError(w, http.StatusConflict, CodeTemplateExists, fmt.Sprintf("%q is a built-in template; saving replaces it until you delete your copy.", t.ID))
		return
	}
	s.saveTemplate(w, r, in.YAML, "template.create", http.StatusCreated)
}

func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.templates.IsUser(id) {
		if _, ok := s.templates.Get(id); ok {
			writeError(w, http.StatusForbidden, CodeForbidden, "Built-in templates cannot be edited; duplicate it instead.")
			return
		}
		writeError(w, http.StatusNotFound, CodeNotFound, "Unknown template.")
		return
	}
	in, t, ok := parseTemplateInput(w, r)
	if !ok {
		return
	}
	if t.ID != id {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "The id inside the YAML must stay "+id+"; to rename, duplicate the template.")
		return
	}
	s.saveTemplate(w, r, in.YAML, "template.update", http.StatusOK)
}

func (s *Server) saveTemplate(w http.ResponseWriter, r *http.Request, yamlText, action string, status int) {
	t, err := s.templates.Save([]byte(yamlText))
	switch {
	case errors.Is(err, templates.ErrNoUserDir):
		writeError(w, http.StatusConflict, CodeConflict, "Custom templates are not enabled on this panel (no template directory configured).")
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, CodeTemplateInvalid, err.Error())
		return
	case t == nil:
		writeError(w, http.StatusInternalServerError, CodeInternal, "The template was saved but could not be loaded back.")
		return
	}
	_, user := sessionFromContext(r.Context())
	details := map[string]any{"name": t.Name}
	if t.Source != nil && t.Source.Kind != "" {
		details["source"] = t.Source.Kind
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: action, Target: t.ID, IP: clientIP(r), Details: details})
	writeJSON(w, status, map[string]any{"template": s.templateItem(t)})
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.templates.IsUser(id) {
		if _, ok := s.templates.Get(id); ok {
			writeError(w, http.StatusForbidden, CodeForbidden, "Built-in templates cannot be deleted.")
			return
		}
		writeError(w, http.StatusNotFound, CodeNotFound, "Unknown template.")
		return
	}
	// Deleting an override falls back to the built-in, so servers keep a
	// template; only a plain custom template can leave servers orphaned.
	if !s.templates.Overrides(id) && r.URL.Query().Get("force") != "1" {
		list, err := s.servers.List(r.Context(), nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, CodeInternal, "Could not list servers.")
			return
		}
		var names []string
		for _, srv := range list {
			if srv.TemplateID == id {
				names = append(names, srv.Name)
			}
		}
		if len(names) > 0 {
			writeError(w, http.StatusConflict, CodeTemplateInUse, fmt.Sprintf("Used by %d server(s): %s. They keep running, but reinstalling them will not be possible.", len(names), strings.Join(names, ", ")))
			return
		}
	}
	if err := s.templates.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal, "Could not delete the template: "+err.Error())
		return
	}
	_, user := sessionFromContext(r.Context())
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "template.delete", Target: id, IP: clientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

type importInput struct {
	Content string `json:"content"`
	URL     string `json:"url"`
}

func (s *Server) handleImportTemplate(w http.ResponseWriter, r *http.Request) {
	var in importInput
	if !decodeJSON(w, r, &in) {
		return
	}
	data := []byte(in.Content)
	source := ""
	if strings.TrimSpace(in.URL) != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		u, body, err := fetchTemplateURL(ctx, strings.TrimSpace(in.URL))
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, CodeImportFailed, err.Error())
			return
		}
		data, source = body, u
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Paste the template, upload a file or give a link.")
		return
	}
	res, err := importer.Import(data, importer.Options{SourceURL: source})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, CodeImportFailed, err.Error())
		return
	}
	warnings := append(append([]string{}, res.Warnings...), hostToolWarnings(res.Template)...)
	writeJSON(w, http.StatusOK, map[string]any{
		"yaml":     string(res.YAML),
		"format":   res.Format,
		"warnings": warnings,
		"template": s.templateItem(res.Template),
	})
}

// hostToolWarnings names programs the template's scripts call that this
// host does not have; package installs inside scripts are skipped.
func hostToolWarnings(t *templates.Template) []string {
	var script strings.Builder
	for _, st := range t.Install {
		if st.Script != nil {
			script.WriteString(st.Script.Content)
		}
	}
	missing := installer.MissingTools(script.String())
	if len(missing) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("The install script uses %s, which is not installed on this machine. Install it first: sudo apt install %s", strings.Join(missing, ", "), strings.Join(missing, " "))}
}

// fetchTemplateURL downloads a template over https from a public address
// (GitHub "blob" links are turned into raw links).
func fetchTemplateURL(ctx context.Context, raw string) (string, []byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", nil, errors.New("only https:// links are supported")
	}
	if u.Host == "github.com" {
		// https://github.com/o/r/blob/branch/path -> raw.githubusercontent.com/o/r/branch/path
		parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 4)
		if len(parts) == 4 && (parts[2] == "blob" || parts[2] == "raw") {
			u = &url.URL{Scheme: "https", Host: "raw.githubusercontent.com", Path: "/" + parts[0] + "/" + parts[1] + "/" + parts[3]}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "panel.cypek.dev template import")
	resp, err := publicHTTPClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("could not download the template: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("could not download the template: HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, importer.MaxSize+1))
	if err != nil {
		return "", nil, fmt.Errorf("could not download the template: %w", err)
	}
	if len(body) > importer.MaxSize {
		return "", nil, errors.New("the file is too large for a template")
	}
	return u.String(), body, nil
}

// publicHTTPClient refuses to connect to loopback, private and link-local
// addresses so a template link cannot be used to probe the LAN.
var publicHTTPClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				ip := net.ParseIP(host)
				if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || inBlocked(ip) {
					return fmt.Errorf("refusing to connect to %s (not a public address)", host)
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("redirect to a non-https link")
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// blockedNets: shared/CGNAT (Tailscale 100.64/10), benchmarking, "this"
// network and NAT64, none of which a public template host uses.
var blockedNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"100.64.0.0/10", "198.18.0.0/15", "0.0.0.0/8", "64:ff9b::/96", "64:ff9b:1::/48"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

func inBlocked(ip net.IP) bool {
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

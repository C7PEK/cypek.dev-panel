package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cypek019/cypek.dev---panel/internal/audit"
	"github.com/cypek019/cypek.dev---panel/internal/mods"
	"github.com/cypek019/cypek.dev---panel/internal/servers"
)

const (
	CodeNotMinecraft    = "not_minecraft"
	CodePlatformUnknown = "platform_unknown"
	CodeNoCompatible    = "no_compatible_version"
	modrinthTimeout     = 30 * time.Second
)

// modsPlatform resolves the add-on platform of the server in the request,
// writing an error for non-Minecraft servers.
func (s *Server) modsPlatform(w http.ResponseWriter, r *http.Request) (*servers.Server, string, mods.Platform, bool) {
	srv := serverFromContext(r.Context())
	if srv.Category != servers.CategoryMinecraft {
		writeError(w, http.StatusConflict, CodeNotMinecraft, "Plugins and mods are only available for Minecraft servers.")
		return nil, "", mods.Platform{}, false
	}
	dir := s.servers.Dir(srv.ID)
	return srv, dir, mods.Detect(dir, mods.Hints{TemplateID: srv.TemplateID, GameVersion: srv.Variables["MC_VERSION_RESOLVED"]}), true
}

func (s *Server) modsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mods.ErrPlatformUnknown):
		writeError(w, http.StatusConflict, CodePlatformUnknown, err.Error())
	case errors.Is(err, mods.ErrNoCompatible):
		writeError(w, http.StatusConflict, CodeNoCompatible, "There is no version of it for this server type and Minecraft version.")
	case errors.Is(err, mods.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeNotFound, "No such plugin/mod file.")
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "modrinth"), strings.Contains(err.Error(), "download"):
		writeError(w, http.StatusBadGateway, CodeUpstream, "Modrinth did not answer correctly: "+err.Error())
	default:
		writeError(w, http.StatusUnprocessableEntity, CodeValidation, err.Error())
	}
}

func (s *Server) handleModsList(w http.ResponseWriter, r *http.Request) {
	_, dir, p, ok := s.modsPlatform(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), modrinthTimeout)
	defer cancel()
	items, err := s.mods.List(ctx, dir, p)
	if err != nil {
		s.modsError(w, err)
		return
	}
	if p.Loaders == nil {
		p.Loaders = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"platform": p, "installed": items})
}

func (s *Server) handleModsUpdates(w http.ResponseWriter, r *http.Request) {
	_, dir, p, ok := s.modsPlatform(w, r)
	if !ok {
		return
	}
	if !p.Known() {
		writeJSON(w, http.StatusOK, map[string]any{"updates": map[string]any{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), modrinthTimeout)
	defer cancel()
	ups, err := s.mods.Updates(ctx, dir, p)
	if err != nil {
		s.modsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updates": ups})
}

type modHit struct {
	ProjectID   string   `json:"projectId"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	IconURL     string   `json:"iconUrl"`
	Downloads   int64    `json:"downloads"`
	Categories  []string `json:"categories"`
	Installed   bool     `json:"installed"`
	URL         string   `json:"url"`
}

var modSorts = map[string]bool{"relevance": true, "downloads": true, "updated": true, "newest": true, "follows": true}

func (s *Server) handleModsSearch(w http.ResponseWriter, r *http.Request) {
	_, dir, p, ok := s.modsPlatform(w, r)
	if !ok {
		return
	}
	if !p.Known() {
		s.modsError(w, mods.ErrPlatformUnknown)
		return
	}
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("q"))
	if len(query) > 100 {
		query = query[:100]
	}
	sort := q.Get("sort")
	if !modSorts[sort] {
		sort = "relevance"
		if query == "" {
			sort = "downloads"
		}
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if offset < 0 || offset > 10000 {
		offset = 0
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(r.Context(), modrinthTimeout)
	defer cancel()
	res, err := s.mods.API.Search(ctx, query, p.ProjectType(), p.Loaders, p.GameVersion, p.Kind == mods.KindMods, sort, offset, limit)
	if err != nil {
		s.modsError(w, err)
		return
	}
	installed := mods.InstalledProjects(dir)
	hits := make([]modHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		hits = append(hits, modHit{
			ProjectID: h.ProjectID, Slug: h.Slug, Title: h.Title, Description: h.Description, Author: h.Author,
			IconURL: h.IconURL, Downloads: h.Downloads, Categories: h.Categories, Installed: installed[h.ProjectID],
			URL: "https://modrinth.com/" + p.ProjectType() + "/" + url.PathEscape(h.Slug),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "total": res.TotalHits, "offset": offset, "limit": limit})
}

func (s *Server) handleModVersions(w http.ResponseWriter, r *http.Request) {
	_, _, p, ok := s.modsPlatform(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), modrinthTimeout)
	defer cancel()
	vs, err := s.mods.CompatibleVersions(ctx, p, chi.URLParam(r, "pid"))
	if err != nil {
		s.modsError(w, err)
		return
	}
	type version struct {
		ID           string   `json:"id"`
		Number       string   `json:"number"`
		Name         string   `json:"name"`
		Type         string   `json:"type"`
		Date         string   `json:"date"`
		GameVersions []string `json:"gameVersions"`
	}
	out := make([]version, 0, len(vs))
	for _, v := range vs {
		gv := v.GameVersions
		if len(gv) > 8 {
			gv = gv[len(gv)-8:] // newest ones are last
		}
		out = append(out, version{ID: v.ID, Number: v.VersionNumber, Name: v.Name, Type: v.VersionType, Date: v.DatePublished, GameVersions: gv})
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": out})
}

type modInstallInput struct {
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId"`
}

func (s *Server) handleModInstall(w http.ResponseWriter, r *http.Request) {
	srv, dir, p, ok := s.modsPlatform(w, r)
	if !ok {
		return
	}
	var in modInstallInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if !validModrinthID(in.ProjectID) || (in.VersionID != "" && !validModrinthID(in.VersionID)) {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Invalid project or version id.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	res, err := s.mods.Install(ctx, dir, p, in.ProjectID, in.VersionID)
	if err != nil {
		s.modsError(w, err)
		return
	}
	s.auditMods(r, srv, "mods.install", res)
	s.writeModResult(w, r, srv, res)
}

func (s *Server) handleModUpdate(w http.ResponseWriter, r *http.Request) {
	srv, dir, p, ok := s.modsPlatform(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	res, err := s.mods.UpdateFile(ctx, dir, p, modFileParam(r))
	if err != nil {
		s.modsError(w, err)
		return
	}
	s.auditMods(r, srv, "mods.update", res)
	s.writeModResult(w, r, srv, res)
}

func (s *Server) writeModResult(w http.ResponseWriter, r *http.Request, srv *servers.Server, res *mods.Result) {
	writeJSON(w, http.StatusOK, map[string]any{
		"installed":     res.Installed,
		"removed":       res.Removed,
		"warnings":      res.Warnings,
		"restartNeeded": s.statusOf(r.Context(), srv.ID).Active(),
	})
}

func (s *Server) auditMods(r *http.Request, srv *servers.Server, action string, res *mods.Result) {
	_, user := sessionFromContext(r.Context())
	var names []string
	for _, f := range res.Installed {
		names = append(names, f.File)
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: action, Target: srv.ID, IP: clientIP(r), Details: map[string]any{"files": names, "removed": res.Removed}})
}

func modFileParam(r *http.Request) string {
	f := chi.URLParam(r, "file")
	if u, err := url.PathUnescape(f); err == nil {
		f = u
	}
	return f
}

func (s *Server) handleModToggle(w http.ResponseWriter, r *http.Request) {
	srv, dir, p, ok := s.modsPlatform(w, r)
	if !ok {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	file := modFileParam(r)
	name, err := s.mods.SetEnabled(dir, p, file, in.Enabled)
	if err != nil {
		s.modsError(w, err)
		return
	}
	_, user := sessionFromContext(r.Context())
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "mods.toggle", Target: srv.ID, IP: clientIP(r), Details: map[string]any{"file": file, "enabled": in.Enabled}})
	writeJSON(w, http.StatusOK, map[string]any{"file": name})
}

func (s *Server) handleModDelete(w http.ResponseWriter, r *http.Request) {
	srv, dir, p, ok := s.modsPlatform(w, r)
	if !ok {
		return
	}
	file := modFileParam(r)
	if err := s.mods.Remove(dir, p, file); err != nil {
		s.modsError(w, err)
		return
	}
	_, user := sessionFromContext(r.Context())
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "mods.remove", Target: srv.ID, IP: clientIP(r), Details: map[string]any{"file": file}})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleModPlatform(w http.ResponseWriter, r *http.Request) {
	srv, dir, _, ok := s.modsPlatform(w, r)
	if !ok {
		return
	}
	var in struct {
		Loader      string `json:"loader"`
		GameVersion string `json:"gameVersion"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.mods.SetPlatform(dir, in.Loader, in.GameVersion); err != nil {
		writeError(w, http.StatusUnprocessableEntity, CodeValidation, err.Error())
		return
	}
	p := mods.Detect(dir, mods.Hints{TemplateID: srv.TemplateID, GameVersion: srv.Variables["MC_VERSION_RESOLVED"]})
	if p.Loaders == nil {
		p.Loaders = []string{}
	}
	writeJSON(w, http.StatusOK, p)
}

// Game versions change a few times a year; cache them for an hour.
var gameVersionsCache struct {
	sync.Mutex
	at   time.Time
	list []string
}

func (s *Server) handleGameVersions(w http.ResponseWriter, r *http.Request) {
	c := &gameVersionsCache
	c.Lock()
	defer c.Unlock()
	if time.Since(c.at) > time.Hour || len(c.list) == 0 {
		ctx, cancel := context.WithTimeout(r.Context(), modrinthTimeout)
		defer cancel()
		list, err := s.mods.API.GameVersions(ctx)
		if err != nil {
			if len(c.list) == 0 {
				writeError(w, http.StatusBadGateway, CodeUpstream, "Could not fetch Minecraft versions: "+err.Error())
				return
			}
		} else {
			c.list, c.at = list, time.Now()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": c.list})
}

func validModrinthID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

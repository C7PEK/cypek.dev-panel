package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// Modrinth is a small client for the Modrinth v2 API (plugins and mods).
// Modrinth asks for a User-Agent that identifies the project; UserAgent does.
type Modrinth struct{ BaseURL string }

func NewModrinth() *Modrinth { return &Modrinth{BaseURL: "https://api.modrinth.com/v2"} }

type ModrinthHit struct {
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	IconURL     string   `json:"icon_url"`
	Downloads   int64    `json:"downloads"`
	Categories  []string `json:"categories"`
	ProjectType string   `json:"project_type"`
}

type ModrinthSearch struct {
	Hits      []ModrinthHit `json:"hits"`
	TotalHits int           `json:"total_hits"`
	Offset    int           `json:"offset"`
	Limit     int           `json:"limit"`
}

type ModrinthFile struct {
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
	Hashes   map[string]string `json:"hashes"`
}

type ModrinthDependency struct {
	ProjectID      string `json:"project_id"`
	VersionID      string `json:"version_id"`
	DependencyType string `json:"dependency_type"` // required | optional | incompatible | embedded
}

type ModrinthVersion struct {
	ID            string               `json:"id"`
	ProjectID     string               `json:"project_id"`
	Name          string               `json:"name"`
	VersionNumber string               `json:"version_number"`
	VersionType   string               `json:"version_type"` // release | beta | alpha
	DatePublished string               `json:"date_published"`
	GameVersions  []string             `json:"game_versions"`
	Loaders       []string             `json:"loaders"`
	Files         []ModrinthFile       `json:"files"`
	Dependencies  []ModrinthDependency `json:"dependencies"`
}

// PrimaryFile is the file Modrinth marks primary (or the first one).
func (v *ModrinthVersion) PrimaryFile() *ModrinthFile {
	for i := range v.Files {
		if v.Files[i].Primary {
			return &v.Files[i]
		}
	}
	if len(v.Files) > 0 {
		return &v.Files[0]
	}
	return nil
}

type ModrinthProject struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	IconURL     string `json:"icon_url"`
	ProjectType string `json:"project_type"`
}

func jsonArray(items []string) string {
	b, _ := json.Marshal(items)
	return string(b)
}

// Search runs a faceted search. projectType is "plugin" or "mod"; loaders
// are OR-ed; gameVersion may be empty; serverSide drops client-only mods.
func (m *Modrinth) Search(ctx context.Context, query, projectType string, loaders []string, gameVersion string, serverSide bool, sort string, offset, limit int) (*ModrinthSearch, error) {
	facets := [][]string{{"project_type:" + projectType}}
	if len(loaders) > 0 {
		var or []string
		for _, l := range loaders {
			or = append(or, "categories:"+l)
		}
		facets = append(facets, or)
	}
	if gameVersion != "" {
		facets = append(facets, []string{"versions:" + gameVersion})
	}
	if serverSide {
		facets = append(facets, []string{"server_side:required", "server_side:optional"})
	}
	fb, _ := json.Marshal(facets)
	q := url.Values{}
	q.Set("query", query)
	q.Set("facets", string(fb))
	q.Set("index", sort)
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))
	var out ModrinthSearch
	if err := getJSON(ctx, m.BaseURL+"/search?"+q.Encode(), &out, decodeJSON); err != nil {
		return nil, fmt.Errorf("modrinth search: %w", err)
	}
	return &out, nil
}

// Versions lists a project's versions for the loaders / game version,
// newest first (Modrinth's order).
func (m *Modrinth) Versions(ctx context.Context, projectID string, loaders []string, gameVersion string) ([]ModrinthVersion, error) {
	q := url.Values{}
	if len(loaders) > 0 {
		q.Set("loaders", jsonArray(loaders))
	}
	if gameVersion != "" {
		q.Set("game_versions", jsonArray([]string{gameVersion}))
	}
	var out []ModrinthVersion
	if err := getJSON(ctx, m.BaseURL+"/project/"+url.PathEscape(projectID)+"/version?"+q.Encode(), &out, decodeJSON); err != nil {
		return nil, fmt.Errorf("modrinth versions of %s: %w", projectID, err)
	}
	return out, nil
}

func (m *Modrinth) Version(ctx context.Context, versionID string) (*ModrinthVersion, error) {
	var out ModrinthVersion
	if err := getJSON(ctx, m.BaseURL+"/version/"+url.PathEscape(versionID), &out, decodeJSON); err != nil {
		return nil, fmt.Errorf("modrinth version %s: %w", versionID, err)
	}
	return &out, nil
}

func (m *Modrinth) Projects(ctx context.Context, ids []string) ([]ModrinthProject, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []ModrinthProject
	if err := getJSON(ctx, m.BaseURL+"/projects?ids="+url.QueryEscape(jsonArray(ids)), &out, decodeJSON); err != nil {
		return nil, fmt.Errorf("modrinth projects: %w", err)
	}
	return out, nil
}

// VersionsByHash identifies files by SHA-1; unknown hashes are absent.
func (m *Modrinth) VersionsByHash(ctx context.Context, sha1s []string) (map[string]ModrinthVersion, error) {
	out := map[string]ModrinthVersion{}
	if len(sha1s) == 0 {
		return out, nil
	}
	err := m.postJSON(ctx, "/version_files", map[string]any{"hashes": sha1s, "algorithm": "sha1"}, &out)
	return out, err
}

// LatestByHash returns, per SHA-1, the newest version compatible with the
// loaders and game version.
func (m *Modrinth) LatestByHash(ctx context.Context, sha1s, loaders []string, gameVersion string) (map[string]ModrinthVersion, error) {
	out := map[string]ModrinthVersion{}
	if len(sha1s) == 0 {
		return out, nil
	}
	body := map[string]any{"hashes": sha1s, "algorithm": "sha1", "loaders": loaders}
	if gameVersion != "" {
		body["game_versions"] = []string{gameVersion}
	}
	err := m.postJSON(ctx, "/version_files/update", body, &out)
	return out, err
}

// GameVersions lists Minecraft release versions, newest first.
func (m *Modrinth) GameVersions(ctx context.Context) ([]string, error) {
	var tags []struct {
		Version     string `json:"version"`
		VersionType string `json:"version_type"`
	}
	if err := getJSON(ctx, m.BaseURL+"/tag/game_version", &tags, decodeJSON); err != nil {
		return nil, fmt.Errorf("modrinth game versions: %w", err)
	}
	var out []string
	for _, t := range tags {
		if t.VersionType == "release" {
			out = append(out, t.Version)
		}
	}
	return out, nil
}

func (m *Modrinth) postJSON(ctx context.Context, path string, body, into any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := Client.Do(req)
	if err != nil {
		return fmt.Errorf("modrinth %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("modrinth %s: HTTP %s", path, resp.Status)
	}
	return decodeJSON(io.LimitReader(resp.Body, 8<<20), into)
}

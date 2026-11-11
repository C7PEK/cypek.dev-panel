package mods

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/C7PEK/cypek.dev-panel/internal/safefs"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/files"
	"github.com/C7PEK/cypek.dev-panel/internal/providers"
)

var (
	ErrPlatformUnknown = errors.New("the server type could not be detected; choose it first")
	ErrNoCompatible    = errors.New("no version is compatible with this server")
	ErrNotFound        = errors.New("no such file")
)

const MaxFileSize = 256 << 20

const (
	stateFile  = "mods.json"
	disabledEx = ".disabled"
)

type fileMeta struct {
	SHA1          string `json:"sha1"`
	Size          int64  `json:"size"`
	ModTime       int64  `json:"modTime"`
	ProjectID     string `json:"projectId,omitempty"`
	Slug          string `json:"slug,omitempty"`
	Title         string `json:"title,omitempty"`
	IconURL       string `json:"iconUrl,omitempty"`
	VersionID     string `json:"versionId,omitempty"`
	VersionNumber string `json:"versionNumber,omitempty"`
	Unknown       bool   `json:"unknown,omitempty"`
}

type state struct {
	Loader      string               `json:"loader,omitempty"`
	GameVersion string               `json:"gameVersion,omitempty"`
	Files       map[string]*fileMeta `json:"files,omitempty"`
}

func loadState(dir string) *state {
	st := &state{}
	if b, err := safefs.ReadFile(dir, filepath.Join(".homepanel", stateFile), 4<<20); err == nil {
		_ = json.Unmarshal(b, st)
	}
	if st.Files == nil {
		st.Files = map[string]*fileMeta{}
	}
	return st
}

func saveState(dir string, st *state) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return safefs.WriteFile(dir, filepath.Join(".homepanel", stateFile), b, 0o640)
}

type Manager struct {
	API *providers.Modrinth
	mu  sync.Mutex
	per map[string]*sync.Mutex
}

func NewManager(api *providers.Modrinth) *Manager {
	return &Manager{API: api, per: map[string]*sync.Mutex{}}
}

func (m *Manager) lock(dir string) func() {
	m.mu.Lock()
	l, ok := m.per[dir]
	if !ok {
		l = &sync.Mutex{}
		m.per[dir] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (m *Manager) SetPlatform(dir, loader, gameVersion string) error {
	if loader != "" && !KnownLoader(loader) {
		return fmt.Errorf("unknown server type %q", loader)
	}
	if gameVersion != "" && !ValidGameVersion(gameVersion) {
		return fmt.Errorf("invalid Minecraft version %q", gameVersion)
	}
	defer m.lock(dir)()
	st := loadState(dir)
	st.Loader, st.GameVersion = loader, gameVersion
	return saveState(dir, st)
}

type Installed struct {
	File       string      `json:"file"`
	Enabled    bool        `json:"enabled"`
	Size       int64       `json:"size"`
	ModifiedAt string      `json:"modifiedAt"`
	Project    *ProjectRef `json:"project"`
	Version    *VersionRef `json:"version"`
	key        string
	meta       *fileMeta
}

type ProjectRef struct {
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	IconURL string `json:"iconUrl"`
}

type VersionRef struct {
	ID     string `json:"id"`
	Number string `json:"number"`
}

func (m *Manager) List(ctx context.Context, dir string, p Platform) ([]Installed, error) {
	if !p.Known() {
		return []Installed{}, nil
	}
	defer m.lock(dir)()
	st := loadState(dir)
	items, err := scan(dir, p, st)
	if err != nil {
		return nil, err
	}
	var unknown []string
	for _, it := range items {
		if it.meta.ProjectID == "" && !it.meta.Unknown {
			unknown = append(unknown, it.meta.SHA1)
		}
	}
	if len(unknown) > 0 && m.API != nil {
		_ = m.identify(ctx, st, unknown)
	}

	present := map[string]bool{}
	for _, it := range items {
		present[it.key] = true
	}
	for k := range st.Files {
		if !present[k] {
			delete(st.Files, k)
		}
	}
	_ = saveState(dir, st)
	for i := range items {
		items[i].fill()
	}
	return items, nil
}

func (it *Installed) fill() {
	if it.meta.ProjectID != "" {
		it.Project = &ProjectRef{ID: it.meta.ProjectID, Slug: it.meta.Slug, Title: it.meta.Title, IconURL: it.meta.IconURL}
		it.Version = &VersionRef{ID: it.meta.VersionID, Number: it.meta.VersionNumber}
	}
}

func scan(dir string, p Platform, st *state) ([]Installed, error) {
	root, err := files.ResolveSafe(dir, p.Dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []Installed{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Installed{}
	for _, e := range entries {
		name := e.Name()
		enabled := strings.HasSuffix(strings.ToLower(name), ".jar")
		if !enabled && !strings.HasSuffix(strings.ToLower(name), ".jar"+disabledEx) {
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		key := strings.TrimSuffix(name, disabledEx)
		meta := st.Files[key]
		if meta == nil || meta.Size != info.Size() || meta.ModTime != info.ModTime().UnixNano() {
			sum, err := hashFile(filepath.Join(root, name), sha1.New())
			if err != nil {
				continue
			}
			if meta == nil || meta.SHA1 != sum {
				meta = &fileMeta{SHA1: sum}
			}
			meta.Size, meta.ModTime = info.Size(), info.ModTime().UnixNano()
			st.Files[key] = meta
		}
		out = append(out, Installed{File: name, Enabled: enabled, Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339), key: key, meta: meta})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].File) < strings.ToLower(out[j].File) })
	return out, nil
}

func hashFile(path string, h hash.Hash) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *Manager) identify(ctx context.Context, st *state, sha1s []string) error {
	found, err := m.API.VersionsByHash(ctx, sha1s)
	if err != nil {
		return err
	}
	var pids []string
	for _, v := range found {
		pids = append(pids, v.ProjectID)
	}
	projects := map[string]providers.ModrinthProject{}
	if ps, err := m.API.Projects(ctx, dedupe(pids)); err == nil {
		for _, p := range ps {
			projects[p.ID] = p
		}
	}
	for _, meta := range st.Files {
		if meta.ProjectID != "" || meta.Unknown || !contains(sha1s, meta.SHA1) {
			continue
		}
		v, ok := found[meta.SHA1]
		if !ok {
			meta.Unknown = true
			continue
		}
		pr := projects[v.ProjectID]
		meta.ProjectID, meta.VersionID, meta.VersionNumber = v.ProjectID, v.ID, v.VersionNumber
		meta.Slug, meta.Title, meta.IconURL = pr.Slug, pr.Title, pr.IconURL
	}
	return nil
}

type Update struct {
	VersionID string `json:"versionId"`
	Number    string `json:"number"`
	Name      string `json:"name"`
}

func (m *Manager) Updates(ctx context.Context, dir string, p Platform) (map[string]Update, error) {
	items, err := m.List(ctx, dir, p)
	if err != nil {
		return nil, err
	}
	var sha1s []string
	byHash := map[string]Installed{}
	for _, it := range items {
		if it.meta.ProjectID != "" {
			sha1s = append(sha1s, it.meta.SHA1)
			byHash[it.meta.SHA1] = it
		}
	}
	out := map[string]Update{}
	latest, err := m.API.LatestByHash(ctx, sha1s, p.Loaders, p.GameVersion)
	if err != nil {
		return nil, err
	}
	for h, v := range latest {
		it, ok := byHash[h]
		if !ok || v.ID == it.meta.VersionID {
			continue
		}
		out[it.File] = Update{VersionID: v.ID, Number: v.VersionNumber, Name: v.Name}
	}
	return out, nil
}

type InstalledFile struct {
	File       string `json:"file"`
	Title      string `json:"title"`
	Version    string `json:"version"`
	Dependency bool   `json:"dependency"`
}

type Result struct {
	Installed []InstalledFile `json:"installed"`
	Removed   []string        `json:"removed"`
	Warnings  []string        `json:"warnings,omitempty"`
}

func (m *Manager) Install(ctx context.Context, dir string, p Platform, projectID, versionID string) (*Result, error) {
	if !p.Known() {
		return nil, ErrPlatformUnknown
	}
	defer m.lock(dir)()
	st := loadState(dir)
	if _, err := scan(dir, p, st); err != nil {
		return nil, err
	}
	res := &Result{Installed: []InstalledFile{}, Removed: []string{}}
	v, err := m.pickVersion(ctx, p, projectID, versionID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	if err := m.installTree(ctx, dir, p, st, v, false, seen, 0, res); err != nil {
		return nil, err
	}
	return res, saveState(dir, st)
}

func (m *Manager) UpdateFile(ctx context.Context, dir string, p Platform, file string) (*Result, error) {
	if !p.Known() {
		return nil, ErrPlatformUnknown
	}
	defer m.lock(dir)()
	st := loadState(dir)
	items, err := scan(dir, p, st)
	if err != nil {
		return nil, err
	}
	var it *Installed
	for i := range items {
		if items[i].File == file {
			it = &items[i]
		}
	}
	if it == nil {
		return nil, ErrNotFound
	}
	if it.meta.ProjectID == "" {
		return nil, fmt.Errorf("%s is not from Modrinth, so it cannot be updated here", file)
	}
	latest, err := m.API.LatestByHash(ctx, []string{it.meta.SHA1}, p.Loaders, p.GameVersion)
	if err != nil {
		return nil, err
	}
	v, ok := latest[it.meta.SHA1]
	if !ok {
		return nil, ErrNoCompatible
	}
	res := &Result{Installed: []InstalledFile{}, Removed: []string{}}
	if v.ID == it.meta.VersionID {
		return res, nil
	}
	if err := m.installTree(ctx, dir, p, st, &v, false, map[string]bool{}, 0, res); err != nil {
		return nil, err
	}
	return res, saveState(dir, st)
}

func (m *Manager) pickVersion(ctx context.Context, p Platform, projectID, versionID string) (*providers.ModrinthVersion, error) {
	if versionID != "" {
		v, err := m.API.Version(ctx, versionID)
		if err != nil {
			return nil, err
		}
		if projectID != "" && v.ProjectID != projectID {
			return nil, fmt.Errorf("version %s does not belong to this project", versionID)
		}
		if !compatible(v, p) {
			return nil, ErrNoCompatible
		}
		return v, nil
	}
	versions, err := m.API.Versions(ctx, projectID, p.Loaders, p.GameVersion)
	if err != nil {
		return nil, err
	}
	var pick *providers.ModrinthVersion
	for i := range versions {
		if !compatible(&versions[i], p) {
			continue
		}
		if versions[i].VersionType == "release" {
			return &versions[i], nil
		}
		if pick == nil {
			pick = &versions[i]
		}
	}
	if pick == nil {
		return nil, ErrNoCompatible
	}
	return pick, nil
}

func (m *Manager) CompatibleVersions(ctx context.Context, p Platform, projectID string) ([]providers.ModrinthVersion, error) {
	if !p.Known() {
		return nil, ErrPlatformUnknown
	}
	versions, err := m.API.Versions(ctx, projectID, p.Loaders, p.GameVersion)
	if err != nil {
		return nil, err
	}
	out := versions[:0]
	for _, v := range versions {
		if compatible(&v, p) {
			out = append(out, v)
		}
	}
	return out, nil
}

func compatible(v *providers.ModrinthVersion, p Platform) bool {
	okLoader := false
	for _, l := range v.Loaders {
		if contains(p.Loaders, l) {
			okLoader = true
		}
	}
	if !okLoader {
		return false
	}
	return p.GameVersion == "" || contains(v.GameVersions, p.GameVersion)
}

const maxDepth = 4

func (m *Manager) installTree(ctx context.Context, dir string, p Platform, st *state, v *providers.ModrinthVersion, dep bool, seen map[string]bool, depth int, res *Result) error {
	if seen[v.ProjectID] {
		return nil
	}
	seen[v.ProjectID] = true
	if err := m.installVersion(ctx, dir, p, st, v, dep, res); err != nil {
		return err
	}
	if depth >= maxDepth {
		return nil
	}
	for _, d := range v.Dependencies {
		if d.DependencyType != "required" || d.ProjectID == "" && d.VersionID == "" {
			continue
		}
		if d.ProjectID != "" && (seen[d.ProjectID] || installedProject(st, d.ProjectID)) {
			continue
		}
		dv, err := m.pickVersion(ctx, p, d.ProjectID, d.VersionID)
		if err != nil {
			if d.VersionID != "" && d.ProjectID != "" {
				dv, err = m.pickVersion(ctx, p, d.ProjectID, "")
			}
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("required dependency %s could not be installed: %v", firstNonEmpty(d.ProjectID, d.VersionID), err))
				continue
			}
		}
		if installedProject(st, dv.ProjectID) {
			continue
		}
		if err := m.installTree(ctx, dir, p, st, dv, true, seen, depth+1, res); err != nil {
			return err
		}
	}
	return nil
}

func installedProject(st *state, projectID string) bool {
	for _, meta := range st.Files {
		if meta.ProjectID == projectID {
			return true
		}
	}
	return false
}

func (m *Manager) installVersion(ctx context.Context, dir string, p Platform, st *state, v *providers.ModrinthVersion, dep bool, res *Result) error {
	f := v.PrimaryFile()
	if f == nil {
		return fmt.Errorf("version %s has no file", v.VersionNumber)
	}
	name := filepath.Base(f.Filename)
	if name != f.Filename || !strings.HasSuffix(strings.ToLower(name), ".jar") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("refusing unexpected file name %q", f.Filename)
	}
	if f.Size > MaxFileSize {
		return fmt.Errorf("%s is too large (%d MB)", name, f.Size>>20)
	}
	if err := os.MkdirAll(filepath.Join(dir, p.Dir), 0o750); err != nil {
		return err
	}
	dest, err := files.ResolveSafe(dir, filepath.Join(p.Dir, name))
	if err != nil {
		return err
	}
	var h hash.Hash = sha512.New()
	want := f.Hashes["sha512"]
	if want == "" {
		h, want = sha1.New(), f.Hashes["sha1"]
	}
	if want == "" {
		return fmt.Errorf("%s has no checksum on Modrinth; not installing it", name)
	}
	if err := providers.DownloadVerified(ctx, f.URL, dest, h, want, MaxFileSize, nil); err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}

	for key, meta := range st.Files {
		if meta.ProjectID != v.ProjectID {
			continue
		}
		for _, candidate := range []string{key, key + disabledEx} {
			if candidate == name {
				continue
			}
			if old, err := files.ResolveSafe(dir, filepath.Join(p.Dir, candidate)); err == nil {
				if os.Remove(old) == nil {
					res.Removed = append(res.Removed, candidate)
				}
			}
		}
		if key != name {
			delete(st.Files, key)
		}
	}
	if disabled, err := files.ResolveSafe(dir, filepath.Join(p.Dir, name+disabledEx)); err == nil {
		if os.Remove(disabled) == nil {
			res.Removed = append(res.Removed, name+disabledEx)
		}
	}
	meta := &fileMeta{SHA1: f.Hashes["sha1"], ProjectID: v.ProjectID, VersionID: v.ID, VersionNumber: v.VersionNumber}
	if info, err := os.Stat(dest); err == nil {
		meta.Size, meta.ModTime = info.Size(), info.ModTime().UnixNano()
	}
	if meta.SHA1 == "" {
		meta.SHA1, _ = hashFile(dest, sha1.New())
	}
	if ps, err := m.API.Projects(ctx, []string{v.ProjectID}); err == nil && len(ps) == 1 {
		meta.Slug, meta.Title, meta.IconURL = ps[0].Slug, ps[0].Title, ps[0].IconURL
	}
	st.Files[name] = meta
	res.Installed = append(res.Installed, InstalledFile{File: name, Title: firstNonEmpty(meta.Title, name), Version: v.VersionNumber, Dependency: dep})
	return nil
}

func (m *Manager) SetEnabled(dir string, p Platform, file string, enabled bool) (string, error) {
	if !p.Known() {
		return "", ErrPlatformUnknown
	}
	defer m.lock(dir)()
	src, err := m.jarPath(dir, p, file)
	if err != nil {
		return "", err
	}
	base := strings.TrimSuffix(file, disabledEx)
	target := base
	if !enabled {
		target = base + disabledEx
	}
	if target == file {
		return file, nil
	}
	dst, err := files.ResolveSafe(dir, filepath.Join(p.Dir, target))
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(dst); err == nil {
		return "", fmt.Errorf("%s already exists", target)
	}
	return target, os.Rename(src, dst)
}

func (m *Manager) Remove(dir string, p Platform, file string) error {
	if !p.Known() {
		return ErrPlatformUnknown
	}
	defer m.lock(dir)()
	path, err := m.jarPath(dir, p, file)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	st := loadState(dir)
	delete(st.Files, strings.TrimSuffix(file, disabledEx))
	return saveState(dir, st)
}

func (m *Manager) jarPath(dir string, p Platform, file string) (string, error) {
	lower := strings.ToLower(file)
	if file == "" || filepath.Base(file) != file || strings.ContainsAny(file, `/\`) ||
		!(strings.HasSuffix(lower, ".jar") || strings.HasSuffix(lower, ".jar"+disabledEx)) {
		return "", ErrNotFound
	}
	path, err := files.ResolveSafe(dir, filepath.Join(p.Dir, file))
	if err != nil {
		return "", ErrNotFound
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return path, nil
}

func InstalledProjects(dir string) map[string]bool {
	out := map[string]bool{}
	for _, meta := range loadState(dir).Files {
		if meta.ProjectID != "" {
			out[meta.ProjectID] = true
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

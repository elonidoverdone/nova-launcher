// Package modrinth searches and installs Fabric mods from Modrinth.
package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"nova/launcher/internal/dl"
)

var (
	API = "https://api.modrinth.com/v2"
	// CDN is the only host mod files are accepted from.
	CDN = "https://cdn.modrinth.com/"
)

const FabricAPIProject = "P7dR8mSH"

type Hit struct {
	ProjectID   string `json:"project_id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	Downloads   int64  `json:"downloads"`
	Author      string `json:"author"`
	Installed   bool   `json:"installed"`
}

type versionFile struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Hashes   struct {
		SHA1 string `json:"sha1"`
	} `json:"hashes"`
}

type version struct {
	ID           string        `json:"id"`
	VersionNum   string        `json:"version_number"`
	Files        []versionFile `json:"files"`
	Dependencies []struct {
		ProjectID string `json:"project_id"`
		Type      string `json:"dependency_type"`
	} `json:"dependencies"`
}

// Index remembers which jar belongs to which Modrinth project (mods/.nova-index.json).
type Index struct {
	mu    sync.Mutex
	path  string
	Files map[string]string `json:"files"` // project id -> filename
}

func OpenIndex(modsDir string) *Index {
	ix := &Index{path: filepath.Join(modsDir, ".nova-index.json"), Files: map[string]string{}}
	if b, err := os.ReadFile(ix.path); err == nil {
		_ = json.Unmarshal(b, ix)
		if ix.Files == nil {
			ix.Files = map[string]string{}
		}
	}
	// forget entries whose jar was deleted by hand
	for id, f := range ix.Files {
		if _, err := os.Stat(filepath.Join(modsDir, f)); err != nil {
			delete(ix.Files, id)
		}
	}
	return ix
}

func (ix *Index) save() error {
	b, _ := json.MarshalIndent(ix, "", "  ")
	_ = os.MkdirAll(filepath.Dir(ix.path), 0o755)
	return os.WriteFile(ix.path, b, 0o644)
}

func (ix *Index) Has(id string) bool {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	_, ok := ix.Files[id]
	return ok
}

func Search(ctx context.Context, query, game string, ix *Index) ([]Hit, error) {
	facets := fmt.Sprintf(`[["versions:%s"],["categories:fabric"],["project_type:mod"]]`, game)
	u := fmt.Sprintf("%s/search?limit=24&index=relevance&query=%s&facets=%s", API, url.QueryEscape(query), url.QueryEscape(facets))
	body, err := dl.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var res struct {
		Hits []Hit `json:"hits"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	for i := range res.Hits {
		res.Hits[i].Installed = ix.Has(res.Hits[i].ProjectID)
	}
	return res.Hits, nil
}

func latestVersion(ctx context.Context, project, game string) (*version, error) {
	u := fmt.Sprintf("%s/project/%s/version?loaders=%s&game_versions=%s", API,
		url.PathEscape(project), url.QueryEscape(`["fabric"]`), url.QueryEscape(`["`+game+`"]`))
	body, err := dl.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var vs []version
	if err := json.Unmarshal(body, &vs); err != nil {
		return nil, err
	}
	if len(vs) == 0 {
		return nil, fmt.Errorf("у мода нет версии под Fabric %s", game)
	}
	return &vs[0], nil
}

// Install installs a project and its required dependencies into modsDir.
func Install(ctx context.Context, project, game, modsDir string, ix *Index) error {
	return install(ctx, project, game, modsDir, ix, map[string]bool{})
}

func install(ctx context.Context, project, game, modsDir string, ix *Index, visiting map[string]bool) error {
	if visiting[project] {
		return nil
	}
	visiting[project] = true
	v, err := latestVersion(ctx, project, game)
	if err != nil {
		return err
	}
	var f *versionFile
	for i := range v.Files {
		if v.Files[i].Primary || f == nil {
			f = &v.Files[i]
		}
	}
	if f == nil {
		return fmt.Errorf("в версии нет файлов")
	}
	if !strings.HasPrefix(f.URL, CDN) {
		return fmt.Errorf("неожиданный адрес файла: %s", f.URL)
	}
	name := filepath.Base(f.Filename)
	if !strings.HasSuffix(strings.ToLower(name), ".jar") {
		return fmt.Errorf("файл мода не .jar: %s", name)
	}
	if err := dl.One(ctx, dl.Job{URL: f.URL, SHA1: f.Hashes.SHA1, Path: filepath.Join(modsDir, name)}); err != nil {
		return err
	}
	ix.mu.Lock()
	if old, ok := ix.Files[project]; ok && old != name {
		_ = os.Remove(filepath.Join(modsDir, old)) // replaced by a newer version
	}
	ix.Files[project] = name
	err = ix.save()
	ix.mu.Unlock()
	if err != nil {
		return err
	}
	for _, d := range v.Dependencies {
		if d.Type == "required" && d.ProjectID != "" && !ix.Has(d.ProjectID) {
			if err := install(ctx, d.ProjectID, game, modsDir, ix, visiting); err != nil {
				return fmt.Errorf("зависимость: %w", err)
			}
		}
	}
	return nil
}

// Remove deletes the jar of a project installed through the launcher.
func Remove(project, modsDir string, ix *Index) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	name, ok := ix.Files[project]
	if !ok {
		return nil
	}
	if err := os.Remove(filepath.Join(modsDir, name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(ix.Files, project)
	return ix.save()
}

// ListJars returns all jar files in the mods folder.
func ListJars(modsDir string) []string {
	entries, _ := os.ReadDir(modsDir)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			out = append(out, e.Name())
		}
	}
	return out
}

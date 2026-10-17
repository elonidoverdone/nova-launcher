package modrinth

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"nova/launcher/internal/dl"
)

// FabulouslyOptimized is the Modrinth project of the Fabulously Optimized modpack.
const FabulouslyOptimized = "1KVo5zza"

// Hosts a modpack may download from (per the Modrinth .mrpack spec).
var allowedPackHosts = map[string]bool{
	"cdn.modrinth.com": true, "github.com": true, "raw.githubusercontent.com": true, "gitlab.com": true,
}

type packIndex struct {
	FormatVersion int    `json:"formatVersion"`
	Game          string `json:"game"`
	VersionID     string `json:"versionId"`
	Name          string `json:"name"`
	Files         []struct {
		Path   string `json:"path"`
		Hashes struct {
			SHA1 string `json:"sha1"`
		} `json:"hashes"`
		Env *struct {
			Client string `json:"client"`
		} `json:"env"`
		Downloads []string `json:"downloads"`
		FileSize  int64    `json:"fileSize"`
	} `json:"files"`
	Dependencies map[string]string `json:"dependencies"`
}

// PackState is remembered in <gameDir>/.nova-pack.json so updates can remove old files.
type PackState struct {
	Project   string   `json:"project"`
	VersionID string   `json:"versionId"`
	Name      string   `json:"name"`
	Loader    string   `json:"fabricLoader"`
	Files     []string `json:"files"`
}

func packStatePath(gameDir string) string { return filepath.Join(gameDir, ".nova-pack.json") }

func ReadPackState(gameDir string) *PackState {
	b, err := os.ReadFile(packStatePath(gameDir))
	if err != nil {
		return nil
	}
	var s PackState
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

// safeJoin joins a pack-relative path to dir, refusing anything that escapes it.
func safeJoin(dir, rel string) (string, error) {
	rel = filepath.FromSlash(strings.TrimSpace(rel))
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, ":") {
		return "", fmt.Errorf("недопустимый путь в сборке: %q", rel)
	}
	p := filepath.Join(dir, rel)
	r, err := filepath.Rel(dir, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("путь выходит за папку игры: %q", rel)
	}
	return p, nil
}

// testHosts lets tests serve pack files from a local server.
var testHosts = map[string]bool{}

func packURLAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "https" && allowedPackHosts[u.Host]) || testHosts[u.Host]
}

// InstallPack installs or updates a Modrinth modpack (.mrpack) into gameDir.
// Files already present with the right hash are kept; files from the previous version of
// the same pack that are no longer listed are removed; config overrides never overwrite
// files the player already has. Returns the Fabric Loader version the pack asks for.
func InstallPack(ctx context.Context, project, game, gameDir, cacheDir string, progress func(stage string, done, total int)) (*PackState, error) {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	progress("Сборка: поиск версии", 0, 0)
	u := fmt.Sprintf("%s/project/%s/version?loaders=%s&game_versions=%s", API,
		url.PathEscape(project), url.QueryEscape(`["fabric"]`), url.QueryEscape(`["`+game+`"]`))
	body, err := dl.Get(ctx, u)
	if err != nil {
		if old := ReadPackState(gameDir); old != nil && old.Project == project {
			return old, nil // offline: keep what is installed
		}
		return nil, err
	}
	var vs []struct {
		ID    string        `json:"id"`
		Files []versionFile `json:"files"`
	}
	if err := json.Unmarshal(body, &vs); err != nil {
		return nil, err
	}
	if len(vs) == 0 {
		return nil, fmt.Errorf("сборка ещё не вышла под %s", game)
	}
	var pf *versionFile
	for i := range vs[0].Files {
		f := &vs[0].Files[i]
		if strings.HasSuffix(strings.ToLower(f.Filename), ".mrpack") && (pf == nil || f.Primary) {
			pf = f
		}
	}
	if pf == nil || !strings.HasPrefix(pf.URL, CDN) {
		return nil, fmt.Errorf("у версии сборки нет файла .mrpack")
	}
	old := ReadPackState(gameDir)
	if old != nil && old.Project == project && old.VersionID == vs[0].ID && allPresent(gameDir, old.Files) {
		return old, nil
	}

	progress("Сборка: загрузка", 0, 0)
	packFile := filepath.Join(cacheDir, filepath.Base(pf.Filename))
	if err := dl.One(ctx, dl.Job{URL: pf.URL, SHA1: pf.Hashes.SHA1, Path: packFile}); err != nil {
		return nil, err
	}
	zr, err := zip.OpenReader(packFile)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var idx packIndex
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = json.NewDecoder(rc).Decode(&idx)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("modrinth.index.json: %w", err)
			}
		}
	}
	if idx.FormatVersion != 1 || idx.Game != "minecraft" {
		return nil, fmt.Errorf("неизвестный формат сборки")
	}
	if want := idx.Dependencies["minecraft"]; want != "" && want != game {
		return nil, fmt.Errorf("сборка сделана под %s, а выбрана %s", want, game)
	}

	var jobs []dl.Job
	var files []string
	for _, f := range idx.Files {
		if f.Env != nil && f.Env.Client == "unsupported" {
			continue
		}
		target, err := safeJoin(gameDir, f.Path)
		if err != nil {
			return nil, err
		}
		var src string
		for _, d := range f.Downloads {
			if packURLAllowed(d) {
				src = d
				break
			}
		}
		if src == "" {
			return nil, fmt.Errorf("файл %s скачивается с неразрешённого адреса", f.Path)
		}
		jobs = append(jobs, dl.Job{URL: src, SHA1: f.Hashes.SHA1, Size: f.FileSize, Path: target})
		files = append(files, filepath.ToSlash(filepath.Clean(filepath.FromSlash(f.Path))))
	}
	if err := dl.All(ctx, jobs, 8, func(d, t int) { progress("Сборка: моды", d, t) }); err != nil {
		return nil, err
	}

	// overrides: only add files the player doesn't have yet
	for _, f := range zr.File {
		var rel string
		switch {
		case strings.HasPrefix(f.Name, "overrides/"):
			rel = strings.TrimPrefix(f.Name, "overrides/")
		case strings.HasPrefix(f.Name, "client-overrides/"):
			rel = strings.TrimPrefix(f.Name, "client-overrides/")
		default:
			continue
		}
		if rel == "" || f.FileInfo().IsDir() {
			continue
		}
		target, err := safeJoin(gameDir, rel)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := extract(f, target); err != nil {
			return nil, err
		}
	}

	// remove jars that belonged to the previous version of this pack
	if old != nil && old.Project == project {
		keep := map[string]bool{}
		for _, f := range files {
			keep[f] = true
		}
		for _, f := range old.Files {
			if !keep[f] {
				if p, err := safeJoin(gameDir, f); err == nil {
					_ = os.Remove(p)
				}
			}
		}
	}

	st := &PackState{Project: project, VersionID: vs[0].ID, Name: idx.Name + " " + idx.VersionID, Loader: idx.Dependencies["fabric-loader"], Files: files}
	b, _ := json.MarshalIndent(st, "", "  ")
	if err := os.WriteFile(packStatePath(gameDir), b, 0o644); err != nil {
		return nil, err
	}
	return st, nil
}

func allPresent(gameDir string, files []string) bool {
	for _, f := range files {
		p, err := safeJoin(gameDir, f)
		if err != nil {
			return false
		}
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

func extract(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(rc, 256<<20))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// RemovePack deletes the files a pack installed (used when the player turns it off).
func RemovePack(gameDir string) error {
	st := ReadPackState(gameDir)
	if st == nil {
		return nil
	}
	for _, f := range st.Files {
		if p, err := safeJoin(gameDir, f); err == nil {
			_ = os.Remove(p)
		}
	}
	return os.Remove(packStatePath(gameDir))
}

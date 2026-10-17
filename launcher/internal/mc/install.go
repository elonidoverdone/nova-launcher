package mc

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"nova/launcher/internal/dl"
)

// Paths: Root holds shared files (versions, libraries, assets, runtime); GameDir holds saves, mods, config.
type Paths struct {
	Root    string
	GameDir string
}

func (p Paths) Versions() string  { return filepath.Join(p.Root, "versions") }
func (p Paths) Libraries() string { return filepath.Join(p.Root, "libraries") }
func (p Paths) Assets() string    { return filepath.Join(p.Root, "assets") }
func (p Paths) Runtime() string   { return filepath.Join(p.Root, "runtime") }
func (p Paths) Mods() string      { return filepath.Join(p.GameDir, "mods") }
func (p Paths) Config() string    { return filepath.Join(p.GameDir, "config") }

// Progress reports install stages to the UI.
type Progress func(stage string, done, total int)

type Installed struct {
	Version   *Version // merged vanilla + Fabric
	VanillaID string
	JavaPath  string
}

// getJSON fetches url and caches it at cachePath; when offline the cached copy is used.
func getJSON(ctx context.Context, rawURL, cachePath, sha string, v any) error {
	body, err := dl.Get(ctx, rawURL)
	if err == nil && sha != "" {
		sum := sha1.Sum(body)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), sha) {
			err = fmt.Errorf("checksum mismatch for %s", rawURL)
		}
	}
	if err == nil {
		if jerr := json.Unmarshal(body, v); jerr != nil {
			return fmt.Errorf("%s: %w", rawURL, jerr)
		}
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		_ = os.WriteFile(cachePath, body, 0o644)
		return nil
	}
	cached, cerr := os.ReadFile(cachePath)
	if cerr != nil {
		return err
	}
	return json.Unmarshal(cached, v)
}

func LoadManifest(ctx context.Context, p Paths) (*Manifest, error) {
	var m Manifest
	err := getJSON(ctx, ManifestURL, filepath.Join(p.Versions(), "version_manifest_v2.json"), "", &m)
	return &m, err
}

// FabricLoaderVersion returns the newest stable Fabric Loader for a game version.
func FabricLoaderVersion(ctx context.Context, p Paths, game string) (string, error) {
	var list []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		} `json:"loader"`
	}
	u := FabricMetaURL + "/versions/loader/" + url.PathEscape(game)
	if err := getJSON(ctx, u, filepath.Join(p.Versions(), "fabric-loaders-"+game+".json"), "", &list); err != nil {
		return "", err
	}
	for _, e := range list {
		if e.Loader.Stable {
			return e.Loader.Version, nil
		}
	}
	if len(list) > 0 {
		return list[0].Loader.Version, nil
	}
	return "", fmt.Errorf("Fabric пока не поддерживает %s", game)
}

// Install downloads everything needed to run game version `game` with Fabric.
// loader pins a Fabric Loader version ("" = newest stable).
func Install(ctx context.Context, p Paths, game, loader string, env Env, progress Progress) (*Installed, error) {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	progress("Список версий", 0, 0)
	man, err := LoadManifest(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список версий: %w", err)
	}
	var entry *ManifestEntry
	for i := range man.Versions {
		if man.Versions[i].ID == game {
			entry = &man.Versions[i]
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("версия %s не найдена", game)
	}

	var vanilla Version
	vdir := filepath.Join(p.Versions(), game)
	if err := getJSON(ctx, entry.URL, filepath.Join(vdir, game+".json"), entry.SHA1, &vanilla); err != nil {
		return nil, err
	}

	progress("Fabric", 0, 0)
	if loader == "" {
		if loader, err = FabricLoaderVersion(ctx, p, game); err != nil {
			return nil, err
		}
	}
	var fabric Version
	fid := "fabric-loader-" + loader + "-" + game
	fu := fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", FabricMetaURL, url.PathEscape(game), url.PathEscape(loader))
	if err := getJSON(ctx, fu, filepath.Join(p.Versions(), fid, fid+".json"), "", &fabric); err != nil {
		return nil, err
	}
	merged := Merge(&vanilla, &fabric)

	// game files
	jobs := []dl.Job{{
		URL: vanilla.Downloads.Client.URL, SHA1: vanilla.Downloads.Client.SHA1,
		Path: filepath.Join(vdir, game+".jar"),
	}}
	libJobs, err := LibraryJobs(p, merged, env)
	if err != nil {
		return nil, err
	}
	jobs = append(jobs, libJobs...)
	if lf := vanilla.Logging.Client.File; lf.URL != "" {
		jobs = append(jobs, dl.Job{URL: lf.URL, SHA1: lf.SHA1, Path: filepath.Join(p.Assets(), "log_configs", lf.ID)})
	}
	progress("Библиотеки", 0, len(jobs))
	if err := dl.All(ctx, jobs, 8, func(d, t int) { progress("Библиотеки", d, t) }); err != nil {
		return nil, err
	}
	if err := extractNatives(p, merged, env); err != nil {
		return nil, err
	}

	// assets
	var index struct {
		Objects map[string]struct {
			Hash string `json:"hash"`
			Size int64  `json:"size"`
		} `json:"objects"`
	}
	ai := vanilla.AssetIndex
	if err := getJSON(ctx, ai.URL, filepath.Join(p.Assets(), "indexes", ai.ID+".json"), ai.SHA1, &index); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var assetJobs []dl.Job
	for _, o := range index.Objects {
		if seen[o.Hash] || len(o.Hash) < 2 {
			continue
		}
		seen[o.Hash] = true
		assetJobs = append(assetJobs, dl.Job{
			URL:  ResourcesURL + o.Hash[:2] + "/" + o.Hash,
			Path: filepath.Join(p.Assets(), "objects", o.Hash[:2], o.Hash),
			Size: o.Size, // size check keeps relaunches fast; files are content-addressed
		})
	}
	progress("Ресурсы", 0, len(assetJobs))
	if err := dl.All(ctx, assetJobs, 16, func(d, t int) { progress("Ресурсы", d, t) }); err != nil {
		return nil, err
	}

	progress("Java", 0, 0)
	java, err := InstallJava(ctx, p, vanilla.JavaVersion.Component, env, progress)
	if err != nil {
		return nil, err
	}
	return &Installed{Version: merged, VanillaID: game, JavaPath: java}, nil
}

// LibraryJobs lists library jars (and legacy natives) needed on this machine.
func LibraryJobs(p Paths, v *Version, env Env) ([]dl.Job, error) {
	var jobs []dl.Job
	for _, l := range v.Libraries {
		if !Allowed(l.Rules, env) {
			continue
		}
		a, err := LibraryArtifact(l)
		if err != nil {
			return nil, err
		}
		if a != nil {
			jobs = append(jobs, dl.Job{URL: a.URL, SHA1: a.SHA1, Size: a.Size, Path: filepath.Join(p.Libraries(), filepath.FromSlash(a.Path))})
		}
		if n := NativeArtifact(l, env); n != nil {
			jobs = append(jobs, dl.Job{URL: n.URL, SHA1: n.SHA1, Size: n.Size, Path: filepath.Join(p.Libraries(), filepath.FromSlash(n.Path))})
		}
	}
	return jobs, nil
}

// NativesDir is where legacy native libraries are extracted.
func NativesDir(p Paths, id string) string { return filepath.Join(p.Versions(), id, "natives") }

func extractNatives(p Paths, v *Version, env Env) error {
	dir := NativesDir(p, v.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, l := range v.Libraries {
		if !Allowed(l.Rules, env) {
			continue
		}
		n := NativeArtifact(l, env)
		if n == nil {
			continue
		}
		if err := unzipNatives(filepath.Join(p.Libraries(), filepath.FromSlash(n.Path)), dir); err != nil {
			return err
		}
	}
	return nil
}

func unzipNatives(jar, dir string) error {
	r, err := zip.OpenReader(jar)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() || strings.HasPrefix(f.Name, "META-INF") {
			continue
		}
		name := filepath.Base(f.Name) // flatten and never escape dir
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

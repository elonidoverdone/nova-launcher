package mc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nova/launcher/internal/dl"
)

// Mojang's Java runtime index (the same one the official launcher uses).
var JavaRuntimesURL = "https://launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

func javaPlatform(env Env) string {
	switch env.OS {
	case "windows":
		switch env.Arch {
		case "x86":
			return "windows-x86"
		case "arm64":
			return "windows-arm64"
		}
		return "windows-x64"
	case "osx":
		if env.Arch == "arm64" {
			return "mac-os-arm64"
		}
		return "mac-os"
	}
	return "linux"
}

type runtimeFile struct {
	Type       string `json:"type"`
	Executable bool   `json:"executable"`
	Downloads  struct {
		Raw Download `json:"raw"`
	} `json:"downloads"`
}

// JavaExecutable returns the java binary path inside an installed runtime.
func JavaExecutable(p Paths, component string, env Env) string {
	if env.OS == "windows" {
		return filepath.Join(p.Runtime(), component, "bin", "javaw.exe")
	}
	return filepath.Join(p.Runtime(), component, "bin", "java")
}

// InstallJava installs Mojang's Java runtime for the given component (e.g. the one 26.3 asks for).
func InstallJava(ctx context.Context, p Paths, component string, env Env, progress Progress) (string, error) {
	if component == "" {
		return "", fmt.Errorf("версия игры не указала нужную Java")
	}
	var all map[string]map[string][]struct {
		Manifest Download `json:"manifest"`
		Version  struct {
			Name string `json:"name"`
		} `json:"version"`
	}
	if err := getJSON(ctx, JavaRuntimesURL, filepath.Join(p.Runtime(), "all.json"), "", &all); err != nil {
		return "", err
	}
	plat := javaPlatform(env)
	entries := all[plat][component]
	if len(entries) == 0 {
		return "", fmt.Errorf("Mojang не публикует %s для %s — укажите путь к Java 25 в настройках", component, plat)
	}
	var manifest struct {
		Files map[string]runtimeFile `json:"files"`
	}
	dir := filepath.Join(p.Runtime(), component)
	if err := getJSON(ctx, entries[0].Manifest.URL, filepath.Join(p.Runtime(), component+".manifest.json"), entries[0].Manifest.SHA1, &manifest); err != nil {
		return "", err
	}
	var jobs []dl.Job
	var execs []string
	for rel, f := range manifest.Files {
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if !insideDir(dir, target) {
			continue
		}
		switch f.Type {
		case "directory":
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case "file":
			jobs = append(jobs, dl.Job{URL: f.Downloads.Raw.URL, SHA1: f.Downloads.Raw.SHA1, Path: target})
			if f.Executable {
				execs = append(execs, target)
			}
		}
	}
	if err := dl.All(ctx, jobs, 8, func(d, t int) { progress("Java", d, t) }); err != nil {
		return "", err
	}
	for _, e := range execs {
		_ = os.Chmod(e, 0o755)
	}
	java := JavaExecutable(p, component, env)
	if _, err := os.Stat(java); err != nil {
		return "", fmt.Errorf("Java установлена, но %s не найден", java)
	}
	return java, nil
}

func insideDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Package mc installs and launches Minecraft (vanilla + Fabric).
package mc

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
)

var (
	ManifestURL   = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	ResourcesURL  = "https://resources.download.minecraft.net/"
	FabricMetaURL = "https://meta.fabricmc.net/v2"
	LibrariesURL  = "https://libraries.minecraft.net/"
)

type Manifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []ManifestEntry `json:"versions"`
}

type ManifestEntry struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
}

type Download struct {
	Path string `json:"path,omitempty"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

type Rule struct {
	Action   string          `json:"action"`
	OS       *RuleOS         `json:"os,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

type RuleOS struct {
	Name string `json:"name,omitempty"`
	Arch string `json:"arch,omitempty"`
}

type Library struct {
	Name      string `json:"name"`
	URL       string `json:"url,omitempty"` // Fabric-style maven base
	SHA1      string `json:"sha1,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Downloads struct {
		Artifact    *Download           `json:"artifact,omitempty"`
		Classifiers map[string]Download `json:"classifiers,omitempty"`
	} `json:"downloads"`
	Natives map[string]string `json:"natives,omitempty"`
	Rules   []Rule            `json:"rules,omitempty"`
}

type Version struct {
	ID                 string     `json:"id"`
	InheritsFrom       string     `json:"inheritsFrom,omitempty"`
	Type               string     `json:"type"`
	MainClass          string     `json:"mainClass"`
	Assets             string     `json:"assets"`
	MinecraftArguments string     `json:"minecraftArguments,omitempty"`
	Libraries          []Library  `json:"libraries"`
	Arguments          *Arguments `json:"arguments,omitempty"`
	AssetIndex         struct {
		ID   string `json:"id"`
		URL  string `json:"url"`
		SHA1 string `json:"sha1"`
		Size int64  `json:"size"`
	} `json:"assetIndex"`
	Downloads struct {
		Client Download `json:"client"`
	} `json:"downloads"`
	JavaVersion struct {
		Component    string `json:"component"`
		MajorVersion int    `json:"majorVersion"`
	} `json:"javaVersion"`
	Logging struct {
		Client struct {
			Argument string `json:"argument"`
			File     struct {
				ID   string `json:"id"`
				URL  string `json:"url"`
				SHA1 string `json:"sha1"`
				Size int64  `json:"size"`
			} `json:"file"`
		} `json:"client"`
	} `json:"logging"`
}

type Arguments struct {
	Game []Arg `json:"game"`
	JVM  []Arg `json:"jvm"`
}

// Arg is either a plain string or {"rules": [...], "value": "x" | ["x","y"]}.
type Arg struct {
	Values []string
	Rules  []Rule
}

func (a *Arg) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		a.Values = []string{s}
		return nil
	}
	var obj struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	a.Rules = obj.Rules
	if err := json.Unmarshal(obj.Value, &s); err == nil {
		a.Values = []string{s}
		return nil
	}
	return json.Unmarshal(obj.Value, &a.Values)
}

func (a Arg) MarshalJSON() ([]byte, error) {
	if len(a.Rules) == 0 && len(a.Values) == 1 {
		return json.Marshal(a.Values[0])
	}
	return json.Marshal(map[string]any{"rules": a.Rules, "value": a.Values})
}

// Env describes the machine rules are evaluated against.
type Env struct {
	OS       string // windows, osx, linux
	Arch     string // x86, x86_64, arm64
	Features map[string]bool
}

func HostEnv() Env {
	osName := runtime.GOOS
	switch osName {
	case "darwin":
		osName = "osx"
	}
	arch := map[string]string{"amd64": "x86_64", "386": "x86", "arm64": "arm64"}[runtime.GOARCH]
	return Env{OS: osName, Arch: arch, Features: map[string]bool{}}
}

// Allowed evaluates Mojang rule lists: the last matching rule wins, default deny when rules exist.
func Allowed(rules []Rule, env Env) bool {
	if len(rules) == 0 {
		return true
	}
	allow := false
	for _, r := range rules {
		if !ruleMatches(r, env) {
			continue
		}
		allow = r.Action == "allow"
	}
	return allow
}

func ruleMatches(r Rule, env Env) bool {
	if r.OS != nil {
		if r.OS.Name != "" && r.OS.Name != env.OS {
			return false
		}
		if r.OS.Arch != "" && r.OS.Arch != env.Arch {
			return false
		}
	}
	for k, v := range r.Features {
		if env.Features[k] != v {
			return false
		}
	}
	return true
}

// MavenPath turns "group:artifact:version[:classifier][@ext]" into a repository path.
func MavenPath(name string) (string, error) {
	ext := "jar"
	if i := strings.LastIndex(name, "@"); i >= 0 {
		ext = name[i+1:]
		name = name[:i]
	}
	p := strings.Split(name, ":")
	if len(p) < 3 {
		return "", fmt.Errorf("bad maven coordinate %q", name)
	}
	group := strings.ReplaceAll(p[0], ".", "/")
	file := p[1] + "-" + p[2]
	if len(p) > 3 {
		file += "-" + p[3]
	}
	return group + "/" + p[1] + "/" + p[2] + "/" + file + "." + ext, nil
}

// libKey identifies a library regardless of version, used for de-duplication.
func libKey(name string) string {
	p := strings.Split(name, ":")
	if len(p) < 3 {
		return name
	}
	key := p[0] + ":" + p[1]
	if len(p) > 3 {
		key += ":" + p[3]
	}
	return key
}

// Merge applies a child profile (e.g. Fabric) on top of its parent (vanilla).
// Child libraries take precedence over parent libraries with the same group:artifact.
func Merge(parent, child *Version) *Version {
	out := *parent
	out.ID = child.ID
	out.InheritsFrom = ""
	if child.MainClass != "" {
		out.MainClass = child.MainClass
	}
	if child.Type != "" {
		out.Type = child.Type
	}
	seen := map[string]bool{}
	out.Libraries = nil
	for _, l := range child.Libraries {
		seen[libKey(l.Name)] = true
		out.Libraries = append(out.Libraries, l)
	}
	for _, l := range parent.Libraries {
		if seen[libKey(l.Name)] {
			continue
		}
		out.Libraries = append(out.Libraries, l)
	}
	if child.Arguments != nil {
		args := Arguments{}
		if parent.Arguments != nil {
			args = *parent.Arguments
		}
		args.Game = append(append([]Arg{}, args.Game...), child.Arguments.Game...)
		args.JVM = append(append([]Arg{}, args.JVM...), child.Arguments.JVM...)
		out.Arguments = &args
	}
	return &out
}

// LibraryArtifact returns the download for a library's main jar on this machine, or nil if none.
func LibraryArtifact(l Library) (*Download, error) {
	if l.Downloads.Artifact != nil && l.Downloads.Artifact.URL != "" {
		d := *l.Downloads.Artifact
		if d.Path == "" {
			p, err := MavenPath(l.Name)
			if err != nil {
				return nil, err
			}
			d.Path = p
		}
		return &d, nil
	}
	if len(l.Natives) > 0 && l.URL == "" {
		// legacy natives-only entry: handled by NativeArtifact
		return nil, nil
	}
	p, err := MavenPath(l.Name)
	if err != nil {
		return nil, err
	}
	base := l.URL
	if base == "" {
		base = LibrariesURL
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return &Download{Path: p, URL: base + p, SHA1: l.SHA1, Size: l.Size}, nil
}

// NativeArtifact returns the legacy natives classifier download for this OS (old versions only).
func NativeArtifact(l Library, env Env) *Download {
	if len(l.Natives) == 0 {
		return nil
	}
	cls, ok := l.Natives[env.OS]
	if !ok {
		return nil
	}
	bits := "64"
	if env.Arch == "x86" {
		bits = "32"
	}
	cls = strings.ReplaceAll(cls, "${arch}", bits)
	d, ok := l.Downloads.Classifiers[cls]
	if !ok {
		return nil
	}
	return &d
}

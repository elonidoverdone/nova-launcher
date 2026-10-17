package mc

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Account struct {
	Name        string
	UUID        string // without dashes is fine
	AccessToken string
	XUID        string
	UserType    string // "msa" or "legacy" (offline)
}

type LaunchOptions struct {
	Paths     Paths
	Installed *Installed
	MemoryMB  int
	ExtraJVM  []string
	Account   Account
	Env       Env
	Launcher  string
	LVersion  string
}

// BuildArgs returns the full java argument list (without the java binary itself).
func BuildArgs(o LaunchOptions) ([]string, error) {
	v := o.Installed.Version
	if v.MainClass == "" {
		return nil, fmt.Errorf("в профиле версии нет mainClass")
	}
	sep := ":"
	if o.Env.OS == "windows" {
		sep = ";"
	}

	// classpath: libraries (child first, already de-duplicated by Merge) + client jar
	var cp []string
	seen := map[string]bool{}
	for _, l := range v.Libraries {
		if !Allowed(l.Rules, o.Env) {
			continue
		}
		a, err := LibraryArtifact(l)
		if err != nil {
			return nil, err
		}
		if a == nil {
			continue
		}
		path := filepath.Join(o.Paths.Libraries(), filepath.FromSlash(a.Path))
		if !seen[path] {
			seen[path] = true
			cp = append(cp, path)
		}
	}
	cp = append(cp, filepath.Join(o.Paths.Versions(), o.Installed.VanillaID, o.Installed.VanillaID+".jar"))

	acc := o.Account
	if acc.UserType == "" {
		acc.UserType = "msa"
	}
	if acc.AccessToken == "" {
		acc.AccessToken = "0"
	}
	if acc.XUID == "" {
		acc.XUID = "0"
	}
	vars := map[string]string{
		"auth_player_name":    acc.Name,
		"version_name":        v.ID,
		"game_directory":      o.Paths.GameDir,
		"assets_root":         o.Paths.Assets(),
		"game_assets":         o.Paths.Assets(),
		"assets_index_name":   v.AssetIndex.ID,
		"auth_uuid":           acc.UUID,
		"auth_access_token":   acc.AccessToken,
		"auth_session":        acc.AccessToken,
		"clientid":            "0",
		"auth_xuid":           acc.XUID,
		"user_type":           acc.UserType,
		"user_properties":     "{}",
		"version_type":        "release",
		"natives_directory":   NativesDir(o.Paths, v.ID),
		"launcher_name":       o.Launcher,
		"launcher_version":    o.LVersion,
		"classpath":           strings.Join(cp, sep),
		"classpath_separator": sep,
		"library_directory":   o.Paths.Libraries(),
	}
	// replaces ${name}; substituted values are never re-scanned
	sub := func(s string) string {
		var b strings.Builder
		for {
			i := strings.Index(s, "${")
			if i < 0 {
				b.WriteString(s)
				return b.String()
			}
			j := strings.Index(s[i:], "}")
			if j < 0 {
				b.WriteString(s)
				return b.String()
			}
			b.WriteString(s[:i])
			b.WriteString(vars[s[i+2:i+j]])
			s = s[i+j+1:]
		}
	}

	var args []string
	if o.MemoryMB > 0 {
		args = append(args, fmt.Sprintf("-Xmx%dM", o.MemoryMB))
	}
	args = append(args, o.ExtraJVM...)

	if v.Arguments != nil && len(v.Arguments.JVM) > 0 {
		for _, a := range v.Arguments.JVM {
			if !Allowed(a.Rules, o.Env) {
				continue
			}
			for _, s := range a.Values {
				args = append(args, sub(s))
			}
		}
	} else {
		args = append(args, "-Djava.library.path="+vars["natives_directory"], "-cp", vars["classpath"])
	}
	if lc := v.Logging.Client; lc.Argument != "" && lc.File.ID != "" {
		args = append(args, strings.ReplaceAll(lc.Argument, "${path}", filepath.Join(o.Paths.Assets(), "log_configs", lc.File.ID)))
	}

	args = append(args, v.MainClass)

	if v.Arguments != nil && len(v.Arguments.Game) > 0 {
		for _, a := range v.Arguments.Game {
			if !Allowed(a.Rules, o.Env) {
				continue
			}
			for _, s := range a.Values {
				args = append(args, sub(s))
			}
		}
	} else {
		for _, s := range strings.Fields(v.MinecraftArguments) {
			args = append(args, sub(s))
		}
	}
	return args, nil
}

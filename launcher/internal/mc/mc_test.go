package mc

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha(b []byte) string { s := sha1.Sum(b); return hex.EncodeToString(s[:]) }

func TestMavenPath(t *testing.T) {
	cases := map[string]string{
		"net.fabricmc:fabric-loader:0.19.5":     "net/fabricmc/fabric-loader/0.19.5/fabric-loader-0.19.5.jar",
		"org.lwjgl:lwjgl:3.3.3:natives-windows": "org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3-natives-windows.jar",
		"com.example:thing:1.0@zip":             "com/example/thing/1.0/thing-1.0.zip",
	}
	for in, want := range cases {
		got, err := MavenPath(in)
		if err != nil || got != want {
			t.Errorf("MavenPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := MavenPath("bad"); err == nil {
		t.Error("expected error for bad coordinate")
	}
}

func TestRules(t *testing.T) {
	win := Env{OS: "windows", Arch: "x86_64", Features: map[string]bool{}}
	osx := Env{OS: "osx", Arch: "arm64", Features: map[string]bool{}}
	onlyOSX := []Rule{{Action: "allow", OS: &RuleOS{Name: "osx"}}}
	notOSX := []Rule{{Action: "allow"}, {Action: "disallow", OS: &RuleOS{Name: "osx"}}}
	demo := []Rule{{Action: "allow", Features: map[string]bool{"is_demo_user": true}}}
	if Allowed(onlyOSX, win) || !Allowed(onlyOSX, osx) {
		t.Error("os allow rule")
	}
	if !Allowed(notOSX, win) || Allowed(notOSX, osx) {
		t.Error("os disallow rule")
	}
	if Allowed(demo, win) {
		t.Error("feature rule should not match by default")
	}
	if !Allowed(nil, win) {
		t.Error("no rules = allowed")
	}
}

// fakeServers serves a tiny but complete Mojang + Fabric + Java runtime setup.
func fakeServers(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	u := srv.URL

	files := map[string][]byte{
		"/files/client.jar":      []byte("client-jar"),
		"/files/lib-common.jar":  []byte("common"),
		"/files/lib-windows.jar": []byte("windows-only"),
		"/files/log4j.xml":       []byte("<xml/>"),
		"/maven/net/fabricmc/fabric-loader/0.19.5/fabric-loader-0.19.5.jar": []byte("loader"),
		"/maven/org/ow2/asm/asm/9.9/asm-9.9.jar":                            []byte("asm-new"),
		"/res/ab/abcdef":                                                    []byte("sound"),
		"/java/bin/java":                                                    []byte("#!/bin/sh\n"),
	}
	assetIndex, _ := json.Marshal(map[string]any{"objects": map[string]any{
		"minecraft/sounds/hit.ogg": map[string]any{"hash": "abcdef", "size": 5},
	}})
	files["/assets/index.json"] = assetIndex
	files["/assets/index.json"] = assetIndex

	version := map[string]any{
		"id": "26.3", "type": "release", "mainClass": "net.minecraft.client.main.Main", "assets": "29",
		"assetIndex":  map[string]any{"id": "29", "url": u + "/assets/index.json", "sha1": sha(assetIndex)},
		"downloads":   map[string]any{"client": map[string]any{"url": u + "/files/client.jar", "sha1": sha(files["/files/client.jar"])}},
		"javaVersion": map[string]any{"component": "java-runtime-test", "majorVersion": 25},
		"logging": map[string]any{"client": map[string]any{"argument": "-Dlog4j.configurationFile=${path}",
			"file": map[string]any{"id": "client-1.21.2.xml", "url": u + "/files/log4j.xml", "sha1": sha(files["/files/log4j.xml"])}}},
		"libraries": []any{
			map[string]any{"name": "com.mojang:common:1.0", "downloads": map[string]any{"artifact": map[string]any{
				"path": "com/mojang/common/1.0/common-1.0.jar", "url": u + "/files/lib-common.jar", "sha1": sha(files["/files/lib-common.jar"])}}},
			map[string]any{"name": "org.ow2.asm:asm:9.6", "downloads": map[string]any{"artifact": map[string]any{
				"path": "org/ow2/asm/asm/9.6/asm-9.6.jar", "url": u + "/files/missing.jar", "sha1": "00"}}},
			map[string]any{"name": "com.mojang:winonly:1.0", "rules": []any{map[string]any{"action": "allow", "os": map[string]any{"name": "windows"}}},
				"downloads": map[string]any{"artifact": map[string]any{"path": "com/mojang/winonly/1.0/winonly-1.0.jar", "url": u + "/files/lib-windows.jar"}}},
		},
		"arguments": map[string]any{
			"game": []any{"--username", "${auth_player_name}", "--version", "${version_name}", "--gameDir", "${game_directory}",
				"--assetsDir", "${assets_root}", "--assetIndex", "${assets_index_name}", "--uuid", "${auth_uuid}",
				"--accessToken", "${auth_access_token}", "--userType", "${user_type}",
				map[string]any{"rules": []any{map[string]any{"action": "allow", "features": map[string]any{"is_demo_user": true}}}, "value": "--demo"}},
			"jvm": []any{
				map[string]any{"rules": []any{map[string]any{"action": "allow", "os": map[string]any{"name": "osx"}}}, "value": []any{"-XstartOnFirstThread"}},
				"-Djava.library.path=${natives_directory}", "-cp", "${classpath}"},
		},
	}
	vb, _ := json.Marshal(version)
	files["/v/26.3.json"] = vb
	manifest, _ := json.Marshal(map[string]any{"versions": []any{
		map[string]any{"id": "26.3", "type": "release", "url": u + "/v/26.3.json", "sha1": sha(vb)},
	}})
	files["/manifest.json"] = manifest
	files["/fabric/versions/loader/26.3"], _ = json.Marshal([]any{
		map[string]any{"loader": map[string]any{"version": "0.20.0-beta", "stable": false}},
		map[string]any{"loader": map[string]any{"version": "0.19.5", "stable": true}},
	})
	files["/fabric/versions/loader/26.3/0.19.5/profile/json"], _ = json.Marshal(map[string]any{
		"id": "fabric-loader-0.19.5-26.3", "inheritsFrom": "26.3", "type": "release",
		"mainClass": "net.fabricmc.loader.impl.launch.knot.KnotClient",
		"arguments": map[string]any{"game": []any{}, "jvm": []any{"-DFabricMcEmu= net.minecraft.client.main.Main "}},
		"libraries": []any{
			map[string]any{"name": "net.fabricmc:fabric-loader:0.19.5", "url": u + "/maven/"},
			map[string]any{"name": "org.ow2.asm:asm:9.9", "url": u + "/maven/", "sha1": sha(files["/maven/org/ow2/asm/asm/9.9/asm-9.9.jar"])},
		},
	})
	javaManifest, _ := json.Marshal(map[string]any{"files": map[string]any{
		"bin":      map[string]any{"type": "directory"},
		"bin/java": map[string]any{"type": "file", "executable": true, "downloads": map[string]any{"raw": map[string]any{"url": u + "/java/bin/java", "sha1": sha(files["/java/bin/java"])}}},
		"../evil":  map[string]any{"type": "file", "downloads": map[string]any{"raw": map[string]any{"url": u + "/java/bin/java"}}},
	}})
	files["/java/manifest.json"] = javaManifest
	files["/java/all.json"], _ = json.Marshal(map[string]any{
		"linux": map[string]any{"java-runtime-test": []any{map[string]any{"manifest": map[string]any{"url": u + "/java/manifest.json", "sha1": sha(javaManifest)}}}},
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	})
	ManifestURL = u + "/manifest.json"
	FabricMetaURL = u + "/fabric"
	ResourcesURL = u + "/res/"
	JavaRuntimesURL = u + "/java/all.json"
	return srv
}

func TestInstallAndBuildArgs(t *testing.T) {
	srv := fakeServers(t)
	defer srv.Close()
	root := t.TempDir()
	p := Paths{Root: root, GameDir: filepath.Join(root, "game")}
	env := Env{OS: "linux", Arch: "x86_64", Features: map[string]bool{}}

	var stages []string
	inst, err := Install(context.Background(), p, "26.3", "", env, func(s string, d, total int) { stages = append(stages, s) })
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if inst.Version.MainClass != "net.fabricmc.loader.impl.launch.knot.KnotClient" {
		t.Errorf("main class not taken from Fabric: %s", inst.Version.MainClass)
	}
	for _, f := range []string{
		"versions/26.3/26.3.jar",
		"libraries/net/fabricmc/fabric-loader/0.19.5/fabric-loader-0.19.5.jar",
		"libraries/org/ow2/asm/asm/9.9/asm-9.9.jar",
		"libraries/com/mojang/common/1.0/common-1.0.jar",
		"assets/objects/ab/abcdef",
		"assets/indexes/29.json",
		"assets/log_configs/client-1.21.2.xml",
		"runtime/java-runtime-test/bin/java",
	} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	// Fabric's newer asm replaces vanilla's (whose URL 404s), windows-only lib skipped on linux
	for _, f := range []string{"libraries/org/ow2/asm/asm/9.6/asm-9.6.jar", "libraries/com/mojang/winonly/1.0/winonly-1.0.jar", "runtime/evil"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			t.Errorf("%s should not be downloaded", f)
		}
	}
	if inst.JavaPath != filepath.Join(root, "runtime/java-runtime-test/bin/java") {
		t.Errorf("java path %s", inst.JavaPath)
	}

	args, err := BuildArgs(LaunchOptions{Paths: p, Installed: inst, MemoryMB: 4096, Env: env,
		Account: Account{Name: "Steve", UUID: "abc", UserType: "legacy"}, Launcher: "nova", LVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if args[0] != "-Xmx4096M" {
		t.Errorf("first arg %s", args[0])
	}
	for _, want := range []string{"--username Steve", "--accessToken 0", "--userType legacy", "-DFabricMcEmu= net.minecraft.client.main.Main ",
		"-Dlog4j.configurationFile=" + filepath.Join(root, "assets/log_configs/client-1.21.2.xml"), "--assetIndex 29",
		"--version fabric-loader-0.19.5-26.3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q\n%s", want, joined)
		}
	}
	for _, bad := range []string{"--demo", "-XstartOnFirstThread", "${"} {
		if strings.Contains(joined, bad) {
			t.Errorf("args should not contain %q", bad)
		}
	}
	var cp string
	for i, a := range args {
		if a == "-cp" {
			cp = args[i+1]
		}
	}
	parts := strings.Split(cp, ":")
	if !strings.HasSuffix(parts[0], "fabric-loader-0.19.5.jar") || !strings.HasSuffix(parts[len(parts)-1], "26.3.jar") {
		t.Errorf("classpath order wrong: %v", parts)
	}
	if strings.Contains(cp, "asm-9.6") {
		t.Error("duplicate asm on classpath")
	}
	// main class sits between JVM and game args
	mi := -1
	for i, a := range args {
		if a == inst.Version.MainClass {
			mi = i
		}
	}
	if mi < 0 || args[mi+1] != "--username" {
		t.Errorf("main class position wrong: %v", args)
	}

	// relaunch works offline from cache
	srv.Close()
	if _, err := Install(context.Background(), p, "26.3", "", env, nil); err != nil {
		t.Errorf("offline relaunch failed: %v", err)
	}
}

func TestWindowsClasspathSeparator(t *testing.T) {
	v := &Version{ID: "x", MainClass: "M", Libraries: []Library{{Name: "a:b:1", URL: "https://x/"}, {Name: "c:d:1", URL: "https://x/"}}}
	args, err := BuildArgs(LaunchOptions{Paths: Paths{Root: `C:\nova`, GameDir: `C:\nova\game`}, Installed: &Installed{Version: v, VanillaID: "x"},
		Env: Env{OS: "windows", Arch: "x86_64"}, Account: Account{Name: "A"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), ";") {
		t.Errorf("windows classpath should use ';': %v", args)
	}
}

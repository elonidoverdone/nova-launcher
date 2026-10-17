package modrinth

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makePack(t *testing.T, version string, files []map[string]any, overrides map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	idx, _ := json.Marshal(map[string]any{
		"formatVersion": 1, "game": "minecraft", "versionId": version, "name": "Fabulously Optimized",
		"files": files, "dependencies": map[string]string{"minecraft": "26.3", "fabric-loader": "0.19.5"},
	})
	w, _ := zw.Create("modrinth.index.json")
	w.Write(idx)
	for name, body := range overrides {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func h(b []byte) string { s := sha1.Sum(b); return hex.EncodeToString(s[:]) }

func TestInstallPackAndUpdate(t *testing.T) {
	jarA, jarB, jarC := []byte("sodium-1"), []byte("fabric-api"), []byte("sodium-2")
	var srv *httptest.Server
	version := "v1"
	packs := map[string][]byte{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/project/fo/version":
			p := packs[version]
			json.NewEncoder(w).Encode([]any{map[string]any{"id": version, "files": []any{
				map[string]any{"url": srv.URL + "/cdn/fo-" + version + ".mrpack", "filename": "fo-" + version + ".mrpack", "primary": true, "hashes": map[string]string{"sha1": h(p)}},
			}}})
		case strings.HasPrefix(r.URL.Path, "/cdn/fo-"):
			w.Write(packs[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/cdn/fo-"), ".mrpack")])
		case r.URL.Path == "/files/a.jar":
			w.Write(jarA)
		case r.URL.Path == "/files/b.jar":
			w.Write(jarB)
		case r.URL.Path == "/files/c.jar":
			w.Write(jarC)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	API, CDN = srv.URL+"/v2", srv.URL+"/cdn/"
	u, _ := url.Parse(srv.URL)
	testHosts[u.Host] = true
	defer delete(testHosts, u.Host)

	file := func(path, src string, body []byte, client string) map[string]any {
		return map[string]any{"path": path, "hashes": map[string]string{"sha1": h(body)}, "downloads": []string{srv.URL + src},
			"env": map[string]string{"client": client, "server": "required"}, "fileSize": len(body)}
	}
	packs["v1"] = makePack(t, "15.0.0-alpha.5", []map[string]any{
		file("mods/sodium-1.jar", "/files/a.jar", jarA, "required"),
		file("mods/fabric-api.jar", "/files/b.jar", jarB, "required"),
		file("mods/server-only.jar", "/files/a.jar", jarA, "unsupported"),
	}, map[string]string{"overrides/config/sodium.json": "pack-default", "client-overrides/options.txt": "fov:70"})
	packs["v2"] = makePack(t, "15.0.0-beta.1", []map[string]any{
		file("mods/sodium-2.jar", "/files/c.jar", jarC, "required"),
		file("mods/fabric-api.jar", "/files/b.jar", jarB, "required"),
	}, map[string]string{"overrides/config/sodium.json": "new-default"})

	game := t.TempDir()
	cache := t.TempDir()
	st, err := InstallPack(context.Background(), "fo", "26.3", game, cache, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Loader != "0.19.5" || len(st.Files) != 2 {
		t.Errorf("state %+v", st)
	}
	for _, f := range []string{"mods/sodium-1.jar", "mods/fabric-api.jar", "config/sodium.json", "options.txt"} {
		if _, err := os.Stat(filepath.Join(game, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(game, "mods/server-only.jar")); err == nil {
		t.Error("server-only file installed")
	}

	// the player edits a config, then the pack updates
	os.WriteFile(filepath.Join(game, "config/sodium.json"), []byte("player-edit"), 0o644)
	version = "v2"
	st, err = InstallPack(context.Background(), "fo", "26.3", game, cache, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(game, "mods/sodium-1.jar")); err == nil {
		t.Error("old jar from previous pack version should be removed")
	}
	if _, err := os.Stat(filepath.Join(game, "mods/sodium-2.jar")); err != nil {
		t.Error("new jar missing")
	}
	if b, _ := os.ReadFile(filepath.Join(game, "config/sodium.json")); string(b) != "player-edit" {
		t.Errorf("player config overwritten: %s", b)
	}

	if err := RemovePack(game); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(game, "mods/sodium-2.jar")); err == nil {
		t.Error("RemovePack left jars")
	}
}

func TestPackRejectsBadPaths(t *testing.T) {
	for _, bad := range []string{"../evil.jar", "/etc/passwd", "mods/../../x", `C:\x.jar`} {
		if _, err := safeJoin(t.TempDir(), bad); err == nil {
			t.Errorf("safeJoin accepted %q", bad)
		}
	}
	if _, err := safeJoin(t.TempDir(), "mods/ok.jar"); err != nil {
		t.Error(err)
	}
	if packURLAllowed("https://evil.example/x.jar") || packURLAllowed("http://cdn.modrinth.com/x.jar") {
		t.Error("untrusted host accepted")
	}
	if !packURLAllowed("https://cdn.modrinth.com/data/x.jar") {
		t.Error("modrinth CDN rejected")
	}
}

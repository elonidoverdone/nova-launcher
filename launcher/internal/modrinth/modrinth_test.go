package modrinth

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallWithDependencies(t *testing.T) {
	jar := []byte("jar-bytes")
	sum := sha1.Sum(jar)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/project/sodium/version":
			if r.URL.Query().Get("loaders") != `["fabric"]` || r.URL.Query().Get("game_versions") != `["26.3"]` {
				t.Errorf("bad query %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode([]any{map[string]any{
				"files":        []any{map[string]any{"url": srv.URL + "/cdn/sodium.jar", "filename": "sodium-1.jar", "primary": true, "hashes": map[string]string{"sha1": hex.EncodeToString(sum[:])}}},
				"dependencies": []any{map[string]any{"project_id": "P7dR8mSH", "dependency_type": "required"}, map[string]any{"project_id": "opt", "dependency_type": "optional"}},
			}})
		case "/v2/project/P7dR8mSH/version":
			json.NewEncoder(w).Encode([]any{map[string]any{
				"files": []any{map[string]any{"url": srv.URL + "/cdn/fapi.jar", "filename": "fabric-api-1.jar", "primary": true}},
			}})
		case "/v2/project/evil/version":
			json.NewEncoder(w).Encode([]any{map[string]any{
				"files": []any{map[string]any{"url": "https://elsewhere.example/x.jar", "filename": "x.jar"}},
			}})
		case "/cdn/sodium.jar", "/cdn/fapi.jar":
			w.Write(jar)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	API = srv.URL + "/v2"
	CDN = srv.URL + "/cdn/"

	mods := t.TempDir()
	ix := OpenIndex(mods)
	if err := Install(context.Background(), "sodium", "26.3", mods, ix); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"sodium-1.jar", "fabric-api-1.jar"} {
		if _, err := os.Stat(filepath.Join(mods, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if !OpenIndex(mods).Has("P7dR8mSH") {
		t.Error("index not saved")
	}
	if err := Install(context.Background(), "evil", "26.3", mods, ix); err == nil {
		t.Error("files outside the CDN must be rejected")
	}
	if err := Remove("sodium", mods, ix); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mods, "sodium-1.jar")); err == nil {
		t.Error("jar not removed")
	}
}

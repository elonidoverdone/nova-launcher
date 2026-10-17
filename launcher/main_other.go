//go:build !windows

// Development mode for non-Windows machines: serves the UI on http://127.0.0.1:8765
// with the same backend, so the interface can be tested in a normal browser.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

func hideConsole(cmd *exec.Cmd) {}
func totalMemoryMB() int        { return 16384 }

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "listen address")
	data := flag.String("data", filepath.Join(os.TempDir(), "nova-dev"), "data dir")
	flag.Parse()

	exe, _ := os.Executable()
	app := NewApp(*data, filepath.Dir(exe))
	app.OpenPath = func(p string) error { log.Printf("open %s", p); return nil }

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(indexHTML))
	})
	http.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			Args   json.RawMessage `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		res, err := app.Call(req.Method, req.Args)
		payload := map[string]any{"ok": err == nil, "result": res}
		if err != nil {
			payload["error"] = err.Error()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	})
	log.Printf("dev UI on http://%s (data: %s)", *addr, *data)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

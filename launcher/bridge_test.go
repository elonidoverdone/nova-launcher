package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nova/launcher/internal/settings"
)

func TestBridge(t *testing.T) {
	dir := t.TempDir()
	app := NewApp(dir, dir)
	defer app.Shutdown()

	raw, err := os.ReadFile(filepath.Join(dir, "game", "config", "nova-bridge.json"))
	if err != nil {
		t.Fatalf("bridge file: %v", err)
	}
	var info struct {
		Port int    `json:"port"`
		Key  string `json:"key"`
	}
	json.Unmarshal(raw, &info)
	base := "http://127.0.0.1:" + app.bridgePort()

	call := func(method, path, key string, body any) (int, map[string]any) {
		var buf bytes.Buffer
		if body != nil {
			json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, base+path, &buf)
		if key != "" {
			req.Header.Set(bridgeHeader, key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	if code, _ := call("GET", "/v1/accounts", "", nil); code != 403 {
		t.Errorf("no key: got %d", code)
	}
	if code, _ := call("GET", "/v1/accounts", "wrong", nil); code != 403 {
		t.Errorf("wrong key: got %d", code)
	}
	if code, out := call("POST", "/v1/accounts/offline", info.Key, map[string]string{"name": "x"}); code != 400 || out["error"] == nil {
		t.Errorf("bad nick accepted: %d %v", code, out)
	}
	if code, _ := call("POST", "/v1/accounts/offline", info.Key, map[string]string{"name": "Steve"}); code != 200 {
		t.Fatalf("add offline: %d", code)
	}
	call("POST", "/v1/accounts/offline", info.Key, map[string]string{"name": "Alex"})
	_, list := call("GET", "/v1/accounts", info.Key, nil)
	if accs := list["accounts"].([]any); len(accs) != 2 || list["microsoft"] != false {
		t.Errorf("accounts %v", list)
	}

	steve := settings.OfflineUUID("Steve")
	code, sess := call("POST", "/v1/accounts/select", info.Key, map[string]string{"id": steve})
	if code != 200 || sess["name"] != "Steve" || sess["uuid"] != steve || sess["type"] != "offline" {
		t.Errorf("select: %d %v", code, sess)
	}
	if app.store.Get().Selected != steve {
		t.Error("selection not saved")
	}

	if code, out := call("POST", "/v1/login/start", info.Key, nil); code != 400 || !strings.Contains(out["error"].(string), "Client ID") {
		t.Errorf("login without client id: %d %v", code, out)
	}

	call("POST", "/v1/accounts/remove", info.Key, map[string]string{"id": steve})
	if s := app.store.Get(); len(s.Accounts) != 1 || s.Selected == steve {
		t.Errorf("remove: %+v", s)
	}

	// foreign Host header (DNS rebinding) is refused
	req, _ := http.NewRequest("GET", base+"/v1/accounts", nil)
	req.Host = "evil.example:80"
	req.Header.Set(bridgeHeader, info.Key)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 403 {
		t.Errorf("foreign host: %d", resp.StatusCode)
	}

	app.Shutdown()
	if _, err := os.Stat(filepath.Join(dir, "game", "config", "nova-bridge.json")); err == nil {
		t.Error("bridge file left after shutdown")
	}
}

package main

// Local channel between the launcher and the NovaFX mod, used by the in-game account screen.
//
// It listens on 127.0.0.1 only, on a random port, and every request must carry a random key.
// Port and key are written to <game>/config/nova-bridge.json, which only the mod reads.
// Browsers cannot reach it: they can't send the custom header without a CORS preflight,
// which this server never approves, and requests with a foreign Host are rejected.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nova/launcher/internal/settings"
)

const bridgeHeader = "X-Nova-Key"

type bridgeState struct {
	srv   *http.Server
	port  int
	token string
	file  string
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *App) startBridge() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	b := &bridgeState{port: ln.Addr().(*net.TCPAddr).Port, token: newToken()}
	b.srv = &http.Server{Handler: a.bridgeHandler(b.token), ReadHeaderTimeout: 5 * time.Second}
	a.mu.Lock()
	a.bridge = b
	a.mu.Unlock()
	go func() { _ = b.srv.Serve(ln) }()
	return a.writeBridgeFile()
}

// writeBridgeFile (re)writes the port/key file into the current game folder.
func (a *App) writeBridgeFile() error {
	a.mu.Lock()
	b := a.bridge
	a.mu.Unlock()
	if b == nil {
		return errors.New("bridge not running")
	}
	path := filepath.Join(a.paths().Config(), "nova-bridge.json")
	if b.file != "" && b.file != path {
		_ = os.Remove(b.file) // game folder changed
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{"port": b.port, "key": b.token, "pid": os.Getpid()})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	b.file = path
	return nil
}

func (a *App) stopBridge() {
	a.mu.Lock()
	b := a.bridge
	a.bridge = nil
	a.mu.Unlock()
	if b == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = b.srv.Shutdown(ctx)
	if b.file != "" {
		_ = os.Remove(b.file)
	}
}

type bridgeAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (a *App) bridgeHandler(token string) http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any, err error) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(v)
	}
	body := func(r *http.Request) map[string]string {
		m := map[string]string{}
		_ = json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(&m)
		return m
	}

	mux.HandleFunc("GET /v1/accounts", func(w http.ResponseWriter, r *http.Request) {
		s := a.store.Get()
		out := struct {
			Selected  string          `json:"selected"`
			Accounts  []bridgeAccount `json:"accounts"`
			Microsoft bool            `json:"microsoft"` // a Client ID is configured
		}{Accounts: []bridgeAccount{}, Microsoft: s.ClientID != ""}
		if sel, ok := s.SelectedAccount(); ok {
			out.Selected = sel.ID
		}
		for _, acc := range s.Accounts {
			out.Accounts = append(out.Accounts, bridgeAccount{ID: acc.ID, Name: acc.Name, Type: acc.Type})
		}
		reply(w, out, nil)
	})

	// select an account and return a ready-to-use session for it
	mux.HandleFunc("POST /v1/accounts/select", func(w http.ResponseWriter, r *http.Request) {
		id := body(r)["id"]
		s := a.store.Get()
		var acc *settings.Account
		for i := range s.Accounts {
			if s.Accounts[i].ID == id {
				acc = &s.Accounts[i]
			}
		}
		if acc == nil {
			reply(w, nil, errors.New("аккаунт не найден"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		sess, err := a.accountSession(ctx, *acc)
		if err != nil {
			reply(w, nil, err)
			return
		}
		_ = a.store.Update(func(s *settings.Settings) { s.Selected = id })
		reply(w, map[string]string{
			"id": id, "name": sess.Name, "uuid": sess.UUID, "accessToken": sess.AccessToken,
			"xuid": sess.XUID, "type": acc.Type,
		}, nil)
	})

	mux.HandleFunc("POST /v1/accounts/offline", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(body(r)["name"])
		if err := a.addOffline(name); err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, map[string]string{"id": settings.OfflineUUID(name)}, nil)
	})

	mux.HandleFunc("POST /v1/accounts/remove", func(w http.ResponseWriter, r *http.Request) {
		id := body(r)["id"]
		reply(w, map[string]bool{"ok": true}, a.store.Update(func(s *settings.Settings) { s.Remove(id) }))
	})

	mux.HandleFunc("POST /v1/login/start", func(w http.ResponseWriter, r *http.Request) {
		res, err := a.startMSLogin()
		reply(w, res, err)
	})
	mux.HandleFunc("GET /v1/login/status", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		l := a.login
		a.mu.Unlock()
		reply(w, l, nil)
	})
	mux.HandleFunc("POST /v1/login/cancel", func(w http.ResponseWriter, r *http.Request) {
		a.cancelMSLogin()
		reply(w, map[string]bool{"ok": true}, nil)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(bridgeHeader)), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// bridgePort is used in tests.
func (a *App) bridgePort() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bridge == nil {
		return ""
	}
	return strconv.Itoa(a.bridge.port)
}

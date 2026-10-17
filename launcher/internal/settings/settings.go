// Package settings stores launcher settings and accounts in <data>/settings.json.
package settings

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Account struct {
	ID   string `json:"id"` // UUID without dashes
	Name string `json:"name"`
	Type string `json:"type"` // "microsoft" or "offline"
	// Microsoft only:
	RefreshEnc string    `json:"refreshEnc,omitempty"` // DPAPI-encrypted MS refresh token
	AccessEnc  string    `json:"accessEnc,omitempty"`  // DPAPI-encrypted Minecraft token
	ExpiresAt  time.Time `json:"expiresAt,omitempty"`
	XUID       string    `json:"xuid,omitempty"`
}

type Settings struct {
	Version       string    `json:"version"`
	MemoryMB      int       `json:"memoryMb"`
	GameDir       string    `json:"gameDir"`
	JavaPath      string    `json:"javaPath"` // optional override
	JVMArgs       string    `json:"jvmArgs"`
	ClientID      string    `json:"msClientId"`
	Selected      string    `json:"selected"`
	Accounts      []Account `json:"accounts"`
	Lyrics        bool      `json:"lyrics"`
	MinimizeOnRun bool      `json:"minimizeOnRun"`
	// install the Fabulously Optimized modpack (Sodium, Iris, Lithium...) alongside NovaFX
	FabulouslyOptimized bool `json:"fabulouslyOptimized"`
}

type Store struct {
	mu   sync.Mutex
	path string
	s    Settings
}

// DataDir is %APPDATA%\.nova on Windows (~/.nova elsewhere).
func DataDir() string {
	if a := os.Getenv("APPDATA"); a != "" {
		return filepath.Join(a, ".nova")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".nova")
}

func Defaults(dataDir string) Settings {
	return Settings{
		Version:             "26.3",
		MemoryMB:            4096,
		GameDir:             filepath.Join(dataDir, "game"),
		Lyrics:              true,
		MinimizeOnRun:       true,
		FabulouslyOptimized: true,
	}
}

func Open(dataDir string) *Store {
	st := &Store{path: filepath.Join(dataDir, "settings.json"), s: Defaults(dataDir)}
	if b, err := os.ReadFile(st.path); err == nil {
		_ = json.Unmarshal(b, &st.s)
	}
	if st.s.Version == "" {
		st.s.Version = "26.3"
	}
	if st.s.GameDir == "" {
		st.s.GameDir = filepath.Join(dataDir, "game")
	}
	if st.s.MemoryMB < 1024 {
		st.s.MemoryMB = 4096
	}
	return st
}

func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	s := st.s
	s.Accounts = append([]Account(nil), st.s.Accounts...)
	return s
}

// Update applies fn and saves.
func (st *Store) Update(fn func(*Settings)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.s)
	return st.saveLocked()
}

func (st *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// SelectedAccount returns the selected account, or false if none.
func (s Settings) SelectedAccount() (Account, bool) {
	for _, a := range s.Accounts {
		if a.ID == s.Selected {
			return a, true
		}
	}
	if len(s.Accounts) > 0 {
		return s.Accounts[0], true
	}
	return Account{}, false
}

// Upsert replaces an account with the same ID or appends it, and selects it.
func (s *Settings) Upsert(a Account) {
	for i := range s.Accounts {
		if s.Accounts[i].ID == a.ID {
			s.Accounts[i] = a
			s.Selected = a.ID
			return
		}
	}
	s.Accounts = append(s.Accounts, a)
	s.Selected = a.ID
}

func (s *Settings) Remove(id string) {
	out := s.Accounts[:0]
	for _, a := range s.Accounts {
		if a.ID != id {
			out = append(out, a)
		}
	}
	s.Accounts = out
	if s.Selected == id {
		s.Selected = ""
		if len(out) > 0 {
			s.Selected = out[0].ID
		}
	}
}

// OfflineUUID matches vanilla's offline UUID: UUID v3 of "OfflinePlayer:<name>".
func OfflineUUID(name string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = sum[6]&0x0f | 0x30
	sum[8] = sum[8]&0x3f | 0x80
	return hex.EncodeToString(sum[:])
}

// ValidName checks Minecraft nickname rules (3-16 chars, a-z, 0-9, _).
func ValidName(name string) bool {
	if len(name) < 3 || len(name) > 16 {
		return false
	}
	for _, r := range strings.ToLower(name) {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

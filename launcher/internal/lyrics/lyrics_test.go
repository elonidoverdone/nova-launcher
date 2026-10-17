package lyrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLRC(t *testing.T) {
	src := "[ar:Someone]\n[00:01.50]Первая строка\r\n[00:03.2][01:00.000]Повтор\n[00:02.25]\nне строка\n[00:04.00]a <00:04.50>b"
	got := ParseLRC(src)
	want := []Line{{1500, "Первая строка"}, {2250, ""}, {3200, "Повтор"}, {4000, "a <00:04.50>b"}, {60000, "Повтор"}}
	if len(got) != len(want) {
		t.Fatalf("got %d lines: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHelpers(t *testing.T) {
	if FirstArtist("Скриптонит, 104") != "Скриптонит" || FirstArtist("A feat. B") != "A" || FirstArtist("Solo") != "Solo" {
		t.Error("FirstArtist")
	}
	if CleanTitle("Song (Remastered 2011)") != "Song" || CleanTitle("Song") != "Song" {
		t.Error("CleanTitle")
	}
}

func TestClock(t *testing.T) {
	var c Clock
	c.Reset(Sample{Status: "Playing", PosMs: 10_000, Updated: 1000}, 1500)
	if p := c.Position(1500); p != 10_500 {
		t.Fatalf("start position %d", p)
	}
	if p := c.Position(3500); p != 12_500 {
		t.Fatalf("extrapolated %d", p)
	}
	// same stale report again: no jump
	if c.Observe(Sample{Status: "Playing", PosMs: 10_000, Updated: 1000}, 3500) {
		t.Error("stale sample should not resync")
	}
	// pause freezes the clock
	if !c.Observe(Sample{Status: "Paused", PosMs: 12_400, Updated: 3600}, 3600) {
		t.Error("pause should be reported")
	}
	if c.Position(9000) != 12_400 {
		t.Errorf("paused position %d", c.Position(9000))
	}
	// seek while playing
	c.Observe(Sample{Status: "Playing", PosMs: 12_400, Updated: 10_000}, 10_000)
	if !c.Observe(Sample{Status: "Playing", PosMs: 60_000, Updated: 11_000}, 11_000) {
		t.Error("seek should resync")
	}
	if p := c.Position(12_000); p != 61_000 {
		t.Errorf("after seek %d", p)
	}
}

func TestServiceWritesLyrics(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/api/get":
			if r.URL.Query().Get("artist_name") != "Artist" {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"syncedLyrics": "[00:01.00]one\n[00:02.00]two", "duration": 180})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	LRCLIBURL = srv.URL + "/api"

	dir := t.TempDir()
	out := filepath.Join(dir, "config", "novafx-lyrics.json")
	s := NewService(&Fetcher{CacheDir: filepath.Join(dir, "cache")}, func() string { return out })
	s.Feed(context.Background(), Sample{Title: "Song", Artist: "Artist, Other", Status: "Playing", PosMs: 500, EndMs: 180000})

	var o Output
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(out)
		if json.Unmarshal(b, &o) == nil && o.Status == "ok" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if o.Status != "ok" || len(o.Lines) != 2 || o.Lines[1].Text != "two" || !o.Playing {
		t.Fatalf("output %+v", o)
	}
	// cached on disk: a second service makes no requests
	before := hits
	s2 := NewService(&Fetcher{CacheDir: filepath.Join(dir, "cache")}, func() string { return out })
	if r, err := s2.Fetcher.Find(context.Background(), Track{Title: "Song", Artist: "Artist, Other"}); err != nil || len(r.Lines) != 2 || hits != before {
		t.Errorf("cache miss: %v %v hits=%d", r, err, hits)
	}
	// player closed -> idle
	s.Feed(context.Background(), Sample{Status: "None"})
	b, _ := os.ReadFile(out)
	json.Unmarshal(b, &o)
	if o.Status != "idle" {
		t.Errorf("expected idle, got %s", o.Status)
	}
}

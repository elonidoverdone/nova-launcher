package lyrics

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"nova/launcher/internal/dl"
)

// LRCLIB is a free, open database of time-synced lyrics: https://lrclib.net
var LRCLIBURL = "https://lrclib.net/api"

type Track struct {
	Title  string
	Artist string
	Album  string
	DurMs  int64
}

func (t Track) Key() string {
	return strings.ToLower(strings.TrimSpace(t.Artist) + "\x00" + strings.TrimSpace(t.Title))
}

type lrclibItem struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// Result: Lines is empty when nothing synced was found.
type Result struct {
	Lines        []Line `json:"lines"`
	Instrumental bool   `json:"instrumental"`
}

// Fetcher looks lyrics up on LRCLIB and caches the answers on disk.
type Fetcher struct {
	CacheDir string
}

func (f *Fetcher) cachePath(t Track) string {
	sum := sha1.Sum([]byte(t.Key()))
	return filepath.Join(f.CacheDir, hex.EncodeToString(sum[:8])+".json")
}

func (f *Fetcher) Find(ctx context.Context, t Track) (*Result, error) {
	if f.CacheDir != "" {
		if b, err := os.ReadFile(f.cachePath(t)); err == nil {
			var r Result
			if json.Unmarshal(b, &r) == nil {
				return &r, nil
			}
		}
	}
	r, err := f.lookup(ctx, t)
	if err != nil {
		return nil, err
	}
	if f.CacheDir != "" {
		_ = os.MkdirAll(f.CacheDir, 0o755)
		b, _ := json.Marshal(r)
		_ = os.WriteFile(f.cachePath(t), b, 0o644)
	}
	return r, nil
}

func (f *Fetcher) lookup(ctx context.Context, t Track) (*Result, error) {
	artists := []string{t.Artist}
	if first := FirstArtist(t.Artist); first != t.Artist {
		artists = append(artists, first)
	}
	title := CleanTitle(t.Title)

	// 1) exact match endpoint
	for _, a := range artists {
		q := url.Values{"track_name": {title}, "artist_name": {a}}
		if t.Album != "" {
			q.Set("album_name", t.Album)
		}
		if t.DurMs > 0 {
			q.Set("duration", fmt.Sprint(int(math.Round(float64(t.DurMs)/1000))))
		}
		body, err := dl.Get(ctx, LRCLIBURL+"/get?"+q.Encode())
		if err == nil {
			var it lrclibItem
			if json.Unmarshal(body, &it) == nil {
				if it.Instrumental {
					return &Result{Instrumental: true}, nil
				}
				if lines := ParseLRC(it.SyncedLyrics); len(lines) > 0 {
					return &Result{Lines: lines}, nil
				}
			}
		} else if !dl.IsNotFound(err) {
			return nil, err
		}
	}

	// 2) search, prefer synced lyrics with the closest duration
	for _, a := range artists {
		q := url.Values{"track_name": {title}, "artist_name": {a}}
		body, err := dl.Get(ctx, LRCLIBURL+"/search?"+q.Encode())
		if err != nil {
			if dl.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		var items []lrclibItem
		if json.Unmarshal(body, &items) != nil {
			continue
		}
		if best := pickBest(items, t.DurMs); best != nil {
			return &Result{Lines: ParseLRC(best.SyncedLyrics)}, nil
		}
	}
	return &Result{}, nil
}

func pickBest(items []lrclibItem, durMs int64) *lrclibItem {
	var best *lrclibItem
	bestDiff := math.MaxFloat64
	for i := range items {
		it := &items[i]
		if it.SyncedLyrics == "" {
			continue
		}
		diff := 0.0
		if durMs > 0 {
			diff = math.Abs(it.Duration - float64(durMs)/1000)
			if diff > 8 {
				continue // probably a different recording
			}
		}
		if diff < bestDiff {
			best, bestDiff = it, diff
		}
	}
	return best
}

// FirstArtist turns "A, B" / "A feat. B" / "A & B" into "A".
func FirstArtist(s string) string {
	low := strings.ToLower(s)
	cut := len(s)
	for _, sep := range []string{",", " feat.", " feat ", " ft.", " & ", " x ", " и "} {
		if i := strings.Index(low, sep); i > 0 && i < cut {
			cut = i
		}
	}
	return strings.TrimSpace(s[:cut])
}

// CleanTitle drops tails like " (Remastered 2011)" or " - Live" that rarely match lyric databases.
func CleanTitle(s string) string {
	low := strings.ToLower(s)
	for _, marker := range []string{" (remaster", " - remaster", " (live", " - live", " (prod", " [prod"} {
		if i := strings.Index(low, marker); i > 0 {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

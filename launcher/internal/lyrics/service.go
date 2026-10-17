package lyrics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Sample is one snapshot from the system media controls (Windows SMTC).
type Sample struct {
	App     string `json:"app"`
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Album   string `json:"album"`
	Status  string `json:"status"` // Playing, Paused, Stopped, None...
	PosMs   int64  `json:"posMs"`
	EndMs   int64  `json:"endMs"`
	Updated int64  `json:"updated"` // unix ms when PosMs was measured (0 if unknown)
}

// Output is the file the mod reads (config/novafx-lyrics.json).
type Output struct {
	Status     string `json:"status"` // ok, not_found, searching, idle
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	App        string `json:"app"`
	Playing    bool   `json:"playing"`
	PositionMs int64  `json:"positionMs"`
	CapturedAt int64  `json:"capturedAt"`
	Lines      []Line `json:"lines"`
}

// Clock tracks playback position. Some players report their position rarely,
// so between reports it is extrapolated from the last anchor.
type Clock struct {
	anchorPos  int64
	anchorTime int64
	playing    bool
	lastRawPos int64
	lastRawUpd int64
}

// Observe feeds a sample and returns true if the visible timeline jumped (seek, pause, resume).
func (c *Clock) Observe(s Sample, now int64) bool {
	playing := strings.EqualFold(s.Status, "Playing")
	changed := playing != c.playing
	reported := s.PosMs
	if s.Updated > 0 && playing {
		reported = s.PosMs + (now - s.Updated)
	}
	fresh := s.PosMs != c.lastRawPos || s.Updated != c.lastRawUpd
	c.lastRawPos, c.lastRawUpd = s.PosMs, s.Updated

	est := c.Position(now)
	if changed {
		c.anchorPos, c.anchorTime, c.playing = est, now, playing
		if fresh {
			c.anchorPos = reported
		}
		return true
	}
	// resync only on new data that disagrees noticeably (seek or drift)
	if fresh && abs(reported-est) > 1200 {
		c.anchorPos, c.anchorTime = reported, now
		return true
	}
	return false
}

// Reset starts a new track at position pos.
func (c *Clock) Reset(s Sample, now int64) {
	c.playing = strings.EqualFold(s.Status, "Playing")
	c.anchorPos = s.PosMs
	if s.Updated > 0 && c.playing {
		c.anchorPos += now - s.Updated
	}
	if c.anchorPos < 0 || c.anchorPos > 24*3600*1000 {
		c.anchorPos = 0
	}
	c.anchorTime = now
	c.lastRawPos, c.lastRawUpd = s.PosMs, s.Updated
}

func (c *Clock) Position(now int64) int64 {
	if !c.playing {
		return c.anchorPos
	}
	return c.anchorPos + (now - c.anchorTime)
}

func (c *Clock) Playing() bool { return c.playing }

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// Service consumes media samples, fetches lyrics and writes the output file.
type Service struct {
	Fetcher *Fetcher
	// OutPath returns where to write (it follows the game directory setting).
	OutPath func() string
	Now     func() int64

	mu      sync.Mutex
	key     string
	clock   Clock
	out     Output
	lastPos int64
	gen     int
}

func NewService(f *Fetcher, outPath func() string) *Service {
	return &Service{Fetcher: f, OutPath: outPath, Now: func() int64 { return time.Now().UnixMilli() }, out: Output{Status: "idle"}}
}

// Snapshot is for the launcher UI.
func (s *Service) Snapshot() (Output, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.out
	return o, s.clock.Position(s.Now())
}

// Feed handles one sample.
func (s *Service) Feed(ctx context.Context, smp Sample) {
	now := s.Now()
	s.mu.Lock()
	if smp.Title == "" || strings.EqualFold(smp.Status, "None") || strings.EqualFold(smp.Status, "Closed") {
		if s.out.Status != "idle" {
			s.key = ""
			s.out = Output{Status: "idle"}
			s.writeLocked(now)
		}
		s.mu.Unlock()
		return
	}
	t := Track{Title: smp.Title, Artist: smp.Artist, Album: smp.Album, DurMs: smp.EndMs}
	if t.Key() != s.key {
		s.key = t.Key()
		s.gen++
		gen := s.gen
		s.clock.Reset(smp, now)
		s.out = Output{Status: "searching", Title: smp.Title, Artist: smp.Artist, App: smp.App}
		s.writeLocked(now)
		s.mu.Unlock()
		go s.fetch(ctx, t, gen)
		return
	}
	if s.clock.Observe(smp, now) {
		s.writeLocked(now)
	}
	s.mu.Unlock()
}

func (s *Service) fetch(ctx context.Context, t Track, gen int) {
	res, err := s.Fetcher.Find(ctx, t)
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		return // track changed meanwhile
	}
	switch {
	case err != nil || res == nil:
		s.out.Status = "not_found"
	case len(res.Lines) == 0:
		s.out.Status = "not_found"
	default:
		s.out.Status = "ok"
		s.out.Lines = res.Lines
	}
	s.writeLocked(s.Now())
}

func (s *Service) writeLocked(now int64) {
	s.out.Playing = s.clock.Playing()
	s.out.PositionMs = s.clock.Position(now)
	s.out.CapturedAt = now
	if s.out.Lines == nil {
		s.out.Lines = []Line{}
	}
	path := s.OutPath()
	if path == "" {
		return
	}
	b, err := json.Marshal(s.out)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// Clear marks the overlay idle (used when the feature is switched off).
func (s *Service) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = ""
	s.out = Output{Status: "idle"}
	s.writeLocked(s.Now())
}

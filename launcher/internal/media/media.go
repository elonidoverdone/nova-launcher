// Package media reads the currently playing track from the OS media controls.
package media

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"time"

	"nova/launcher/internal/lyrics"
)

//go:embed smtc.ps1
var script string

// Script returns the PowerShell poller source.
func Script() string { return script }

// readSamples parses JSON lines from r and sends them to fn until EOF.
func readSamples(r io.Reader, fn func(lyrics.Sample)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var s lyrics.Sample
		if json.Unmarshal(sc.Bytes(), &s) == nil {
			fn(s)
		}
	}
}

// Watch runs the platform poller until ctx is cancelled, restarting it if it dies.
func Watch(ctx context.Context, fn func(lyrics.Sample), logf func(string, ...any)) {
	for ctx.Err() == nil {
		err := run(ctx, fn)
		if ctx.Err() != nil {
			return
		}
		if logf != nil {
			logf("media watcher stopped: %v (restarting)", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

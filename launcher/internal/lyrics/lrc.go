// Package lyrics finds time-synced lyrics for the track that is playing and hands them to the mod.
package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Line struct {
	T    int64  `json:"t"` // ms from track start
	Text string `json:"text"`
}

var stampRe = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)

// ParseLRC parses "[mm:ss.xx]text" lines; a line may carry several timestamps.
// Metadata tags like [ar:...] are ignored. The result is sorted by time.
func ParseLRC(src string) []Line {
	var out []Line
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		raw = strings.TrimSpace(raw)
		stamps := stampRe.FindAllStringSubmatchIndex(raw, -1)
		if len(stamps) == 0 {
			continue
		}
		// text is whatever follows the last leading timestamp
		end := 0
		var times []int64
		for _, m := range stamps {
			if m[0] != end {
				break // timestamp in the middle of the text (word-level LRC) — stop here
			}
			end = m[1]
			min, _ := strconv.ParseInt(raw[m[2]:m[3]], 10, 64)
			sec, _ := strconv.ParseInt(raw[m[4]:m[5]], 10, 64)
			var ms int64
			if m[6] >= 0 {
				frac := raw[m[6]:m[7]]
				v, _ := strconv.ParseInt(frac, 10, 64)
				switch len(frac) {
				case 1:
					ms = v * 100
				case 2:
					ms = v * 10
				default:
					ms = v
				}
			}
			times = append(times, min*60000+sec*1000+ms)
		}
		text := strings.TrimSpace(stampRe.ReplaceAllString(raw[end:], ""))
		for _, t := range times {
			out = append(out, Line{T: t, Text: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

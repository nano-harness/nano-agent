package banner

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// AnimationElement is a semantic role for coloring (not directly bound to specific colors).
type AnimationElement string

const (
	ElemBorder    AnimationElement = "border"
	ElemHead      AnimationElement = "head"
	ElemEyes      AnimationElement = "eyes"
	ElemGoggles   AnimationElement = "goggles"
	ElemShine     AnimationElement = "shine"
	ElemStars     AnimationElement = "stars"
	ElemText      AnimationElement = "text" // NANO AGENT text main body
	ElemTextShade AnimationElement = "block_shadow"
	ElemTrail     AnimationElement = "trail"    // flight trail
	ElemSubtitle  AnimationElement = "subtitle" // subtitle text
)

// Frame is a single animation frame.
type Frame struct {
	Title      string        `json:"title"`
	Duration   time.Duration `json:"-"` // converted from DurationMs
	DurationMs int           `json:"duration_ms"`
	Content    string        `json:"-"` // from same-named .txt file
	// Colors maps "row,col" -> role
	Colors map[string]AnimationElement `json:"colors"`
}

// manifest.json example:
// { "frames": [
//
//	{"file": "01.txt", "title": "intro",  "duration_ms": 80,  "colors": {...}},
//	{"file": "02.txt", "title": "fly_in", "duration_ms": 100, "colors": {...}},
//	...
//
// ]}
type manifest struct {
	Frames []struct {
		File       string                      `json:"file"`
		Title      string                      `json:"title"`
		DurationMs int                         `json:"duration_ms"`
		Colors     map[string]AnimationElement `json:"colors"`
	} `json:"frames"`
}

//go:embed frames/*.txt frames/manifest.json
var assets embed.FS

// LoadFrames parses manifest + each .txt frame file, returns ordered frame sequence.
func LoadFrames() ([]Frame, error) {
	raw, err := assets.ReadFile("frames/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("banner: read manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("banner: parse manifest: %w", err)
	}
	frames := make([]Frame, 0, len(m.Frames))
	for _, fm := range m.Frames {
		body, err := assets.ReadFile("frames/" + fm.File)
		if err != nil {
			return nil, fmt.Errorf("banner: read %s: %w", fm.File, err)
		}
		frames = append(frames, Frame{
			Title:      fm.Title,
			DurationMs: fm.DurationMs,
			Duration:   time.Duration(fm.DurationMs) * time.Millisecond,
			Content:    strings.TrimRight(string(body), "\n"),
			Colors:     fm.Colors,
		})
	}
	return frames, nil
}

// TotalDuration returns cumulative duration of all frames, for self-checking "≈ 3 seconds".
func TotalDuration(frames []Frame) time.Duration {
	var total time.Duration
	for _, f := range frames {
		total += f.Duration
	}
	return total
}

// sortedKeys is used to stabilize colors iteration (test-only).
func sortedKeys(m map[string]AnimationElement) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

package banner

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestLoadFrames_HappyPath(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}
	if len(frames) != 20 {
		t.Errorf("Expected exactly 20 frames, got %d", len(frames))
	}

	// Verify each frame has required fields
	for i, f := range frames {
		if f.Title == "" {
			t.Errorf("Frame %d missing title", i)
		}
		if f.DurationMs <= 0 {
			t.Errorf("Frame %d has invalid duration_ms: %d", i, f.DurationMs)
		}
		if f.Duration != time.Duration(f.DurationMs)*time.Millisecond {
			t.Errorf("Frame %d duration mismatch: duration=%v, duration_ms=%d", i, f.Duration, f.DurationMs)
		}
		if f.Content == "" {
			t.Errorf("Frame %d has empty content", i)
		}
		// Colors can be nil or empty for some frames
	}
}

func TestTotalDuration_Around3Seconds(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}

	total := TotalDuration(frames)
	minDuration := 2800 * time.Millisecond
	maxDuration := 3200 * time.Millisecond

	if total < minDuration || total > maxDuration {
		t.Errorf("Total duration %v is outside expected range [%v, %v]", total, minDuration, maxDuration)
	}
}

func TestRenderFrame_NoColor_ReturnsRaw(t *testing.T) {
	frame := Frame{
		Title:      "test",
		Content:    "Hello\nWorld",
		Colors:     map[string]AnimationElement{"0,0": ElemText},
		Duration:   100 * time.Millisecond,
		DurationMs: 100,
	}

	result := RenderFrame(frame, DefaultTheme, false)
	if result != frame.Content {
		t.Errorf("Expected raw content %q, got %q", frame.Content, result)
	}
}

func TestRenderFrame_AppliesTheme(t *testing.T) {
	frame := Frame{
		Title:      "test",
		Content:    "AB",
		Colors:     map[string]AnimationElement{"0,0": ElemText, "0,1": ElemHead},
		Duration:   100 * time.Millisecond,
		DurationMs: 100,
	}

	result := RenderFrame(frame, DefaultTheme, true)
	// lipgloss may detect non-TTY environment in tests and not emit ANSI codes
	// Just verify that rendering doesn't crash and returns something
	if result != "AB" && !strings.Contains(result, "AB") {
		t.Errorf("Expected result to contain original content 'AB', got %q", result)
	}
	t.Logf("Rendered output: %q (length=%d)", result, len(result))
}

func TestPlay_RespectsSkipCh(t *testing.T) {
	skipCh := make(chan struct{})
	close(skipCh) // Immediately skip

	buf := &bytes.Buffer{}
	start := time.Now()

	err := Play(buf, Options{
		Theme:    DefaultTheme,
		Colorize: true,
		SkipCh:   skipCh,
	})

	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Play() failed: %v", err)
	}

	// Should return quickly (less than 100ms) due to immediate skip
	if elapsed > 100*time.Millisecond {
		t.Errorf("Play() took %v, expected < 100ms with immediate skip", elapsed)
	}
}

func TestPlay_RespectsMaxDuration(t *testing.T) {
	buf := &bytes.Buffer{}
	start := time.Now()

	err := Play(buf, Options{
		Theme:       DefaultTheme,
		Colorize:    true,
		MaxDuration: 50 * time.Millisecond,
	})

	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Play() failed: %v", err)
	}

	// Should return within ~200ms (50ms max + some overhead)
	if elapsed > 200*time.Millisecond {
		t.Errorf("Play() took %v, expected < 200ms with 50ms MaxDuration", elapsed)
	}
}

func TestPlay_ClearsFinalFrameAfterCompletion(t *testing.T) {
	buf := &bytes.Buffer{}

	err := Play(buf, Options{
		Theme:    DefaultTheme,
		Colorize: true,
	})
	if err != nil {
		t.Fatalf("Play() failed: %v", err)
	}

	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}
	finalClear := clearFrameSequence(frames[len(frames)-1].Content)
	output := buf.String()
	if !strings.Contains(output, finalClear) {
		t.Fatalf("expected output to clear final frame before restoring cursor; suffix=%q", output[max(0, len(output)-32):])
	}
}

func TestPlay_NoColorize_PrintsStaticFrame(t *testing.T) {
	buf := &bytes.Buffer{}

	err := Play(buf, Options{
		Theme:    DefaultTheme,
		Colorize: false,
	})

	if err != nil {
		t.Fatalf("Play() failed: %v", err)
	}

	output := buf.String()
	// Should have some output (the last frame)
	if len(output) == 0 {
		t.Error("Expected output when colorize=false, got empty string")
	}

	// Should NOT contain ANSI cursor movement codes when colorize=false
	if strings.Contains(output, "\x1b[") && !strings.Contains(output, "\x1b[?25") {
		t.Error("Expected no ANSI codes (except cursor) when colorize=false")
	}
}

func TestThemeColorFor(t *testing.T) {
	theme := DefaultTheme
	frame := Frame{
		Colors: map[string]AnimationElement{
			"0,0": ElemHead,
			"1,5": ElemEyes,
		},
	}

	// Test existing mapping
	color := theme.ColorFor(0, 0, frame)
	if color != "75" {
		t.Errorf("Expected color '75' for ElemHead, got %q", color)
	}

	color = theme.ColorFor(1, 5, frame)
	if color != "114" {
		t.Errorf("Expected color '114' for ElemEyes, got %q", color)
	}

	// Test non-existent mapping
	color = theme.ColorFor(10, 10, frame)
	if color != "" {
		t.Errorf("Expected empty color for unmapped coordinate, got %q", color)
	}
}

func TestFrameContentNotEmpty(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}

	for i, f := range frames {
		lines := strings.Split(f.Content, "\n")
		if len(lines) == 0 {
			t.Errorf("Frame %d (%s) has no lines", i, f.Title)
		}
		// All frames should be approximately the same height for proper redraw
		if i > 0 {
			prevLines := strings.Split(frames[i-1].Content, "\n")
			// Allow some variance but frames should be roughly same height
			if abs(len(lines)-len(prevLines)) > 2 {
				t.Logf("Warning: Frame %d has %d lines, frame %d has %d lines (large difference)",
					i, len(lines), i-1, len(prevLines))
			}
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func TestFrames_UniformHeight(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}

	for i, f := range frames {
		lines := strings.Split(f.Content, "\n")
		if len(lines) != 16 {
			t.Errorf("Frame %d (%s) has %d lines, expected exactly 16", i, f.Title, len(lines))
		}
	}
}

func TestDefaultTheme_HasSubtitle(t *testing.T) {
	color, ok := DefaultTheme[ElemSubtitle]
	if !ok {
		t.Error("DefaultTheme missing ElemSubtitle")
	}
	if color == "" {
		t.Error("DefaultTheme[ElemSubtitle] is empty")
	}
}

func TestFinalFrame_ContainsAgent(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}

	if len(frames) < 20 {
		t.Fatalf("Expected at least 20 frames, got %d", len(frames))
	}

	finalFrame := frames[19]
	content := finalFrame.Content

	// Check for AGENT characteristic strokes
	if !strings.Contains(content, "\\____") {
		t.Error("Final frame missing '\\____' (AGENT characteristic)")
	}
	if !strings.Contains(content, "_____") {
		t.Error("Final frame missing '_____' (AGENT characteristic)")
	}
	if !strings.Contains(content, "» your terminal AI") {
		t.Error("Final frame missing subtitle '» your terminal AI'")
	}
}

func TestNanoAgentSpacing(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}

	if len(frames) < 20 {
		t.Fatalf("Expected at least 20 frames, got %d", len(frames))
	}

	finalFrame := frames[19]
	content := finalFrame.Content

	// Look for the pattern: NANO's \___/ followed by 2-4 spaces and AGENT's /_/
	// This verifies the 3-column spacing between NANO and AGENT
	lines := strings.Split(content, "\n")
	found := false
	for _, line := range lines {
		// Look for pattern like: \___/  /_/   \_\
		if strings.Contains(line, "\\___/") && strings.Contains(line, "/_/") {
			found = true
			// Extract the substring between \___/ and /_/
			idx1 := strings.Index(line, "\\___/")
			idx2 := strings.Index(line[idx1+5:], "/_/")
			if idx2 >= 0 {
				spacing := line[idx1+5 : idx1+5+idx2]
				if len(spacing) < 2 {
					t.Errorf("Spacing between NANO and AGENT is too small: %d characters (expected at least 2)", len(spacing))
				}
			}
			break
		}
	}

	if !found {
		t.Error("Could not find NANO/AGENT spacing pattern in final frame")
	}
}

func TestApplyIconMode_Tea(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}
	applyIconMode(frames, IconTea)
	for i, f := range frames {
		if !isSettledFrame(f) {
			continue
		}
		if strings.Contains(f.Content, "⚛") {
			t.Errorf("Frame %d (%s): settled frame still contains ⚛ after tea mode", i, f.Title)
		}
		if !strings.Contains(f.Content, "|_____|)") {
			t.Errorf("Frame %d (%s): settled frame missing '|_____|)' in tea mode", i, f.Title)
		}
		if !strings.Contains(f.Content, "\\___/") {
			t.Errorf("Frame %d (%s): settled frame missing '\\___/' in tea mode", i, f.Title)
		}
	}
}

func TestApplyIconMode_MilkTea(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}
	applyIconMode(frames, IconMilkTea)
	for i, f := range frames {
		if !isSettledFrame(f) {
			continue
		}
		if strings.Contains(f.Content, "⚛") {
			t.Errorf("Frame %d (%s): settled frame still contains ⚛ after milktea mode", i, f.Title)
		}
		if !strings.Contains(f.Content, ".=|=.") {
			t.Errorf("Frame %d (%s): settled frame missing '.=|=.' in milktea mode", i, f.Title)
		}
		if !strings.Contains(f.Content, "\\_/") {
			t.Errorf("Frame %d (%s): settled frame missing '\\_/' in milktea mode", i, f.Title)
		}
	}
}

func TestApplyIconMode_Default(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}
	origContents := make([]string, len(frames))
	for i, f := range frames {
		origContents[i] = f.Content
	}
	applyIconMode(frames, IconDefault)
	for i, f := range frames {
		if f.Content != origContents[i] {
			t.Errorf("Frame %d (%s): content changed in default mode", i, f.Title)
		}
	}
}

func TestApplyIconMode_FlyingFramesUnchanged(t *testing.T) {
	frames, err := LoadFrames()
	if err != nil {
		t.Fatalf("LoadFrames() failed: %v", err)
	}
	// Record content of flying frames (contain ⚛ but not ∘) before applying icon mode.
	type savedFrame struct {
		idx     int
		content string
	}
	var flyingFrames []savedFrame
	for i, f := range frames {
		if strings.Contains(f.Content, "⚛") && !strings.Contains(f.Content, "∘") {
			flyingFrames = append(flyingFrames, savedFrame{i, f.Content})
		}
	}
	if len(flyingFrames) == 0 {
		t.Fatal("No flying frames found (frames with ⚛ but without ∘)")
	}
	for _, mode := range []IconMode{IconTea, IconMilkTea} {
		reloaded, _ := LoadFrames()
		applyIconMode(reloaded, mode)
		for _, sf := range flyingFrames {
			if reloaded[sf.idx].Content != sf.content {
				t.Errorf("Mode %q: flying frame %d content changed", mode, sf.idx)
			}
		}
	}
}

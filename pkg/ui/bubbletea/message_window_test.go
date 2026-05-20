package bubbletea

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

// TestRenderMessageWindow_HandlesEmptyStore verifies that an empty
// MessageStore (or maxLines <= 0) produces an empty string instead of
// panicking, so callers can substitute the result directly into a View
// builder without guarding.
func TestRenderMessageWindow_HandlesEmptyStore(t *testing.T) {
	m := newTestModel(80)

	if got := m.renderMessageWindow(10); got != "" {
		t.Fatalf("empty store should produce empty render, got %q", got)
	}

	m.recordMessage("user", "hello")
	if got := m.renderMessageWindow(0); got != "" {
		t.Fatalf("maxLines=0 should produce empty render, got %q", got)
	}
	if got := m.renderMessageWindow(-5); got != "" {
		t.Fatalf("negative maxLines should produce empty render, got %q", got)
	}

	// nil receiver guard.
	var nilModel *Model
	if got := nilModel.renderMessageWindow(10); got != "" {
		t.Fatalf("nil receiver should produce empty render, got %q", got)
	}
}

// TestRenderMessageWindow_FitsWithinMaxLines verifies that when all
// stored messages comfortably fit within maxLines, every message is
// rendered in insertion order and the total row count stays within the
// budget.
func TestRenderMessageWindow_FitsWithinMaxLines(t *testing.T) {
	m := newTestModel(80)
	m.recordMessage("user", "first")
	m.recordMessage("assistant", "second")
	m.recordMessage("system", "third")

	out := m.renderMessageWindow(50)
	plain := xansi.Strip(out)

	// All three messages should appear in insertion order.
	idxFirst := strings.Index(plain, "first")
	idxSecond := strings.Index(plain, "second")
	idxThird := strings.Index(plain, "third")
	if idxFirst < 0 || idxSecond < 0 || idxThird < 0 {
		t.Fatalf("expected all three messages in output, got %q", plain)
	}
	if !(idxFirst < idxSecond && idxSecond < idxThird) {
		t.Fatalf("expected messages in insertion order (first<second<third), got %q", plain)
	}

	// Output must not exceed the budget.
	rowCount := strings.Count(out, "\n") + 1
	if rowCount > 50 {
		t.Fatalf("output %d rows exceeds maxLines=50", rowCount)
	}

	// And no truncation marker should appear when everything fits.
	if strings.Contains(plain, "行已截断") {
		t.Fatalf("did not expect truncation marker when content fits, got %q", plain)
	}
}

// TestRenderMessageWindow_TruncatesTopMessage verifies that when the
// total message height exceeds the budget, the oldest visible message
// is truncated from the top with a marker, and the most recent message
// is preserved in full.
func TestRenderMessageWindow_TruncatesTopMessage(t *testing.T) {
	m := newTestModel(80)
	// Build a tall first message (10 lines) and a short second message.
	tall := strings.Repeat("aaaa\n", 9) + "aaaa"
	m.recordMessage("user", tall)
	m.recordMessage("assistant", "short reply")

	// maxLines=5: short reply (1 styled row) + truncated tall message
	// to fill the rest of the budget.
	out := m.renderMessageWindow(5)
	plain := xansi.Strip(out)

	rowCount := strings.Count(out, "\n") + 1
	if rowCount > 5 {
		t.Fatalf("output exceeds budget: got %d rows, want <=5", rowCount)
	}

	if !strings.Contains(plain, "short reply") {
		t.Fatalf("most recent message must remain visible, got %q", plain)
	}
	if !strings.Contains(plain, "行已截断") {
		t.Fatalf("expected truncation marker in output, got %q", plain)
	}

	// Pure-unbounded budget shouldn't truncate.
	full := xansi.Strip(m.renderMessageWindow(100))
	if strings.Contains(full, "行已截断") {
		t.Fatalf("large budget should not truncate, got %q", full)
	}
}

// TestView_IncludesMessageWindowAboveInputSection verifies that View() renders
// messages from the MessageStore above the input section when termHeight is
// set, and that the total row count does not exceed termHeight.
func TestView_IncludesMessageWindowAboveInputSection(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 24

	m.recordMessage("user", "hello world")
	m.recordMessage("assistant", "hi there")

	v := m.View()
	plain := xansi.Strip(v.Content)

	if !strings.Contains(plain, "hello world") {
		t.Errorf("expected user message in View(), got %q", plain)
	}
	if !strings.Contains(plain, "hi there") {
		t.Errorf("expected assistant message in View(), got %q", plain)
	}
	// Input section separator should always be present.
	if !strings.Contains(plain, "─────") {
		t.Errorf("expected input section separator in View(), got %q", plain)
	}
	// Total rows must not exceed terminal height.
	rows := strings.Count(plain, "\n") + 1
	if rows > m.termHeight+2 { // +2 for rounding tolerance
		t.Errorf("View() produced %d rows, exceeds termHeight=%d", rows, m.termHeight)
	}
	// Messages must appear above the separator.
	msgIdx := strings.Index(plain, "hello world")
	sepIdx := strings.Index(plain, "─────")
	if msgIdx >= sepIdx {
		t.Errorf("expected message before separator; msgIdx=%d sepIdx=%d", msgIdx, sepIdx)
	}
}

// TestView_NoMessageWindowWhenStoreEmpty verifies that View() still renders
// the input section correctly even when the MessageStore is empty.
func TestView_NoMessageWindowWhenStoreEmpty(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 24

	v := m.View()
	plain := xansi.Strip(v.Content)

	if !strings.Contains(plain, "─────") {
		t.Errorf("expected input section separator even with empty store, got %q", plain)
	}
}

// TestRenderedMessageLines_CachesOnFirstCall verifies that the first call to
// renderedMessageLines populates msg.Rendered and that subsequent calls reuse
// the cached value without re-computing.
func TestRenderedMessageLines_CachesOnFirstCall(t *testing.T) {
	m := newTestModel(80)
	msg := m.messages.Add("user", "hello")

	if msg.Rendered != "" {
		t.Fatal("Rendered should be empty before first render")
	}

	rows1 := m.renderedMessageLines(msg)
	if msg.Rendered == "" {
		t.Fatal("renderedMessageLines should populate msg.Rendered")
	}
	cached := msg.Rendered

	// Mutate content without invalidating — cache should be reused.
	msg.Content = "should not be rendered"
	rows2 := m.renderedMessageLines(msg)

	if msg.Rendered != cached {
		t.Fatalf("cache should not be recomputed, got %q", msg.Rendered)
	}
	if len(rows1) != len(rows2) {
		t.Fatalf("row count mismatch: %d vs %d", len(rows1), len(rows2))
	}
}

// TestRenderedMessageLines_InvalidateClearsCacheAndRecomputes verifies that
// msg.InvalidateCache() forces a fresh render on the next call so that the
// new content is reflected.
func TestRenderedMessageLines_InvalidateClearsCacheAndRecomputes(t *testing.T) {
	m := newTestModel(80)
	msg := m.messages.Add("assistant", "original")

	// Warm cache.
	_ = m.renderedMessageLines(msg)
	original := msg.Rendered
	if original == "" {
		t.Fatal("expected non-empty Rendered after first call")
	}

	// Update content and invalidate.
	msg.Content = "updated"
	msg.InvalidateCache()

	rows := m.renderedMessageLines(msg)
	if msg.Rendered == original {
		t.Fatalf("expected Rendered to be recomputed after InvalidateCache, still got %q", msg.Rendered)
	}
	if len(rows) == 0 {
		t.Fatal("expected non-empty rows after recompute")
	}
}

// TestRenderedMessageLines_SetsHeight verifies that renderedMessageLines
// populates msg.Height (via SetRendered) so that the FullscreenModel's
// virtual-scrolling layout (which sums msg.Height values) sees a correct
// line count without needing a separate render pass.
func TestRenderedMessageLines_SetsHeight(t *testing.T) {
	m := newTestModel(80)

	// Multi-line content to ensure Height > 1 is exercised.
	msg := m.messages.Add("user", "line one\nline two\nline three")

	if msg.Height != 0 {
		t.Fatalf("Height should be 0 before first render, got %d", msg.Height)
	}

	rows := m.renderedMessageLines(msg)
	if msg.Height == 0 {
		t.Fatal("renderedMessageLines should populate msg.Height via SetRendered")
	}
	if msg.Height != len(rows) {
		t.Fatalf("msg.Height=%d should equal len(renderedLines)=%d", msg.Height, len(rows))
	}

	// After InvalidateCache, Height must be reset to 0 and restored on re-render.
	msg.InvalidateCache()
	if msg.Height != 0 {
		t.Fatalf("InvalidateCache should reset Height to 0, got %d", msg.Height)
	}

	rows2 := m.renderedMessageLines(msg)
	if msg.Height != len(rows2) {
		t.Fatalf("Height after re-render: got %d, want %d", msg.Height, len(rows2))
	}
}

// assistant_stream messages are wrapped at the full terminal width rather than
// at the narrower safeWrapWidth that reserves columns for emoji prefixes.
// assistant_stream does not add a prefix in formatLine, so using the prefix
// reserve wastes 4 columns of display width per line.
func TestRenderedMessageLines_AssistantStreamUsesFullWidth(t *testing.T) {
	// termWidth=20, safeWrapWidth = 20-4 = 16.
	// Content "word1 word234567890" is 19 visible chars:
	//   - at wrapWidth=20 it fits in ONE line (19 ≤ 20)
	//   - at wrapWidth=16 wordwrap breaks before "word234567890" → TWO lines
	// A correct implementation must use wrapWidth=20 (full termWidth).
	const termWidth = 20
	m := newTestModel(termWidth)
	msg := m.messages.Add("assistant_stream", "word1 word234567890")

	rows := m.renderedMessageLines(msg)

	if len(rows) != 1 {
		t.Fatalf("assistant_stream: expected 1 rendered line at termWidth=%d (prefix reserve must not apply), got %d lines: %v",
			termWidth, len(rows), rows)
	}
	if w := xansi.StringWidth(rows[0]); w > termWidth {
		t.Fatalf("rendered line exceeds termWidth=%d (width=%d): %q", termWidth, w, rows[0])
	}
}

// TestRenderedMessageLines_PrefixRoleUsesReservedWidth verifies that roles with
// an emoji prefix (e.g. "user") do apply the formattedLinePrefixReserve, so
// the emoji and its content together do not overflow the terminal width.
func TestRenderedMessageLines_PrefixRoleUsesReservedWidth(t *testing.T) {
	const termWidth = 20
	m := newTestModel(termWidth)
	// "word1 word234567890" is 19 visible chars; the "user" role prepends
	// "👤 " (3 visible cols) making the styled first-line 22 cols wide —
	// over termWidth. The prefix reserve should cause wordwrap to break
	// the content so that no line exceeds termWidth after the prefix.
	msg := m.messages.Add("user", "word1 word234567890")

	rows := m.renderedMessageLines(msg)

	for i, line := range rows {
		if w := xansi.StringWidth(line); w > termWidth {
			t.Fatalf("user role: rendered line %d exceeds termWidth=%d (width=%d): %q",
				i+1, termWidth, w, line)
		}
	}
}

// MessageStore.InvalidateCache() (called on terminal resize) clears all
// per-message caches and that the next renderMessageWindow call recomputes
// them correctly.
func TestRenderMessageWindow_UsesCacheAfterInvalidation(t *testing.T) {
	m := newTestModel(80)
	m.recordMessage("user", "first")
	m.recordMessage("assistant", "second")

	// First render warms the cache.
	out1 := m.renderMessageWindow(50)
	if out1 == "" {
		t.Fatal("expected non-empty output from first render")
	}
	// Verify both messages have their cache populated.
	m.messages.Range(func(_ int, msg *FormattedMessage) bool {
		if msg.Rendered == "" {
			t.Errorf("expected Rendered to be populated for %q after render", msg.Content)
		}
		return true
	})

	// Simulate resize: invalidate and change width.
	m.messages.InvalidateCache()
	m.termWidth = 40

	out2 := m.renderMessageWindow(50)
	if out2 == "" {
		t.Fatal("expected non-empty output after invalidation")
	}

	// After re-render with narrower width all messages should have fresh cache.
	m.messages.Range(func(_ int, msg *FormattedMessage) bool {
		if msg.Rendered == "" {
			t.Errorf("expected Rendered to be repopulated for %q after resize render", msg.Content)
		}
		return true
	})
}

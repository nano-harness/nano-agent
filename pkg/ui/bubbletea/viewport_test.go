package bubbletea

import (
	"testing"
)

func TestViewportScroll(t *testing.T) {
	vp := NewViewportState(10)

	// Test initial state
	if vp.ScrollOffset != 0 {
		t.Errorf("Expected initial scroll offset 0, got %d", vp.ScrollOffset)
	}
	if !vp.IsSticky {
		t.Error("Expected initial sticky mode to be true")
	}

	// Set total height first (this will auto-scroll to bottom in sticky mode)
	vp.SetTotalHeight(100)
	expectedBottomScroll := 90 // 100 - 10 viewport height
	if vp.ScrollOffset != expectedBottomScroll {
		t.Errorf("Expected scroll offset %d after SetTotalHeight in sticky mode, got %d", expectedBottomScroll, vp.ScrollOffset)
	}

	// Test scrolling up (disables sticky)
	vp.ScrollUp(5)
	if vp.ScrollOffset != 85 {
		t.Errorf("Expected scroll offset 85 after scrolling up 5, got %d", vp.ScrollOffset)
	}
	if vp.IsSticky {
		t.Error("Expected sticky mode to be disabled after scrolling up")
	}

	// Test scrolling down
	vp.ScrollDown(10)
	if vp.ScrollOffset != 90 {
		t.Errorf("Expected scroll offset 90, got %d", vp.ScrollOffset)
	}

	// Should re-enable sticky when we reach the bottom
	if !vp.IsSticky {
		t.Error("Expected sticky mode to be re-enabled when scrolling to bottom")
	}

	// Test scroll to top
	vp.ScrollToTop()
	if vp.ScrollOffset != 0 {
		t.Errorf("Expected scroll offset 0 after scrolling to top, got %d", vp.ScrollOffset)
	}
	if vp.IsSticky {
		t.Error("Expected sticky mode to be disabled after scrolling to top")
	}
}

func TestViewportPageScroll(t *testing.T) {
	vp := NewViewportState(10)
	// Disable sticky mode for this test
	vp.IsSticky = false
	vp.SetTotalHeight(100)

	// Reset to 0 after SetTotalHeight
	vp.ScrollOffset = 0

	// Test page down
	vp.PageDown()
	if vp.ScrollOffset != 10 {
		t.Errorf("Expected scroll offset 10 after page down, got %d", vp.ScrollOffset)
	}

	// Test page up
	vp.PageUp()
	if vp.ScrollOffset != 0 {
		t.Errorf("Expected scroll offset 0 after page up, got %d", vp.ScrollOffset)
	}
}

func TestViewportVisibleRange(t *testing.T) {
	vp := NewViewportState(10)
	// Disable overscan to test pure visible-range semantics.
	vp.OverscanRows = 0

	// Test with 5 messages of varying heights
	heights := []int{2, 3, 5, 4, 6} // total = 20 lines
	vp.SetTotalHeight(20)
	vp.ScrollToTop()

	// At scroll offset 0, should see first few messages
	start, end := vp.VisibleRange(heights)
	if start != 0 {
		t.Errorf("Expected start index 0, got %d", start)
	}
	// Should include messages until we fill the viewport (10 lines)
	// Message 0 (2 lines) + Message 1 (3 lines) + Message 2 (5 lines) = 10 lines
	if end != 3 {
		t.Errorf("Expected end index 3, got %d", end)
	}

	// Scroll down to see later messages
	vp.ScrollDown(5)
	start, end = vp.VisibleRange(heights)
	// Should start somewhere in the middle
	if start < 1 || start > 2 {
		t.Errorf("Expected start index 1 or 2, got %d", start)
	}
}

// TestViewportVisibleRangeOverscan verifies the default OverscanRows widens
// the materialized range above and below the literal viewport.
func TestViewportVisibleRangeOverscan(t *testing.T) {
	vp := NewViewportState(10)
	// 200 messages of height 1 — total 200 lines, well beyond overscan.
	heights := make([]int, 200)
	for i := range heights {
		heights[i] = 1
	}
	vp.SetTotalHeight(200)
	vp.ScrollOffset = 100
	vp.IsSticky = false

	start, end := vp.VisibleRange(heights)
	// With ScrollOffset=100, ViewportHeight=10, OverscanRows=80 by default:
	//   viewportTop    = 100 - 80 = 20
	//   viewportBottom = 100 + 10 + 80 = 190
	// So range should be roughly [20, 190).
	if start != 20 {
		t.Errorf("expected start ~20 with overscan, got %d", start)
	}
	if end != 190 {
		t.Errorf("expected end ~190 with overscan, got %d", end)
	}
}

// TestViewportVisibleRangeMaxMounted verifies the MaxMounted cap.
func TestViewportVisibleRangeMaxMounted(t *testing.T) {
	vp := NewViewportState(10)
	vp.MaxMounted = 50
	heights := make([]int, 1000)
	for i := range heights {
		heights[i] = 1
	}
	vp.SetTotalHeight(1000)
	vp.ScrollOffset = 500
	vp.IsSticky = false

	start, end := vp.VisibleRange(heights)
	if end-start > 50 {
		t.Errorf("expected at most 50 mounted messages, got %d (start=%d end=%d)", end-start, start, end)
	}
}

// TestViewportVisibleRangeBinarySearch sanity-checks that the binary search
// produces the same start/end as the prior linear scan on a 10k-message list.
func TestViewportVisibleRangeBinarySearch(t *testing.T) {
	vp := NewViewportState(40)
	vp.OverscanRows = 0
	vp.MaxMounted = 0
	heights := make([]int, 10000)
	for i := range heights {
		heights[i] = 1
	}
	vp.SetTotalHeight(10000)
	vp.ScrollOffset = 7777
	vp.IsSticky = false

	start, end := vp.VisibleRange(heights)
	if start != vp.ScrollOffset {
		t.Errorf("expected start=%d, got %d", vp.ScrollOffset, start)
	}
	wantEnd := vp.ScrollOffset + vp.ViewportHeight
	if end != wantEnd {
		t.Errorf("expected end=%d, got %d", wantEnd, end)
	}
}

func TestViewportSticky(t *testing.T) {
	vp := NewViewportState(10)

	// Test sticky behavior on content growth
	vp.SetTotalHeight(5)
	if vp.ScrollOffset != 0 {
		t.Errorf("Expected scroll offset 0 with small content, got %d", vp.ScrollOffset)
	}

	// Grow content while sticky - should auto-scroll
	vp.SetTotalHeight(50)
	expectedOffset := 40 // 50 - 10 viewport height
	if vp.ScrollOffset != expectedOffset {
		t.Errorf("Expected scroll offset %d after content growth in sticky mode, got %d", expectedOffset, vp.ScrollOffset)
	}

	// Disable sticky and grow content - should not auto-scroll
	vp.ScrollToTop()
	vp.SetTotalHeight(60)
	if vp.ScrollOffset != 0 {
		t.Errorf("Expected scroll offset 0 after content growth without sticky mode, got %d", vp.ScrollOffset)
	}
}

func TestViewportScrollPercentage(t *testing.T) {
	vp := NewViewportState(10)

	// Test with small content (should be 100%)
	vp.SetTotalHeight(5)
	pct := vp.ScrollPercentage()
	if pct != 100.0 {
		t.Errorf("Expected 100%% for small content, got %.2f%%", pct)
	}

	// Test at top (should be 0%)
	vp.SetTotalHeight(100)
	vp.ScrollToTop()
	pct = vp.ScrollPercentage()
	if pct != 0.0 {
		t.Errorf("Expected 0%% at top, got %.2f%%", pct)
	}

	// Test at bottom (should be 100%)
	vp.ScrollToBottom()
	pct = vp.ScrollPercentage()
	if pct != 100.0 {
		t.Errorf("Expected 100%% at bottom, got %.2f%%", pct)
	}

	// Test at middle (should be ~50%)
	vp.ScrollOffset = 45 // Middle of 0-90 range
	pct = vp.ScrollPercentage()
	if pct < 49.0 || pct > 51.0 {
		t.Errorf("Expected ~50%% at middle, got %.2f%%", pct)
	}
}

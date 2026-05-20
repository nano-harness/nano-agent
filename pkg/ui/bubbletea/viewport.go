package bubbletea

import "sort"

// Virtual scrolling constants modeled after Claude Code's fullscreen renderer.
// These bound the amount of work the renderer performs per frame even when
// history grows arbitrarily large.
const (
	// DefaultOverscanRows renders this many extra rows above and below the
	// visible viewport so small scroll deltas don't require re-laying out
	// the message list. Mirrors Claude Code's OVERSCAN_ROWS = 80.
	DefaultOverscanRows = 80

	// DefaultMaxMounted caps the number of FormattedMessage entries the
	// renderer materializes per frame. Mirrors Claude Code's
	// MAX_MOUNTED_ITEMS = 300.
	DefaultMaxMounted = 300

	// DefaultScrollQuantum batches scroll deltas under this size into a
	// single frame to reduce reflow churn during fast wheel events.
	// Mirrors Claude Code's SCROLL_QUANTUM = 40.
	DefaultScrollQuantum = 40
)

// ViewportState manages virtual scrolling for the fullscreen TUI.
// It tracks scroll position, viewport dimensions, and auto-scroll behavior.
type ViewportState struct {
	ScrollOffset   int  // Current scroll offset (in lines from top)
	ViewportHeight int  // Height of visible viewport (in lines)
	TotalHeight    int  // Total height of all messages combined
	IsSticky       bool // Auto-scroll to bottom when new content arrives

	// OverscanRows is the number of extra rows to render above and below the
	// visible viewport. Defaults to DefaultOverscanRows.
	OverscanRows int
	// MaxMounted caps how many messages VisibleRange will materialize per
	// frame. Defaults to DefaultMaxMounted.
	MaxMounted int
	// ScrollQuantum is the minimum scroll delta the viewport responds to.
	// Defaults to DefaultScrollQuantum; if 0 every delta is applied.
	ScrollQuantum int
}

// NewViewportState creates a new viewport with sticky scroll enabled by default.
func NewViewportState(viewportHeight int) *ViewportState {
	return &ViewportState{
		ScrollOffset:   0,
		ViewportHeight: viewportHeight,
		TotalHeight:    0,
		IsSticky:       true,
		OverscanRows:   DefaultOverscanRows,
		MaxMounted:     DefaultMaxMounted,
		ScrollQuantum:  DefaultScrollQuantum,
	}
}

// SetViewportHeight updates the viewport height and adjusts scroll if needed.
func (v *ViewportState) SetViewportHeight(height int) {
	if height < 1 {
		height = 1
	}
	v.ViewportHeight = height
	v.constrainScroll()
}

// SetTotalHeight updates the total content height and auto-scrolls if sticky.
func (v *ViewportState) SetTotalHeight(height int) {
	if height < 0 {
		height = 0
	}
	oldTotal := v.TotalHeight
	v.TotalHeight = height

	// If sticky and content grew, scroll to bottom
	if v.IsSticky && height > oldTotal {
		v.ScrollToBottom()
	} else {
		v.constrainScroll()
	}
}

// ScrollUp scrolls up by the specified number of lines.
func (v *ViewportState) ScrollUp(lines int) {
	if lines <= 0 {
		return
	}
	v.IsSticky = false
	v.ScrollOffset -= lines
	v.constrainScroll()
}

// ScrollDown scrolls down by the specified number of lines.
func (v *ViewportState) ScrollDown(lines int) {
	if lines <= 0 {
		return
	}
	v.ScrollOffset += lines
	v.constrainScroll()

	// Re-enable sticky if we scrolled to bottom
	if v.isAtBottom() {
		v.IsSticky = true
	}
}

// ScrollToTop scrolls to the top of content.
func (v *ViewportState) ScrollToTop() {
	v.IsSticky = false
	v.ScrollOffset = 0
}

// ScrollToBottom scrolls to the bottom of content and enables sticky mode.
func (v *ViewportState) ScrollToBottom() {
	v.IsSticky = true
	maxScroll := v.TotalHeight - v.ViewportHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	v.ScrollOffset = maxScroll
}

// PageUp scrolls up by one viewport page.
func (v *ViewportState) PageUp() {
	v.ScrollUp(v.ViewportHeight)
}

// PageDown scrolls down by one viewport page.
func (v *ViewportState) PageDown() {
	v.ScrollDown(v.ViewportHeight)
}

// constrainScroll ensures scroll offset stays within valid bounds.
func (v *ViewportState) constrainScroll() {
	maxScroll := v.TotalHeight - v.ViewportHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	if v.ScrollOffset < 0 {
		v.ScrollOffset = 0
	} else if v.ScrollOffset > maxScroll {
		v.ScrollOffset = maxScroll
	}
}

// isAtBottom returns true if viewport is scrolled to the bottom.
func (v *ViewportState) isAtBottom() bool {
	maxScroll := v.TotalHeight - v.ViewportHeight
	if maxScroll < 0 {
		return true
	}
	return v.ScrollOffset >= maxScroll
}

// VisibleRange calculates which message indices should be rendered based on
// the current scroll position. Returns (startIdx, endIdx) where endIdx is
// exclusive.
//
// The range is widened by OverscanRows above and below the literal viewport,
// then capped by MaxMounted so a frame never materializes an unbounded number
// of messages — both behaviours mirror Claude Code's virtual scroller.
// Boundary lookups use binary search on the cumulative-height prefix sum.
func (v *ViewportState) VisibleRange(heights []int) (startIdx, endIdx int) {
	if len(heights) == 0 || v.ViewportHeight <= 0 {
		return 0, 0
	}

	// Build cumulative offset array for O(log n) position lookup.
	offsets := make([]int, len(heights)+1)
	for i := 0; i < len(heights); i++ {
		offsets[i+1] = offsets[i] + heights[i]
	}

	overscan := v.OverscanRows
	if overscan < 0 {
		overscan = 0
	}
	viewportTop := v.ScrollOffset - overscan
	if viewportTop < 0 {
		viewportTop = 0
	}
	viewportBottom := v.ScrollOffset + v.ViewportHeight + overscan

	// Binary search for the first message whose end offset crosses viewportTop.
	startIdx = sort.Search(len(heights), func(i int) bool {
		return offsets[i+1] > viewportTop
	})
	if startIdx >= len(heights) {
		return len(heights), len(heights)
	}

	// Binary search for the first message whose start offset >= viewportBottom.
	endIdx = sort.Search(len(heights), func(i int) bool {
		return offsets[i] >= viewportBottom
	})
	if endIdx < startIdx {
		endIdx = startIdx
	}

	// Cap by MaxMounted so we never materialise an unbounded list per frame.
	if v.MaxMounted > 0 && endIdx-startIdx > v.MaxMounted {
		endIdx = startIdx + v.MaxMounted
	}

	return startIdx, endIdx
}

// TopSpacerHeight returns the number of blank lines to render above visible content.
func (v *ViewportState) TopSpacerHeight(heights []int, startIdx int) int {
	if startIdx <= 0 || len(heights) == 0 {
		return 0
	}

	// Sum heights of all messages before startIdx
	spacer := 0
	for i := 0; i < startIdx && i < len(heights); i++ {
		spacer += heights[i]
	}

	return spacer
}

// BottomSpacerHeight returns the number of blank lines to render below visible content.
func (v *ViewportState) BottomSpacerHeight(heights []int, endIdx int) int {
	if endIdx >= len(heights) || len(heights) == 0 {
		return 0
	}

	// Sum heights of all messages after endIdx
	spacer := 0
	for i := endIdx; i < len(heights); i++ {
		spacer += heights[i]
	}

	return spacer
}

// ScrollPercentage returns the current scroll position as a percentage (0-100).
func (v *ViewportState) ScrollPercentage() float64 {
	if v.TotalHeight <= v.ViewportHeight {
		return 100.0
	}
	maxScroll := v.TotalHeight - v.ViewportHeight
	if maxScroll <= 0 {
		return 100.0
	}
	return float64(v.ScrollOffset) / float64(maxScroll) * 100.0
}

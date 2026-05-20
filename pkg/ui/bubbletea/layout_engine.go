package bubbletea

// LayoutMode classifies the active terminal width into four discrete
// buckets so renderers can pick a layout that fits the available space.
//
// Boundaries are chosen so that:
//   - Wide   ( >= 100 cols ) can afford generous padding and labels.
//   - Normal ( 60–99  cols ) is the default 80x24 baseline.
//   - Narrow ( 40–59  cols ) drops decorative borders, shortens labels.
//   - Minimal ( < 40  cols ) is best-effort survival mode used by tiling
//     terminals and embedded shells. All rendering must still complete
//     without panicking even at very small widths.
type LayoutMode int

const (
	LayoutWide LayoutMode = iota
	LayoutNormal
	LayoutNarrow
	LayoutMinimal
)

// LayoutEngine is a small, stateless-ish helper that the fullscreen model
// consults whenever it needs to know how tall the status bar / input panel
// should be, how wide the content column is, or which LayoutMode applies.
//
// Keeping all these calculations in one place ensures the message area,
// input panel and help line agree on the same set of constants — bugs
// where the input panel covered the last line of the message area used to
// be common when each renderer computed the heights independently.
type LayoutEngine struct {
	termWidth  int
	termHeight int
	mode       LayoutMode
}

// NewLayoutEngine constructs a layout engine with sensible defaults so
// pre-WindowSizeMsg renders don't divide by zero.
func NewLayoutEngine() *LayoutEngine {
	l := &LayoutEngine{termWidth: 80, termHeight: 24}
	l.recompute()
	return l
}

// Update records the current terminal dimensions and recomputes the
// derived layout mode.
func (l *LayoutEngine) Update(width, height int) {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	l.termWidth = width
	l.termHeight = height
	l.recompute()
}

func (l *LayoutEngine) recompute() {
	switch {
	case l.termWidth >= 100:
		l.mode = LayoutWide
	case l.termWidth >= 60:
		l.mode = LayoutNormal
	case l.termWidth >= 40:
		l.mode = LayoutNarrow
	default:
		l.mode = LayoutMinimal
	}
}

// Mode returns the classified layout mode.
func (l *LayoutEngine) Mode() LayoutMode { return l.mode }

// TermWidth returns the last terminal width seen via Update.
func (l *LayoutEngine) TermWidth() int { return l.termWidth }

// TermHeight returns the last terminal height seen via Update.
func (l *LayoutEngine) TermHeight() int { return l.termHeight }

// StatusBarHeight is fixed at two rows so the message area never has
// to react to a status bar growing or shrinking — the previous design
// stacked Token/Thinking/Swarm lines which made the layout jitter.
func (l *LayoutEngine) StatusBarHeight() int { return 2 }

// HelpHeight is also fixed at one row. Narrow terminals receive a
// shortened help string via the renderer's responsive fallback chain.
func (l *LayoutEngine) HelpHeight() int { return 1 }

// InputHeight reserves space for the textarea plus its border. The
// textarea itself owns minInputHeight rows; we add 2 for the rounded
// border (top + bottom).
func (l *LayoutEngine) InputHeight() int {
	switch l.mode {
	case LayoutMinimal:
		// No border on minimal screens to keep more space for messages.
		return minInputHeight
	default:
		return minInputHeight + 2
	}
}

// MessageAreaHeight returns the number of rows available for the
// scrolling message area. It can return 0 when the terminal is very
// short; callers should treat 0 or negative as "skip rendering".
func (l *LayoutEngine) MessageAreaHeight() int {
	h := l.termHeight - l.StatusBarHeight() - l.InputHeight() - l.HelpHeight()
	if h < 0 {
		return 0
	}
	return h
}

// ContentWidth returns the width available for the inner content of a
// message bubble. It accounts for left/right margins and the vertical
// bar that prefixes each bubble.
func (l *LayoutEngine) ContentWidth() int {
	var w int
	switch l.mode {
	case LayoutMinimal:
		// No outer margin, no left bar — every column counts.
		w = l.termWidth - 2
	case LayoutNarrow:
		w = l.termWidth - 3
	default:
		w = l.termWidth - 5
	}
	if w < 10 {
		w = 10
	}
	return w
}

// InputInnerWidth returns the width the textarea should occupy inside
// the floating input panel after subtracting border + padding.
func (l *LayoutEngine) InputInnerWidth() int {
	var w int
	switch l.mode {
	case LayoutMinimal:
		// No panel border or margin on minimal screens.
		w = l.termWidth - 2
	case LayoutNarrow:
		// Border (2) + margin (2) + textarea padding (2).
		w = l.termWidth - 6
	default:
		w = l.termWidth - 4 - 2 - 2
	}
	if w < 10 {
		w = 10
	}
	return w
}

// ShouldUseBorder reports whether the floating input panel should draw
// a border. Minimal layouts skip the border to save rows.
func (l *LayoutEngine) ShouldUseBorder() bool {
	return l.mode != LayoutMinimal
}

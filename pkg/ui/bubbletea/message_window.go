package bubbletea

import (
	"fmt"
	"strings"
)

// recordMessage appends a message to the shared MessageStore.
//
// `content` is the *raw* content (i.e. what would be passed to
// formatLine), not the styled scrollback line. Renderers downstream are
// responsible for applying lipgloss styling so cached Rendered output
// can be invalidated cleanly on resize / theme change.
func (m *Model) recordMessage(role, content string) *FormattedMessage {
	if m.messages == nil {
		return nil
	}
	return m.messages.Add(role, content)
}

// renderedMessageLines returns the terminal lines for msg, using the
// cached value in msg.Rendered when it is still valid for the current
// terminal width. On a cache miss the rendered output is computed from
// formatLine + wrapFormattedLineForRole and stored via msg.SetRendered so
// that msg.Height is always consistent with the rendered content. Subsequent
// View() ticks reuse the cache. It is invalidated by msg.InvalidateCache()
// which is called on every WindowSizeMsg (via MessageStore.InvalidateCache)
// and whenever streaming content changes.
//
// The wrap width is role-dependent: roles whose formatLine output includes an
// emoji prefix reserve formattedLinePrefixReserve columns; roles without a
// prefix (assistant_stream) use the full terminal width.
func (m *Model) renderedMessageLines(msg *FormattedMessage) []string {
	if msg.Rendered == "" {
		styled := formatLine(msg.Role, msg.Content)
		msg.SetRendered(m.wrapFormattedLineForRole(msg.Role, styled))
	}
	return strings.Split(msg.Rendered, "\n")
}

// messageWindowLineCount returns the total number of rendered terminal
// lines across every message currently in the store. It reuses the cached
// per-message rendered output (computed lazily via renderedMessageLines)
// so the calculation is cheap when callers have already painted the
// message window.
func (m *Model) messageWindowLineCount() int {
	if m == nil || m.messages == nil {
		return 0
	}
	total := 0
	for i := 0; i < m.messages.Len(); i++ {
		total += len(m.renderedMessageLines(m.messages.Get(i)))
	}
	return total
}

// clampMessageScrollOffset clamps m.messageScrollOffsetLines so it stays
// within [0, max(0, totalLines - viewportLines)]. Callers invoke this on
// terminal resize and after the message store changes so the offset never
// references rows that no longer exist.
func (m *Model) clampMessageScrollOffset(viewportLines int) {
	if m == nil {
		return
	}
	if viewportLines <= 0 {
		m.messageScrollOffsetLines = 0
		return
	}
	max := m.messageWindowLineCount() - viewportLines
	if max < 0 {
		max = 0
	}
	if m.messageScrollOffsetLines < 0 {
		m.messageScrollOffsetLines = 0
	}
	if m.messageScrollOffsetLines > max {
		m.messageScrollOffsetLines = max
	}
}

// renderMessageWindow renders messages from the store into a string that
// fits within maxLines terminal rows.
//
// The window is conceptually anchored to the bottom of the rendered
// stream when m.messageScrollOffsetLines == 0. A positive offset shifts
// the visible slice upward by that many rendered lines so PgUp /
// MouseWheelUp let the user inspect earlier conversation history.
//
// When the topmost visible row falls inside a message (i.e. some rows of
// that message have been elided above the window) the first visible row
// is replaced with `"... N 行已截断"` so the user sees an explicit marker
// rather than silently missing context.
//
// An empty store, a non-positive maxLines, or a nil receiver all
// produce an empty string so callers can substitute the result directly
// into a Bubble Tea View() builder.
func (m *Model) renderMessageWindow(maxLines int) string {
	if m == nil || m.messages == nil || m.messages.Len() == 0 || maxLines <= 0 {
		return ""
	}

	// Render every message once and gather the rendered rows in order.
	allRows := make([]string, 0, m.messages.Len()*2)
	// firstRowOfMessage[i] = absolute index in allRows of the first row
	// of message i. Used to detect when the visible window starts in the
	// middle of a message so we can emit the truncation marker.
	firstRowOfMessage := make([]int, 0, m.messages.Len())
	for i := 0; i < m.messages.Len(); i++ {
		firstRowOfMessage = append(firstRowOfMessage, len(allRows))
		rows := m.renderedMessageLines(m.messages.Get(i))
		allRows = append(allRows, rows...)
	}
	total := len(allRows)

	offset := m.messageScrollOffsetLines
	if offset < 0 {
		offset = 0
	}
	// Clamp offset to the maximum scroll-up amount.
	maxOff := total - maxLines
	if maxOff < 0 {
		maxOff = 0
	}
	if offset > maxOff {
		offset = maxOff
	}

	end := total - offset
	if end < 0 {
		end = 0
	}
	if end > total {
		end = total
	}
	start := end - maxLines
	if start < 0 {
		start = 0
	}

	visible := append([]string(nil), allRows[start:end]...)
	if len(visible) == 0 {
		return ""
	}

	// If the topmost visible row falls within a message (not at its
	// first row), prepend a "... N 行已截断" marker so the user notices
	// content was elided from the top of the window.
	for i := len(firstRowOfMessage) - 1; i >= 0; i-- {
		mStart := firstRowOfMessage[i]
		if mStart <= start {
			// Number of rows from this message that have been elided
			// above the window. Zero means the window starts cleanly
			// at a message boundary so no marker is needed.
			elided := start - mStart
			if elided > 0 {
				marker := fmt.Sprintf("... %d 行已截断", elided)
				visible[0] = marker
			}
			break
		}
	}

	return strings.Join(visible, "\n")
}

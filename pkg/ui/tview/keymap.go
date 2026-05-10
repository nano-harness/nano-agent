package tview

import (
	"fmt"
	"strings"
)

// KeyHint pairs a key binding with a short Chinese description for display
// in the status bar. Keeping this struct outside the Model lets unit tests
// compose hints without instantiating a tview application.
type KeyHint struct {
	Key  string
	Desc string
}

// DefaultKeyHints returns the set of keyboard shortcuts surfaced in the
// tview status bar. The order is preserved by FormatKeyHints.
//
// Keep this list small enough to fit on a typical 80-column terminal; the
// status bar truncates if needed.
func DefaultKeyHints() []KeyHint {
	return []KeyHint{
		{Key: "Ctrl+P", Desc: "命令"},
		{Key: "Ctrl+L", Desc: "新会话"},
		{Key: "Ctrl+Z", Desc: "取消"},
		{Key: "Tab", Desc: "切换"},
		{Key: "q", Desc: "退出"},
	}
}

// FormatKeyHints renders the hints as "Key Desc | Key Desc | ...".
func FormatKeyHints(hints []KeyHint) string {
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, fmt.Sprintf("%s %s", h.Key, h.Desc))
	}
	return strings.Join(parts, " | ")
}

// formatStatusHints is a convenience wrapper used by updateStatusBar.
func formatStatusHints() string {
	return FormatKeyHints(DefaultKeyHints())
}

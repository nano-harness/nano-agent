package bubbletea

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
)

func applyTextareaTheme(ta *textarea.Model) {
	styles := ta.Styles()
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.CursorLineNumber = lipgloss.NewStyle()
	styles.Blurred.CursorLine = lipgloss.NewStyle()
	// NOTE: lipgloss BorderStyle causes ghost artifacts in BubbleTea v2 inline renderer.
	styles.Focused.Base = lipgloss.NewStyle().
		PaddingLeft(1).
		PaddingRight(1)
	// The default textarea Prompt is "┃ " (a thick vertical bar) and the
	// default focused Prompt foreground inherits the accent color, which
	// in milktea mode renders as a stray blue/black dot in the upper-left
	// corner before any text is typed. We disable the prompt entirely and
	// reset its style so the input area starts visually empty.
	styles.Focused.Prompt = lipgloss.NewStyle()
	styles.Blurred.Prompt = lipgloss.NewStyle()
	ta.Prompt = ""
	ta.SetStyles(styles)
}

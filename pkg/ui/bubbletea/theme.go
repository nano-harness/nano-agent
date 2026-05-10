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
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(colorInfoTitle))
	ta.SetStyles(styles)
}

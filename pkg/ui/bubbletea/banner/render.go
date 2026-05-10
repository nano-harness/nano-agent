package banner

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// RenderFrame renders a frame as a multi-line colored string ready for println.
// When colorize=false (NO_COLOR / non-TTY), returns raw Content.
func RenderFrame(f Frame, theme Theme, colorize bool) string {
	if !colorize {
		return f.Content
	}
	var out strings.Builder
	lines := strings.Split(f.Content, "\n")
	for row, line := range lines {
		// Segment adjacent same-color characters to reduce ANSI writes
		var segText strings.Builder
		var segColor string
		flush := func() {
			if segText.Len() == 0 {
				return
			}
			if segColor == "" {
				out.WriteString(segText.String())
			} else {
				style := lipgloss.NewStyle().Foreground(lipgloss.Color(segColor))
				out.WriteString(style.Render(segText.String()))
			}
			segText.Reset()
		}
		for col, r := range line {
			c := theme.ColorFor(row, col, f)
			if c != segColor {
				flush()
				segColor = c
			}
			segText.WriteRune(r)
		}
		flush()
		if row < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

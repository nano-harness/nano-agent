package banner

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// LastFrameRendered returns the static last frame ready to embed in a TUI
// view (no trailing newline). When colorize is false, returns the raw ASCII
// content. Returns an empty string and the load error when frames cannot be
// loaded.
//
// An optional IconMode may be provided to swap the default atom icon for a
// teacup / bubble-tea cup glyph, matching the icon rendered by Play() during
// the startup animation so the settled last frame remains visually
// consistent with the played animation.
//
// This is the preferred way to surface the product banner inside Bubble Tea
// views: callers can play the animation via Play and then keep the resulting
// last frame on screen so the TUI is never blank of product identity.
func LastFrameRendered(theme Theme, colorize bool, icon ...IconMode) (string, error) {
	frames, err := LoadFrames()
	if err != nil {
		return "", err
	}
	if len(frames) == 0 {
		return "", nil
	}
	if theme == nil {
		theme = DefaultTheme
	}
	mode := IconDefault
	if len(icon) > 0 {
		mode = icon[0]
	}
	applyIconMode(frames, mode)
	return RenderFrame(frames[len(frames)-1], theme, colorize), nil
}

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

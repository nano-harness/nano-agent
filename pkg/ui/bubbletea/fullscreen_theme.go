package bubbletea

import (
	"charm.land/lipgloss/v2"
)

// Fullscreen-specific color palette (dark theme, high contrast).
// These constants are only used by the fullscreen (--milktea) model and
// MUST NOT leak into the inline model's styling.
const (
	fsColorStatusBg    = "236" // dark gray status bar background
	fsColorStatusFg    = "250" // light gray status bar foreground
	fsColorAccent      = "75"  // soft blue accent (status indicator)
	fsColorUserBorder  = "75"  // user message border (soft blue)
	fsColorAIBorder    = "114" // assistant message border (green)
	fsColorToolBorder  = "245" // tool message border (gray)
	fsColorThinkBorder = "179" // thinking message border (warm gold)
	fsColorErrBorder   = "203" // error message border (soft coral red)
	fsColorSysBorder   = "179" // system message border (warm gold)
	fsColorInputBorder = "75"  // input panel border (soft blue)
	fsColorHelpText    = "242" // help text (dim gray)
	fsColorEmptyHint   = "240" // empty state hint (very dim gray)
	fsColorRoleLabel   = "252" // bold role label foreground
)

// fsMessageContentWidth returns the width available for the inner content of a
// message bubble given the terminal width. It reserves space for the left
// vertical bar, label padding and outer margins.
func fsMessageContentWidth(termWidth int) int {
	// 2 cols outer margin (left+right), 2 cols inner padding (left+right),
	// 1 col left bar.
	w := termWidth - 5
	if w < 10 {
		w = 10
	}
	return w
}

// fsMessageBubbleStyle returns a lipgloss style that draws a colored left
// vertical bar with the given border color and 1 column of left padding for
// the inner content. Width is the terminal width (the outer width of the
// bubble); the inner content width is computed from it.
func fsMessageBubbleStyle(borderColor string, termWidth int) lipgloss.Style {
	border := lipgloss.Border{Left: "│"}
	contentWidth := fsMessageContentWidth(termWidth)
	return lipgloss.NewStyle().
		Border(border, false, false, false, true).
		BorderForeground(lipgloss.Color(borderColor)).
		PaddingLeft(1).
		PaddingRight(1).
		MarginLeft(1).
		Width(contentWidth + 2) // +2 for left/right padding
}

// fsRoleLabelStyle returns a bold style for the role label that prefixes the
// message content. The color matches the bubble's accent color.
func fsRoleLabelStyle(color string) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
}

// fsStatusBarStyle returns the status bar style spanning the full terminal
// width with the dark accent background.
func fsStatusBarStyle(termWidth int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorStatusFg)).
		Background(lipgloss.Color(fsColorStatusBg)).
		Width(termWidth).
		Padding(0, 1)
}

// fsInputPanelStyle returns the style for the floating input panel. It uses a
// rounded border in the accent color and stretches across the available width.
// The width argument is the inner width that the textarea should occupy.
func fsInputPanelStyle(termWidth int) lipgloss.Style {
	// Reserve 2 cols for the rounded border (left+right) and 2 cols for outer
	// margin so the panel "floats" away from the screen edges.
	inner := termWidth - 4
	if inner < 10 {
		inner = 10
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(fsColorInputBorder)).
		MarginLeft(1).
		MarginRight(1).
		Width(inner)
}

// fsHelpStyle returns the dim style for the bottom help hint.
func fsHelpStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorHelpText)).
		PaddingLeft(2)
}

// fsEmptyHintStyle returns the style for the empty-state hint shown when no
// messages have been exchanged yet.
func fsEmptyHintStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorEmptyHint)).
		Padding(2, 2)
}

// fsBorderColorForRole maps a message role to its bubble border color.
func fsBorderColorForRole(role string) string {
	switch role {
	case "user":
		return fsColorUserBorder
	case "assistant", "assistant_stream":
		return fsColorAIBorder
	case "tool":
		return fsColorToolBorder
	case "thinking":
		return fsColorThinkBorder
	case "error":
		return fsColorErrBorder
	case "system":
		return fsColorSysBorder
	default:
		return fsColorToolBorder
	}
}

// fsLabelForRole returns the human-friendly label rendered at the top of each
// message bubble.
func fsLabelForRole(role string) string {
	switch role {
	case "user":
		return "You"
	case "assistant", "assistant_stream":
		return "Assistant"
	case "tool":
		return "🛠 Tool"
	case "thinking":
		return "Thinking"
	case "error":
		return "Error"
	case "system":
		return "System"
	default:
		return role
	}
}

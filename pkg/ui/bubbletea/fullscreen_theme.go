package bubbletea

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Fullscreen-specific color palette (dark theme, high contrast).
// Role colors are derived from palette.go so inline (--tea) and fullscreen
// (--milktea) use the same semantic token for the same message role. Only
// border/chrome colors that exist exclusively in the fullscreen layout remain
// as local constants here.
const (
	fsColorStatusFg    = "252"            // light gray status bar foreground (WCAG AA tuning)
	fsColorStatusDim   = "245"            // muted foreground for right-side stats
	fsColorStatusRule  = "240"            // subtle horizontal rule below status bar
	fsColorAccent      = "75"             // soft blue accent (status indicator)
	fsColorUserBorder  = paletteUser      // user message border — shared with inline colorUser
	fsColorAIBorder    = paletteAssistant // assistant message border — shared with inline colorAssistant
	fsColorToolBorder  = paletteTool      // tool message border — shared with inline colorTool
	fsColorThinkBorder = paletteSystem    // thinking message border — shared with inline colorSystem
	fsColorErrBorder   = paletteError     // error message border — shared with inline colorError
	fsColorSysBorder   = paletteSystem    // system message border
	fsColorInputBorder = "75"             // input panel border (soft blue)
	fsColorHelpText    = "249"            // help text (slightly brighter for legibility)
	fsColorEmptyHint   = "246"            // empty state hint (#949494)
	fsColorRoleLabel   = "252"            // bold role label foreground
	fsColorConfirmBg   = "236"            // confirmation dialog title background
	fsColorButtonFg    = "15"             // confirmation dialog selected button foreground
	fsColorYesButtonBg = "28"             // confirmation confirm button background
	fsColorNoButtonBg  = "124"            // confirmation cancel button background
	fsColorAlwaysBg    = "33"             // confirmation always-allow button background
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
// vertical bar with the given border color and 1 column of left/right padding
// for the inner content. `termWidth` is the terminal width; the inner content
// width is computed from it.
//
// Note on lipgloss Width semantics: when a Border and Padding are set,
// lipgloss treats `Width` as the **content width** — it does NOT include
// the border or padding. We pass `contentWidth + 2` here because
// `fsMessageContentWidth` already subtracts 5 columns (1 bar + 2 padding +
// 2 outer margin), so we add the 2 padding columns back so the styled
// block's content area matches `contentWidth`. The left bar (1 col) and
// margin (1 col) are added on top by Border/MarginLeft automatically.
func fsMessageBubbleStyle(borderColor string, termWidth int) lipgloss.Style {
	border := lipgloss.Border{Left: "│"}
	contentWidth := fsMessageContentWidth(termWidth)
	return lipgloss.NewStyle().
		Border(border, false, false, false, true).
		BorderForeground(lipgloss.Color(borderColor)).
		PaddingLeft(1).
		PaddingRight(1).
		MarginLeft(1).
		Width(contentWidth + 2) // +2 reintroduces the L/R padding columns
}

// fsRoleLabelStyle returns a bold style for the role label that prefixes the
// message content. The color matches the bubble's accent color.
func fsRoleLabelStyle(color string) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Bold(true)
}

// fsStatusBarStyle returns the status bar style spanning the full terminal
// width without forcing a background, so it blends with the terminal theme.
func fsStatusBarStyle(termWidth int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorStatusFg)).
		Width(termWidth)
}

func fsStatusBarDimStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorStatusDim))
}

func fsStatusBarRule(termWidth int, cap TermCapability) string {
	if termWidth < 0 {
		termWidth = 0
	}
	ch := "─"
	if !cap.SupportsBoxDraw {
		ch = "-"
	}
	rule := strings.Repeat(ch, termWidth)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorStatusRule)).
		Width(termWidth).
		Render(rule)
}

// fsInputPanelStyle returns the style for the floating input panel. It uses a
// normal border in the accent color. The `innerWidth` argument is the width
// the textarea should occupy inside the panel — callers should pass
// LayoutEngine.InputInnerWidth() so the panel agrees with the layout
// engine's calculations rather than duplicating the math.
func fsInputPanelStyle(innerWidth int) lipgloss.Style {
	inner := innerWidth
	if inner < 10 {
		inner = 10
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
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

// fsThinkingBlockStyle returns the framed style used to render an inline
// thinking block in the message stream.
func fsThinkingBlockStyle(termWidth int) lipgloss.Style {
	w := fsMessageContentWidth(termWidth)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorThinkBorder)).
		PaddingLeft(2).
		Width(w + 2)
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

func fsConfirmationTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fsColorSysBorder)).
		Background(lipgloss.Color(fsColorConfirmBg)).
		Padding(0, 1)
}

func fsConfirmationMessageStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorRoleLabel)).
		Bold(true)
}

func fsConfirmationInfoStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorHelpText)).
		Italic(true)
}

func fsConfirmationButtonStyle(selected bool, kind string) lipgloss.Style {
	style := lipgloss.NewStyle().
		Padding(0, 2)
	if !selected {
		return style.
			Foreground(lipgloss.Color(fsColorHelpText)).
			Background(lipgloss.Color(fsColorConfirmBg))
	}
	bg := fsColorYesButtonBg
	switch kind {
	case "no":
		bg = fsColorNoButtonBg
	case "always":
		bg = fsColorAlwaysBg
	}
	return style.
		Foreground(lipgloss.Color(fsColorButtonFg)).
		Background(lipgloss.Color(bg)).
		Bold(true)
}

// fsLabelForRole returns the human-friendly label rendered at the top of each
// message bubble. Labels are deliberately ASCII so they render correctly
// regardless of terminal font support. Emoji decorations are added by the
// caller using SafeChar / TermCapability when appropriate.
func fsLabelForRole(role string) string {
	switch role {
	case "user":
		return "You"
	case "assistant", "assistant_stream":
		return "Assistant"
	case "tool":
		return "Tool"
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

// fsHeaderLabel prepends a role-appropriate emoji prefix (with ASCII
// fallback selected via the supplied TermCapability) to the base label
// returned by fsLabelForRole. Roles without a registered prefix get
// their plain label.
func fsHeaderLabel(role, label string, cap TermCapability) string {
	var key string
	switch role {
	case "tool":
		key = "tool_prefix"
	case "error":
		key = "error_prefix"
	default:
		return label
	}
	if prefix := SafeChar(key, cap); prefix != "" {
		return prefix + label
	}
	return label
}

// fsWelcomeHintStyle returns the style for the centered hint shown on the
// welcome page underneath the product banner.
func fsWelcomeHintStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorRoleLabel)).
		Bold(true).
		Align(lipgloss.Center)
}

// fsWelcomeTipStyle returns the style for the dim secondary tip on the
// welcome page.
func fsWelcomeTipStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorHelpText)).
		Italic(true).
		Align(lipgloss.Center)
}

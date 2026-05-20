package bubbletea

// palette.go – single source of truth for all semantic color tokens used by
// both the inline (--tea) and fullscreen (--milktea) models. Both model
// packages import from here; no raw ANSI color literals should appear in
// model.go or fullscreen_theme.go unless they are local derivations (e.g.
// button backgrounds that exist only in one mode).
//
// Colors are expressed as 256-color ANSI terminal codes (strings) compatible
// with charm.land/lipgloss ColorProfile256.

// Semantic role colors — shared across both TUI modes.
const (
	// paletteAssistant is the canonical AI-response foreground / border color.
	// Both inline (text foreground) and fullscreen (left-bar border) use this.
	// Was: inline "115" (softer sage), fullscreen "151" (WCAG ~9.2:1 sage);
	// unified to "151" for slightly better legibility without sacrificing style.
	paletteAssistant = "151" // sage green  – AI responses

	// paletteUser is shared between both modes (unchanged, already aligned).
	paletteUser = "75" // soft blue   – user messages

	// paletteTool is the neutral gray for tool / secondary information.
	paletteTool = "249" // light gray  – tool output

	// paletteError is the soft coral-red for error messages.
	paletteError = "203" // coral red   – errors

	// paletteSystem is the warm gold for system / thinking messages.
	paletteSystem = "179" // warm gold   – system / thinking

	// paletteMuted is the medium gray used for borders, help text, separators.
	paletteMuted = "245"

	// paletteDetail is the light gray for detail / description text.
	paletteDetail = "252"
)

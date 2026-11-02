package tview

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Styles represents the color scheme and styling for the TUI
type Styles struct {
	// Color palette
	primary    tcell.Color
	secondary  tcell.Color
	success    tcell.Color
	warning    tcell.Color
	error      tcell.Color
	muted      tcell.Color
	background tcell.Color
	foreground tcell.Color

	// UI component colors
	headerBg      tcell.Color
	headerFg      tcell.Color
	statusBarBg   tcell.Color
	statusBarFg   tcell.Color
	inputBoxBg    tcell.Color
	inputBoxFg    tcell.Color
	helpBoxBg     tcell.Color
	helpBoxFg     tcell.Color
	userMessageBg tcell.Color
	userMessageFg tcell.Color
	assistantBg   tcell.Color
	assistantFg   tcell.Color
	systemBg      tcell.Color
	systemFg      tcell.Color
	loadingBg     tcell.Color
	loadingFg     tcell.Color
	emptyStateBg  tcell.Color
	emptyStateFg  tcell.Color
}

// Color constants
const (
	ColorPrimary    = tcell.ColorBlue
	ColorSecondary  = tcell.ColorTeal
	ColorSuccess    = tcell.ColorGreen
	ColorWarning    = tcell.ColorYellow
	ColorError      = tcell.ColorRed
	ColorMuted      = tcell.ColorGray
	ColorBackground = tcell.ColorBlack
	ColorForeground = tcell.ColorWhite
)

// newStyles creates a new Styles instance with default colors
func newStyles() *Styles {
	return &Styles{
		// Color palette
		primary:    ColorPrimary,
		secondary:  ColorSecondary,
		success:    ColorSuccess,
		warning:    ColorWarning,
		error:      ColorError,
		muted:      ColorMuted,
		background: ColorBackground,
		foreground: ColorForeground,

		// UI component colors
		headerBg:      ColorPrimary,
		headerFg:      ColorForeground,
		statusBarBg:   ColorMuted,
		statusBarFg:   ColorForeground,
		inputBoxBg:    ColorBackground,
		inputBoxFg:    ColorForeground,
		helpBoxBg:     ColorBackground,
		helpBoxFg:     ColorForeground,
		userMessageBg: ColorBackground,
		userMessageFg: ColorPrimary,
		assistantBg:   ColorBackground,
		assistantFg:   ColorSuccess,
		systemBg:      ColorBackground,
		systemFg:      ColorWarning,
		loadingBg:     ColorBackground,
		loadingFg:     ColorWarning,
		emptyStateBg:  ColorBackground,
		emptyStateFg:  ColorMuted,
	}
}

// GetPrimary returns the primary color
func (s *Styles) GetPrimary() tcell.Color {
	return s.primary
}

// GetSecondary returns the secondary color
func (s *Styles) GetSecondary() tcell.Color {
	return s.secondary
}

// GetSuccess returns the success color
func (s *Styles) GetSuccess() tcell.Color {
	return s.success
}

// GetWarning returns the warning color
func (s *Styles) GetWarning() tcell.Color {
	return s.warning
}

// GetError returns the error color
func (s *Styles) GetError() tcell.Color {
	return s.error
}

// GetMuted returns the muted color
func (s *Styles) GetMuted() tcell.Color {
	return s.muted
}

// GetBackground returns the background color
func (s *Styles) GetBackground() tcell.Color {
	return s.background
}

// GetForeground returns the foreground color
func (s *Styles) GetForeground() tcell.Color {
	return s.foreground
}

// GetHeaderColors returns header background and foreground colors
func (s *Styles) GetHeaderColors() (tcell.Color, tcell.Color) {
	return s.headerBg, s.headerFg
}

// GetStatusBarColors returns status bar background and foreground colors
func (s *Styles) GetStatusBarColors() (tcell.Color, tcell.Color) {
	return s.statusBarBg, s.statusBarFg
}

// GetInputBoxColors returns input box background and foreground colors
func (s *Styles) GetInputBoxColors() (tcell.Color, tcell.Color) {
	return s.inputBoxBg, s.inputBoxFg
}

// GetHelpBoxColors returns help box background and foreground colors
func (s *Styles) GetHelpBoxColors() (tcell.Color, tcell.Color) {
	return s.helpBoxBg, s.helpBoxFg
}

// GetUserMessageColors returns user message background and foreground colors
func (s *Styles) GetUserMessageColors() (tcell.Color, tcell.Color) {
	return s.userMessageBg, s.userMessageFg
}

// GetAssistantColors returns assistant message background and foreground colors
func (s *Styles) GetAssistantColors() (tcell.Color, tcell.Color) {
	return s.assistantBg, s.assistantFg
}

// GetSystemColors returns system message background and foreground colors
func (s *Styles) GetSystemColors() (tcell.Color, tcell.Color) {
	return s.systemBg, s.systemFg
}

// GetLoadingColors returns loading indicator background and foreground colors
func (s *Styles) GetLoadingColors() (tcell.Color, tcell.Color) {
	return s.loadingBg, s.loadingFg
}

// GetEmptyStateColors returns empty state background and foreground colors
func (s *Styles) GetEmptyStateColors() (tcell.Color, tcell.Color) {
	return s.emptyStateBg, s.emptyStateFg
}

// ApplyTheme applies a predefined theme to the styles
func (s *Styles) ApplyTheme(theme string) {
	switch theme {
	case "dark":
		s.applyDarkTheme()
	case "light":
		s.applyLightTheme()
	case "blue":
		s.applyBlueTheme()
	default:
		// Keep current theme
	}
}

// applyDarkTheme applies a dark color scheme
func (s *Styles) applyDarkTheme() {
	s.background = tcell.ColorBlack
	s.foreground = tcell.ColorWhite
	s.primary = tcell.ColorBlue
	s.secondary = tcell.ColorTeal
	s.success = tcell.ColorGreen
	s.warning = tcell.ColorYellow
	s.error = tcell.ColorRed
	s.muted = tcell.ColorGray

	// Update component colors
	s.headerBg = s.primary
	s.headerFg = s.foreground
	s.statusBarBg = s.muted
	s.statusBarFg = s.foreground
	s.inputBoxBg = s.background
	s.inputBoxFg = s.foreground
	s.helpBoxBg = s.background
	s.helpBoxFg = s.foreground
	s.userMessageBg = s.background
	s.userMessageFg = s.primary
	s.assistantBg = s.background
	s.assistantFg = s.success
	s.systemBg = s.background
	s.systemFg = s.warning
	s.loadingBg = s.background
	s.loadingFg = s.warning
	s.emptyStateBg = s.background
	s.emptyStateFg = s.muted
}

// applyLightTheme applies a light color scheme
func (s *Styles) applyLightTheme() {
	s.background = tcell.ColorWhite
	s.foreground = tcell.ColorBlack
	s.primary = tcell.ColorBlue
	s.secondary = tcell.ColorDarkBlue
	s.success = tcell.ColorGreen
	s.warning = tcell.ColorOrange
	s.error = tcell.ColorRed
	s.muted = tcell.ColorGray

	// Update component colors
	s.headerBg = s.primary
	s.headerFg = s.background
	s.statusBarBg = s.muted
	s.statusBarFg = s.background
	s.inputBoxBg = s.background
	s.inputBoxFg = s.foreground
	s.helpBoxBg = s.background
	s.helpBoxFg = s.foreground
	s.userMessageBg = s.background
	s.userMessageFg = s.primary
	s.assistantBg = s.background
	s.assistantFg = s.success
	s.systemBg = s.background
	s.systemFg = s.warning
	s.loadingBg = s.background
	s.loadingFg = s.warning
	s.emptyStateBg = s.background
	s.emptyStateFg = s.muted
}

// applyBlueTheme applies a blue-focused color scheme
func (s *Styles) applyBlueTheme() {
	s.background = tcell.ColorNavy
	s.foreground = tcell.ColorWhite
	s.primary = tcell.ColorLightBlue
	s.secondary = tcell.ColorTeal
	s.success = tcell.ColorLime
	s.warning = tcell.ColorYellow
	s.error = tcell.ColorMaroon
	s.muted = tcell.ColorLightGray

	// Update component colors
	s.headerBg = s.primary
	s.headerFg = s.background
	s.statusBarBg = s.muted
	s.statusBarFg = s.background
	s.inputBoxBg = s.background
	s.inputBoxFg = s.foreground
	s.helpBoxBg = s.background
	s.helpBoxFg = s.foreground
	s.userMessageBg = s.background
	s.userMessageFg = s.primary
	s.assistantBg = s.background
	s.assistantFg = s.success
	s.systemBg = s.background
	s.systemFg = s.warning
	s.loadingBg = s.background
	s.loadingFg = s.warning
	s.emptyStateBg = s.background
	s.emptyStateFg = s.muted
}

// ConfigureTViewStyles configures tview component styles
func (s *Styles) ConfigureTViewStyles() {
	// Override tview global styles to ensure consistent background (avoid green focus background)
	tview.Styles.PrimitiveBackgroundColor = s.background
	tview.Styles.ContrastBackgroundColor = s.background
	tview.Styles.MoreContrastBackgroundColor = s.background
	tview.Styles.BorderColor = s.muted
	tview.Styles.TitleColor = s.muted
	tview.Styles.PrimaryTextColor = s.foreground
	tview.Styles.SecondaryTextColor = s.muted
	tview.Styles.TertiaryTextColor = s.foreground
	// Inverse/contrast text colors for selections/focus states
	tview.Styles.InverseTextColor = s.foreground
	tview.Styles.ContrastSecondaryTextColor = s.foreground
}

// GetColorTag returns a tview color tag for the given color type
func (s *Styles) GetColorTag(colorType string) string {
	switch colorType {
	case "primary":
		return "[#5fafff]" // soft blue (≈ ANSI 75)
	case "secondary":
		return "[#5fafaf]" // soft teal (≈ ANSI 73)
	case "success":
		return "[#87d787]" // soft green (≈ ANSI 114)
	case "warning":
		return "[#d7af5f]" // warm gold (≈ ANSI 179)
	case "error":
		return "[#ff5f5f]" // soft coral red (≈ ANSI 203)
	case "muted":
		return "[#8a8a8a]" // medium gray (≈ ANSI 245)
	case "white":
		return "[#d0d0d0]" // near-white (≈ ANSI 252)
	case "black":
		return "[black]"
	default:
		return "[#d0d0d0]"
	}
}

// GetResetTag returns the tview reset color tag
func (s *Styles) GetResetTag() string {
	return "[-]"
}

package bubbletea

import (
	"os"
	"strings"
)

// TermCapability describes which classes of characters the active terminal
// is expected to render correctly. Rendering code consults a TermCapability
// before emitting emoji, Braille spinners or Box Drawing glyphs so that
// fallback ASCII characters can be used when the terminal font is unlikely
// to have the matching glyph (which historically rendered as black squares
// or tofu boxes in the milktea TUI).
type TermCapability struct {
	SupportsEmoji   bool
	SupportsBraille bool
	SupportsBoxDraw bool
	// ColorDepth is 0, 16, 256, or a value greater than 256 to indicate
	// truecolor support. The current renderer only branches on whether
	// any color is supported so callers can treat any non-zero value
	// equivalently for now.
	ColorDepth int
}

// safeCharMap maps a symbolic key to a [rich, fallback] pair. The rich form
// uses emoji / Braille / Box Drawing characters; the fallback form uses
// plain ASCII that is guaranteed to render in any monospaced font.
//
// Centralising the map keeps the inline and fullscreen renderers in sync
// and lets callers add new keys without touching detection logic.
var safeCharMap = map[string][2]string{ //nolint:gochecknoglobals
	"user_prefix":      {"👤 ", "[U] "},
	"assistant_prefix": {"🤖 ", "[A] "},
	"thinking_prefix":  {"🧠 ", "[T] "},
	"tool_prefix":      {"🔧 ", "[tool] "},
	"error_prefix":     {"❌ ", "[!] "},
	"system_prefix":    {"⚙ ", "[*] "},
	"success":          {"✅", "[ok]"},
	"bullet":           {"●", "*"},
	"folder_prefix":    {"📁 ", "F:"},
	"globe_prefix":     {"🌐 ", "A:"},
}

// asciiSpinnerFrames are the safe-character fallback frames used when the
// terminal cannot render Braille Patterns.
var asciiSpinnerFrames = [...]string{"|", "/", "-", "\\"} //nolint:gochecknoglobals

// SafeChar returns the appropriate character or string for the given symbolic
// key based on the terminal's capabilities. Unknown keys return an empty
// string so callers may treat missing entries as "no decoration".
func SafeChar(key string, cap TermCapability) string {
	pair, ok := safeCharMap[key]
	if !ok {
		return ""
	}
	if cap.SupportsEmoji {
		return pair[0]
	}
	return pair[1]
}

// SafeSpinnerFrame returns the spinner frame for the given index, falling
// back to ASCII rotation when the terminal cannot render Braille.
func SafeSpinnerFrame(idx int, cap TermCapability) string {
	if idx < 0 {
		idx = 0
	}
	if cap.SupportsBraille {
		return spinnerFrames[idx%len(spinnerFrames)]
	}
	return asciiSpinnerFrames[idx%len(asciiSpinnerFrames)]
}

// SafeProgressBar renders a fixed-width progress bar using ASCII-safe
// characters by default. We deliberately avoid the Box Drawing glyphs
// (`█` / `░`) here even on terminals that advertise SupportsBoxDraw,
// because those characters frequently render as solid blocks or tofu in
// the status bar of common terminal fonts (the milktea "black square"
// artifact). `NANO_TUI_ASCII=1` continues to force the safest fallback
// across the rest of the renderer for users who hit additional issues.
func SafeProgressBar(filled, width int, _ TermCapability) string {
	if width <= 0 {
		return ""
	}
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	const (
		full  = "="
		empty = "-"
	)
	return strings.Repeat(full, filled) + strings.Repeat(empty, width-filled)
}

// DetectTermCapability inspects environment variables to make a best-effort
// guess at what the active terminal can render. The detection is
// intentionally conservative: callers can rely on the rich path only when
// the relevant capability is true, and the fallback path is always safe
// to take.
//
// Heuristics:
//   - Emoji + Braille: assumed when LANG or LC_ALL is a UTF-8 locale AND
//     the terminal is not a known limited terminal (e.g. dumb / linux
//     console). Many modern terminals (xterm-256color, screen-256color,
//     iTerm.app, Apple_Terminal, Alacritty, WezTerm, gnome-terminal,
//     vscode, Windows Terminal) handle both well.
//   - Box Drawing: same conditions as above; almost every UTF-8 terminal
//     ships these glyphs.
//   - ColorDepth: 256 by default when COLORTERM/TERM looks color-capable.
func DetectTermCapability() TermCapability {
	cap := TermCapability{}

	term := strings.ToLower(os.Getenv("TERM"))
	colorterm := strings.ToLower(os.Getenv("COLORTERM"))
	termProgram := strings.ToLower(os.Getenv("TERM_PROGRAM"))

	// Check each locale variable independently. Concatenating them
	// before searching for "UTF-8" produces false positives such as
	// LANG="C" + LC_CTYPE="UTF-8" → "CUTF-8" (which incorrectly
	// matches "UTF-8"). The empty string never satisfies the check.
	utf8 := false
	for _, v := range []string{os.Getenv("LANG"), os.Getenv("LC_ALL"), os.Getenv("LC_CTYPE")} {
		up := strings.ToUpper(v)
		if strings.Contains(up, "UTF-8") || strings.Contains(up, "UTF8") {
			utf8 = true
			break
		}
	}

	// "dumb" / "linux" / empty TERM should never claim rich glyphs.
	limited := term == "" || term == "dumb" || strings.HasPrefix(term, "linux") ||
		strings.HasPrefix(term, "vt100") || strings.HasPrefix(term, "vt220")
	// NANO_TUI_ASCII=1 forces the safest fallback path; useful for
	// remote sessions where the terminal font is unknown.
	forceAscii := strings.TrimSpace(os.Getenv("NANO_TUI_ASCII")) == "1"

	if utf8 && !limited && !forceAscii {
		cap.SupportsBoxDraw = true
		cap.SupportsBraille = true
		// Emoji rendering is the most fragile of the three: many
		// fixed-width fonts have Box Drawing + Braille but lack
		// modern emoji. We accept emoji when running under a known
		// modern terminal program OR when COLORTERM=truecolor (a
		// strong signal of a recent terminal emulator).
		if termProgram != "" || colorterm == "truecolor" ||
			strings.Contains(term, "256color") || strings.Contains(term, "xterm-kitty") ||
			strings.Contains(term, "alacritty") || strings.Contains(term, "wezterm") {
			cap.SupportsEmoji = true
		}
	}

	switch {
	case colorterm == "truecolor" || colorterm == "24bit":
		cap.ColorDepth = 1 << 24
	case strings.Contains(term, "256"):
		cap.ColorDepth = 256
	case term != "" && term != "dumb":
		cap.ColorDepth = 16
	default:
		cap.ColorDepth = 0
	}

	return cap
}

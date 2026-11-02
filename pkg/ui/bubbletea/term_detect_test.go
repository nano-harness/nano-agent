package bubbletea

import (
	"os"
	"strings"
	"testing"
)

func TestSafeChar_EmojiVsFallback(t *testing.T) {
	rich := TermCapability{SupportsEmoji: true, SupportsBraille: true, SupportsBoxDraw: true}
	plain := TermCapability{}

	if got := SafeChar("bullet", rich); got != "●" {
		t.Fatalf("rich bullet = %q, want ●", got)
	}
	if got := SafeChar("bullet", plain); got != "*" {
		t.Fatalf("plain bullet = %q, want *", got)
	}
	if got := SafeChar("nope_unknown_key", rich); got != "" {
		t.Fatalf("unknown key should yield empty string, got %q", got)
	}
}

func TestSafeSpinnerFrame_BraillePresence(t *testing.T) {
	braille := TermCapability{SupportsBraille: true}
	plain := TermCapability{}

	rich := SafeSpinnerFrame(0, braille)
	if rich != spinnerFrames[0] {
		t.Fatalf("expected first Braille frame, got %q", rich)
	}
	ascii := SafeSpinnerFrame(0, plain)
	if ascii != "|" {
		t.Fatalf("expected ASCII spinner '|', got %q", ascii)
	}
	// Negative idx must not panic.
	_ = SafeSpinnerFrame(-1, plain)
}

func TestSafeProgressBar_LengthAndChars(t *testing.T) {
	rich := TermCapability{SupportsBoxDraw: true}
	plain := TermCapability{}

	// Even with rich Box Drawing support the progress bar must remain
	// ASCII-safe — the `█`/`░` glyphs were the source of the milktea
	// status-bar "black square" artifact.
	bar := SafeProgressBar(3, 10, rich)
	if l := len([]rune(bar)); l != 10 {
		t.Fatalf("expected 10 runes, got %d (%q)", l, bar)
	}
	if strings.ContainsAny(bar, "█░") {
		t.Fatalf("progress bar must not contain Box Drawing block glyphs: %q", bar)
	}
	if !strings.HasPrefix(bar, "===") || !strings.HasSuffix(bar, "-------") {
		t.Fatalf("rich bar wrong shape: %q", bar)
	}

	bar = SafeProgressBar(3, 10, plain)
	if !strings.HasPrefix(bar, "===") || !strings.HasSuffix(bar, "-------") {
		t.Fatalf("plain bar wrong shape: %q", bar)
	}
	if strings.ContainsAny(bar, "█░") {
		t.Fatalf("plain progress bar must not contain Box Drawing block glyphs: %q", bar)
	}
	if SafeProgressBar(0, 0, rich) != "" {
		t.Fatal("zero-width bar should be empty")
	}
}

func TestDetectTermCapability_ASCIIForceFlag(t *testing.T) {
	saved := map[string]string{
		"LANG":           os.Getenv("LANG"),
		"LC_ALL":         os.Getenv("LC_ALL"),
		"LC_CTYPE":       os.Getenv("LC_CTYPE"),
		"TERM":           os.Getenv("TERM"),
		"COLORTERM":      os.Getenv("COLORTERM"),
		"TERM_PROGRAM":   os.Getenv("TERM_PROGRAM"),
		"NANO_TUI_ASCII": os.Getenv("NANO_TUI_ASCII"),
	}
	t.Cleanup(func() {
		for k, v := range saved {
			_ = os.Setenv(k, v)
		}
	})

	_ = os.Setenv("LANG", "en_US.UTF-8")
	_ = os.Setenv("LC_ALL", "")
	_ = os.Setenv("LC_CTYPE", "")
	_ = os.Setenv("TERM", "xterm-256color")
	_ = os.Setenv("COLORTERM", "truecolor")
	_ = os.Setenv("TERM_PROGRAM", "iTerm.app")
	_ = os.Setenv("NANO_TUI_ASCII", "")

	cap := DetectTermCapability()
	if !cap.SupportsEmoji || !cap.SupportsBraille || !cap.SupportsBoxDraw {
		t.Fatalf("expected modern terminal to support all rich glyphs, got %+v", cap)
	}

	_ = os.Setenv("NANO_TUI_ASCII", "1")
	cap = DetectTermCapability()
	if cap.SupportsEmoji || cap.SupportsBraille || cap.SupportsBoxDraw {
		t.Fatalf("NANO_TUI_ASCII=1 should force fallback path, got %+v", cap)
	}
}

func TestDetectTermCapability_DumbAndLinux(t *testing.T) {
	saved := os.Getenv("TERM")
	savedLang := os.Getenv("LANG")
	savedAscii := os.Getenv("NANO_TUI_ASCII")
	t.Cleanup(func() {
		_ = os.Setenv("TERM", saved)
		_ = os.Setenv("LANG", savedLang)
		_ = os.Setenv("NANO_TUI_ASCII", savedAscii)
	})
	_ = os.Setenv("NANO_TUI_ASCII", "")
	_ = os.Setenv("LANG", "en_US.UTF-8")

	for _, term := range []string{"dumb", "linux", ""} {
		_ = os.Setenv("TERM", term)
		cap := DetectTermCapability()
		if cap.SupportsEmoji || cap.SupportsBraille || cap.SupportsBoxDraw {
			t.Fatalf("limited TERM=%q should not advertise rich glyphs, got %+v", term, cap)
		}
	}
}

// TestDetectTermCapability_LocaleVarsIndependent guards against the
// regression where LANG/LC_ALL/LC_CTYPE were concatenated before
// matching against "UTF-8": values like LANG="C" + LC_CTYPE="UTF-8"
// would falsely match "UTF-8" because the joined string "CUTF-8"
// contains it.
func TestDetectTermCapability_LocaleVarsIndependent(t *testing.T) {
	saved := map[string]string{
		"LANG":           os.Getenv("LANG"),
		"LC_ALL":         os.Getenv("LC_ALL"),
		"LC_CTYPE":       os.Getenv("LC_CTYPE"),
		"TERM":           os.Getenv("TERM"),
		"COLORTERM":      os.Getenv("COLORTERM"),
		"TERM_PROGRAM":   os.Getenv("TERM_PROGRAM"),
		"NANO_TUI_ASCII": os.Getenv("NANO_TUI_ASCII"),
	}
	t.Cleanup(func() {
		for k, v := range saved {
			_ = os.Setenv(k, v)
		}
	})
	_ = os.Setenv("NANO_TUI_ASCII", "")
	_ = os.Setenv("TERM", "xterm-256color")
	_ = os.Setenv("COLORTERM", "")
	_ = os.Setenv("TERM_PROGRAM", "")

	// All three locale vars empty → no UTF-8 capability.
	_ = os.Setenv("LANG", "")
	_ = os.Setenv("LC_ALL", "")
	_ = os.Setenv("LC_CTYPE", "")
	cap := DetectTermCapability()
	if cap.SupportsBraille || cap.SupportsBoxDraw {
		t.Fatalf("empty locale must not yield rich glyphs, got %+v", cap)
	}

	// Only LC_CTYPE carries the UTF-8 marker → still detected.
	_ = os.Setenv("LANG", "C")
	_ = os.Setenv("LC_ALL", "")
	_ = os.Setenv("LC_CTYPE", "en_US.UTF-8")
	cap = DetectTermCapability()
	if !cap.SupportsBraille || !cap.SupportsBoxDraw {
		t.Fatalf("LC_CTYPE alone should enable rich glyphs, got %+v", cap)
	}

	// All three are non-UTF-8 (POSIX/C) → no rich glyphs.
	_ = os.Setenv("LANG", "C")
	_ = os.Setenv("LC_ALL", "POSIX")
	_ = os.Setenv("LC_CTYPE", "C")
	cap = DetectTermCapability()
	if cap.SupportsBraille || cap.SupportsBoxDraw {
		t.Fatalf("non-UTF-8 locale must not yield rich glyphs, got %+v", cap)
	}
}

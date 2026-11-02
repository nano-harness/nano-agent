package bubtesting

import (
	"bytes"
	"io"
	"os"
	"regexp"
	stdtesting "testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ui "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// NewTeatestModel creates a Bubble Tea teatest model with a fixed terminal size.
func NewTeatestModel(t stdtesting.TB, model tea.Model, opts ...teatest.TestOption) *teatest.TestModel {
	t.Helper()
	ApplyTestTheme(t)
	allOpts := append([]teatest.TestOption{
		teatest.WithInitialTermSize(120, 30),
		teatest.WithProgramOptions(tea.WithColorProfile(colorprofile.ANSI256)),
	}, opts...)
	return teatest.NewTestModel(t, model, allOpts...)
}

// ApplyTestTheme freezes color-related environment for deterministic ANSI output.
func ApplyTestTheme(t stdtesting.TB) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("TERM", "xterm-256color")
}

// WaitForText waits for output matching pattern. ANSI escape sequences are
// stripped before matching so cell-level renderer output (which emits per-cell
// styling) still matches simple literal patterns.
func WaitForText(t stdtesting.TB, tm *teatest.TestModel, pattern string, timeout time.Duration) {
	t.Helper()
	re := regexp.MustCompile(pattern)
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return re.MatchString(ansi.Strip(string(b)))
	}, teatest.WithDuration(timeout))
}

// WaitForModelState quits the program and asserts its final model state.
func WaitForModelState(t stdtesting.TB, tm *teatest.TestModel, predicate func(*ui.Model) bool, timeout time.Duration) {
	t.Helper()
	if err := tm.Quit(); err != nil {
		t.Fatalf("quit teatest model: %v", err)
	}
	final, ok := tm.FinalModel(t, teatest.WithFinalTimeout(timeout)).(*ui.Model)
	if !ok {
		t.Fatalf("final model type %T, want *bubbletea.Model", final)
	}
	if !predicate(final) {
		t.Fatalf("final model state did not satisfy predicate")
	}
}

// OutputString returns all currently buffered teatest output.
func OutputString(t stdtesting.TB, tm *teatest.TestModel) string {
	t.Helper()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, tm.Output()); err != nil {
		t.Fatalf("read teatest output: %v", err)
	}
	return buf.String()
}

// SendKey sends one key to a teatest model.
func SendKey(tm *teatest.TestModel, key string) {
	tm.Send(keyMsg(key))
}

// SendKeys sends multiple keys to a teatest model.
func SendKeys(tm *teatest.TestModel, keys ...string) {
	for _, key := range keys {
		SendKey(tm, key)
	}
}

// InjectTokenStats sends a token statistics update.
func InjectTokenStats(tm *teatest.TestModel, used, max int) {
	tm.Send(ui.TokenStatsUpdate{TotalTokens: used, ContextUsedTokens: used, ContextWindowMax: max})
}

// InjectThinkingDelta sends thinking delta messages.
func InjectThinkingDelta(tm *teatest.TestModel, lines ...string) {
	for _, line := range lines {
		tm.Send(ui.ThinkingMsg{Title: "thinking", ReasoningDelta: line})
	}
}

// InjectStreamChunk sends assistant streaming chunks.
func InjectStreamChunk(tm *teatest.TestModel, chunks ...string) {
	for _, chunk := range chunks {
		tm.Send(ui.Message{Role: "assistant_stream", Content: chunk})
	}
}

func keyMsg(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+l":
		return tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	default:
		if len([]rune(key)) == 1 {
			r := []rune(key)[0]
			return tea.KeyPressMsg{Code: r, Text: string(r)}
		}
		return tea.KeyPressMsg{Code: 0, Text: key}
	}
}

func init() {
	_ = os.Setenv("TERM", "xterm-256color")
}

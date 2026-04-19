package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	xansi "github.com/charmbracelet/x/ansi"
)

// newTestModel returns a minimal Model suitable for unit tests.
func newTestModel(termWidth int) *Model {
	ti := textinput.New()
	ti.Placeholder = "输入您的请求..."
	ti.Focus()
	ti.Width = 50
	return &Model{
		status:    "等待输入",
		termWidth: termWidth,
		input:     ti,
	}
}

// --- truncateLines ---

func TestTruncateLines_NoWidth(t *testing.T) {
	s := "hello\nworld"
	if got := truncateLines(s, 0); got != s {
		t.Errorf("expected no change for width=0, got %q", got)
	}
}

func TestTruncateLines_FitsWithin(t *testing.T) {
	s := "hi\nbye"
	if got := truncateLines(s, 80); got != s {
		t.Errorf("expected no change when content fits, got %q", got)
	}
}

func TestTruncateLines_TruncatesLongLine(t *testing.T) {
	long := strings.Repeat("a", 100)
	s := long + "\nshort"
	got := truncateLines(s, 10)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if xansi.StringWidth(lines[0]) > 10 {
		t.Errorf("first line should be ≤10 terminal columns, got %d: %q", xansi.StringWidth(lines[0]), lines[0])
	}
	if lines[1] != "short" {
		t.Errorf("second line should be unchanged, got %q", lines[1])
	}
}

// --- buildHelpText ---

func TestBuildHelpText_WideTerminal(t *testing.T) {
	m := newTestModel(200)
	full := "Ctrl+C 退出 | Ctrl+Z 取消任务 | Ctrl+P 命令列表 | Tab 补全 | ↑↓ 历史"
	if got := m.buildHelpText(); got != full {
		t.Errorf("expected full text for wide terminal, got %q", got)
	}
}

func TestBuildHelpText_NoTermWidth(t *testing.T) {
	m := newTestModel(0)
	full := "Ctrl+C 退出 | Ctrl+Z 取消任务 | Ctrl+P 命令列表 | Tab 补全 | ↑↓ 历史"
	if got := m.buildHelpText(); got != full {
		t.Errorf("expected full text when termWidth=0, got %q", got)
	}
}

func TestBuildHelpText_NarrowFallsBack(t *testing.T) {
	// At width=1 nothing fits fully; buildHelpText must fall back to
	// xansi.Truncate. The result must fit within termWidth columns.
	m := newTestModel(1)
	got := m.buildHelpText()
	if xansi.StringWidth(got) > m.termWidth {
		t.Errorf("expected result ≤%d terminal columns, got width %d: %q", m.termWidth, xansi.StringWidth(got), got)
	}
}

func TestBuildHelpText_ShortVariant(t *testing.T) {
	short := "Ctrl+C 退出 | Ctrl+Z 取消 | Ctrl+P 命令 | Tab 补全 | ↑↓ 历史"
	full := "Ctrl+C 退出 | Ctrl+Z 取消任务 | Ctrl+P 命令列表 | Tab 补全 | ↑↓ 历史"
	// Choose a width that fits short but not full.
	width := xansi.StringWidth(short)
	if xansi.StringWidth(full) <= width {
		// If the test environment renders them the same size, skip gracefully.
		t.Skip("full and short are the same width in this environment")
	}
	m := newTestModel(width)
	got := m.buildHelpText()
	if got != short {
		t.Errorf("expected short variant at width=%d, got %q", width, got)
	}
	if xansi.StringWidth(got) > width {
		t.Errorf("short variant exceeds termWidth=%d: got width %d", width, xansi.StringWidth(got))
	}
}

func TestBuildHelpText_MinimalVariant(t *testing.T) {
	minimal := "^C退出 | ^Z取消 | ^P命令 | Tab补全"
	short := "Ctrl+C 退出 | Ctrl+Z 取消 | Ctrl+P 命令 | Tab 补全 | ↑↓ 历史"
	// Choose a width that fits minimal but not short.
	width := xansi.StringWidth(minimal)
	if xansi.StringWidth(short) <= width {
		t.Skip("short and minimal are the same width in this environment")
	}
	m := newTestModel(width)
	got := m.buildHelpText()
	if got != minimal {
		t.Errorf("expected minimal variant at width=%d, got %q", width, got)
	}
	if xansi.StringWidth(got) > width {
		t.Errorf("minimal variant exceeds termWidth=%d: got width %d", width, xansi.StringWidth(got))
	}
}

// --- renderInputSection fixed line count ---

func TestRenderInputSectionFixedLineCount_EmptyStatus(t *testing.T) {
	m := newTestModel(80)
	m.status = ""
	m.tokenStatus = ""

	var b strings.Builder
	m.renderInputSection(&b)
	output := b.String()

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d:\n%s", len(lines), output)
	}
}

func TestRenderInputSectionFixedLineCount_WithStatus(t *testing.T) {
	m := newTestModel(80)
	m.status = "处理中..."
	m.tokenStatus = "输入 1K | 输出 0 | 总计 1K"

	var b strings.Builder
	m.renderInputSection(&b)
	output := b.String()

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d:\n%s", len(lines), output)
	}
}

func TestRenderInputSectionFixedLineCount_WithTokenStatus(t *testing.T) {
	m := newTestModel(80)
	m.status = "等待输入"
	m.tokenStatus = ""

	var b strings.Builder
	m.renderInputSection(&b)
	output1 := b.String()

	m.tokenStatus = "输入 57.5K | 输出 0 | 总计 57.5K"
	b.Reset()
	m.renderInputSection(&b)
	output2 := b.String()

	count1 := strings.Count(output1, "\n")
	count2 := strings.Count(output2, "\n")
	if count1 != count2 {
		t.Errorf("line count changed when tokenStatus toggled: %d vs %d", count1, count2)
	}
}

func TestRenderInputSectionTokenPlaceholder(t *testing.T) {
	m := newTestModel(80)
	m.tokenStatus = ""

	var b strings.Builder
	m.renderInputSection(&b)

	if !strings.Contains(b.String(), "[令牌]") {
		t.Error("expected token line to be present even when tokenStatus is empty")
	}
}

func TestRenderInputSectionStatusPlaceholder(t *testing.T) {
	m := newTestModel(80)
	m.status = ""

	var b strings.Builder
	m.renderInputSection(&b)

	if !strings.Contains(b.String(), "[状态]") {
		t.Error("expected status line to be present even when status is empty")
	}
}

// TestRenderInputSection_NarrowWidth verifies that the status, token, and
// separator lines fit within termWidth columns when the terminal is very narrow
// and status/token strings are much longer than the terminal width.  This
// directly guards the regression that caused ghost input blocks in inline mode.
func TestRenderInputSection_NarrowWidth(t *testing.T) {
	const termWidth = 10
	m := newTestModel(termWidth)
	const overflowRepeatCount = 20 // any value that makes the string much longer than termWidth
	m.status = strings.Repeat("处理中...", overflowRepeatCount)
	m.tokenStatus = strings.Repeat("输入 1M | 输出 1M | ", overflowRepeatCount)

	var b strings.Builder
	m.renderInputSection(&b)
	output := b.String()

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d:\n%s", len(lines), output)
	}
	// Lines 0 (separator), 1 (status), and 2 (token) are explicitly
	// truncated by renderInputSection to termWidth.  Line 3 is the text
	// input widget (width managed separately) and line 4 is the help text
	// (handled by buildHelpText).
	for _, i := range []int{0, 1, 2} {
		w := xansi.StringWidth(lines[i])
		if w > termWidth {
			t.Errorf("line %d exceeds termWidth=%d (got width %d): %q", i+1, termWidth, w, lines[i])
		}
	}
}

func TestRenderInputSection_VeryNarrow(t *testing.T) {
	// Even at width=1 the function must not panic, must produce 5 lines,
	// and the separator/status/token lines must not exceed 1 column.
	m := newTestModel(1)
	m.status = "busy"
	m.tokenStatus = "tok"

	var b strings.Builder
	m.renderInputSection(&b)
	output := b.String()

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines at width=1, got %d:\n%s", len(lines), output)
	}
	for _, i := range []int{0, 1, 2} {
		w := xansi.StringWidth(lines[i])
		if w > 1 {
			t.Errorf("line %d exceeds termWidth=1 (got width %d): %q", i+1, w, lines[i])
		}
	}
}

// --- q key input fix tests ---

// TestQKeyDoesNotQuit verifies that pressing 'q' in normal input mode
// does not trigger quit, but instead passes the key to the text input.
func TestQKeyDoesNotQuit(t *testing.T) {
	m := newTestModel(80)
	m.input.SetValue("") // Start with empty input

	// Simulate pressing 'q' key
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	newModel, cmd := m.Update(msg)

	// Verify that it's not a quit command by trying to execute it
	// tea.Quit returns a special quitMsg
	if cmd != nil {
		result := cmd()
		if _, isQuit := result.(tea.QuitMsg); isQuit {
			t.Error("pressing 'q' should not trigger quit command")
		}
	}

	// Verify that 'q' was forwarded to the text input field
	updatedModel, ok := newModel.(*Model)
	if !ok {
		t.Fatal("model type changed unexpectedly")
	}
	if got := updatedModel.input.Value(); got != "q" {
		t.Errorf("expected input value to be %q after pressing 'q', got %q", "q", got)
	}
}

// TestCtrlCStillQuits verifies that Ctrl+C still triggers quit
// even after removing 'q' from the quit keys.
func TestCtrlCStillQuits(t *testing.T) {
	m := newTestModel(80)

	// Simulate pressing Ctrl+C
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := m.Update(msg)

	// Verify that a quit command was returned
	if cmd == nil {
		t.Error("Ctrl+C should trigger a quit command")
		return
	}

	// Execute the command and verify it's a QuitMsg
	result := cmd()
	if _, isQuit := result.(tea.QuitMsg); !isQuit {
		t.Error("Ctrl+C should return a QuitMsg")
	}
}

package bubbletea

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

// newTestModel returns a minimal Model suitable for unit tests.
func newTestModel(termWidth int) *Model {
	ta := textarea.New()
	ta.Placeholder = "输入您的请求..."
	ta.Focus()
	ta.SetWidth(50)
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.KeyMap.InsertNewline.SetEnabled(false)
	return &Model{
		status:          "等待输入",
		termWidth:       termWidth,
		input:           ta,
		historyIndex:    -1,
		activeToolCalls: make(map[string]string),
		currentPhase:    phaseIdle,
	}
}

// runTeaCmd recursively executes Bubble Tea commands so tests can observe
// side effects from batched or sequenced command chains.
func runTeaCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	switch msg := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range msg {
			runTeaCmd(t, c)
		}
	default:
		// tea.Sequence returns an unexported slice type (sequenceMsg), so tests
		// use reflection to recurse through that command chain as well.
		v := reflect.ValueOf(msg)
		if v.Kind() != reflect.Slice {
			return
		}
		for i := 0; i < v.Len(); i++ {
			c, ok := v.Index(i).Interface().(tea.Cmd)
			if !ok {
				continue
			}
			runTeaCmd(t, c)
		}
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
	full := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 新会话 | Ctrl+P 命令 | Ctrl+C 退出 | Tab 补全 | ↑↓ 历史"
	if got := m.buildHelpText(); got != full {
		t.Errorf("expected full text for wide terminal, got %q", got)
	}
}

func TestBuildHelpText_NoTermWidth(t *testing.T) {
	m := newTestModel(0)
	full := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 新会话 | Ctrl+P 命令 | Ctrl+C 退出 | Tab 补全 | ↑↓ 历史"
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
	short := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 新会话 | Ctrl+P 命令 | Tab 补全"
	full := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 新会话 | Ctrl+P 命令 | Ctrl+C 退出 | Tab 补全 | ↑↓ 历史"
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
	minimal := "Enter发送 | ^J换行 | ^R新会话"
	short := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 新会话 | Ctrl+P 命令 | Tab 补全"
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

func TestCtrlJInsertsNewline(t *testing.T) {
	m := newTestModel(80)
	m.input.SetValue("hello")

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if cmd != nil {
		t.Fatalf("Ctrl+J should not submit or produce a command, got %v", cmd)
	}
	updatedModel := newModel.(*Model)
	if got := updatedModel.input.Value(); got != "hello\n" {
		t.Fatalf("expected Ctrl+J to append newline, got %q", got)
	}
	if got := updatedModel.input.Height(); got != 2 {
		t.Fatalf("expected textarea height to grow to 2, got %d", got)
	}
}

func TestBackslashEnterInsertsNewline(t *testing.T) {
	m := newTestModel(80)
	var outbound []eventsource.Outbound
	m.BindOutbound(func(o eventsource.Outbound) error {
		outbound = append(outbound, o)
		return nil
	})
	m.input.SetValue("hello\\")

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runTeaCmd(t, cmd)

	updatedModel := newModel.(*Model)
	if got := updatedModel.input.Value(); got != "hello\n" {
		t.Fatalf("expected trailing backslash Enter to insert newline, got %q", got)
	}
	if len(outbound) != 0 {
		t.Fatalf("expected no outbound submit for line continuation, got %+v", outbound)
	}
}

func TestEnterSubmits(t *testing.T) {
	m := newTestModel(80)
	var outbound []eventsource.Outbound
	m.BindOutbound(func(o eventsource.Outbound) error {
		outbound = append(outbound, o)
		return nil
	})
	m.input.SetValue("hi")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runTeaCmd(t, cmd)

	if len(outbound) != 1 {
		t.Fatalf("expected one outbound submit, got %+v", outbound)
	}
	if outbound[0].Kind != "submit" || outbound[0].Text != "hi" {
		t.Fatalf("expected submit outbound with text hi, got %+v", outbound[0])
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected input reset after submit, got %q", got)
	}
}

func TestCtrlRClearsAndPrintsFeedback(t *testing.T) {
	m := newTestModel(80)
	m.status = "处理中..."
	m.lines = []string{"old"}
	m.input.SetValue("draft")
	m.newSessionHandler = func() string { return "session_test" }

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd == nil {
		t.Fatal("expected Ctrl+R to return clear/print command")
	}
	updatedModel := newModel.(*Model)
	if len(updatedModel.lines) == 0 {
		t.Fatal("expected Ctrl+R to append feedback line")
	}
	last := updatedModel.lines[len(updatedModel.lines)-1]
	if !strings.Contains(last, "已开启新会话 (id: session_test)") {
		t.Fatalf("expected new-session feedback, got %q", last)
	}
	if updatedModel.status != "等待输入" {
		t.Fatalf("expected status reset to 等待输入, got %q", updatedModel.status)
	}
	if got := updatedModel.input.Value(); got != "" {
		t.Fatalf("expected input reset, got %q", got)
	}
}

func TestUpDownArrowOnlyBrowsesHistoryAtBoundary(t *testing.T) {
	m := newTestModel(80)
	m.inputHistory = []string{"previous"}
	m.input.SetValue("line1\nline2\nline3")
	m.input.CursorUp()

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	updatedModel := newModel.(*Model)
	if got := updatedModel.input.Value(); got != "line1\nline2\nline3" {
		t.Fatalf("up from middle line should move cursor, not browse history; got %q", got)
	}
	if updatedModel.historyIndex != -1 {
		t.Fatalf("expected history browsing to remain inactive, got index %d", updatedModel.historyIndex)
	}

	updatedModel.input.CursorEnd()
	newModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyDown})
	updatedModel = newModel.(*Model)
	if got := updatedModel.input.Value(); got != "line1\nline2\nline3" {
		t.Fatalf("down before last line should move cursor, not browse history; got %q", got)
	}
	if updatedModel.historyIndex != -1 {
		t.Fatalf("expected history browsing to remain inactive, got index %d", updatedModel.historyIndex)
	}

	updatedModel.input.CursorUp()
	updatedModel.input.CursorUp()
	newModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyUp})
	updatedModel = newModel.(*Model)
	if got := updatedModel.input.Value(); got != "previous" {
		t.Fatalf("up on first line should browse history, got %q", got)
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

// --- status state machine tests ---

func TestRenderInputSectionMultilineAtLeastFixedLineCount(t *testing.T) {
	m := newTestModel(80)
	m.input.SetValue("line 1\nline 2\nline 3")
	m.adjustInputHeight() // sync height after direct SetValue

	var b strings.Builder
	m.renderInputSection(&b)
	output := b.String()

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) < 5 {
		t.Errorf("expected at least 5 lines, got %d:\n%s", len(lines), output)
	}
	for _, want := range []string{"line 1", "line 2", "line 3"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected rendered multiline input to contain %q:\n%s", want, output)
		}
	}
}

func TestToolUseMsgUpdatesStatusToToolExecuting(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(ToolUseMsg{ID: "1", ToolName: "read_file", Status: "executing"})

	if got := m.status; got != "工具执行中: read_file" {
		t.Fatalf("expected tool executing status, got %q", got)
	}
	if m.currentPhase != phaseToolCall {
		t.Fatalf("expected phaseToolCall, got %v", m.currentPhase)
	}
}

func TestToolUseMsgMultipleParallelShowsCount(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(ToolUseMsg{ID: "1", ToolName: "read_file", Status: "executing"})
	_, _ = m.Update(ToolUseMsg{ID: "2", ToolName: "write_file", Status: "executing"})

	if got := m.status; got != "工具执行中 (2 个并行)" {
		t.Fatalf("expected parallel tool count status, got %q", got)
	}
}

func TestToolUseMsgSuccessTransitsBackToProcessing(t *testing.T) {
	m := newTestModel(80)
	_, _ = m.Update(ToolUseMsg{ID: "1", ToolName: "read_file", Status: "executing"})

	_, _ = m.Update(ToolUseMsg{ID: "1", ToolName: "read_file", Status: "success", Result: "ok"})

	if got := m.status; got != "处理中..." {
		t.Fatalf("expected processing after final tool success, got %q", got)
	}
	if m.currentPhase != phaseProcessing {
		t.Fatalf("expected phaseProcessing, got %v", m.currentPhase)
	}
}

func TestStatusUpdateDoneDoesNotDowngradeToolPhase(t *testing.T) {
	m := newTestModel(80)
	_, _ = m.Update(ToolUseMsg{ID: "1", ToolName: "read_file", Status: "executing"})

	_, _ = m.Update(StatusUpdate("完成"))

	if got := m.status; got != "工具执行中: read_file" {
		t.Fatalf("expected stale Done to be ignored during tool execution, got %q", got)
	}
	if m.currentPhase != phaseToolCall {
		t.Fatalf("expected phaseToolCall, got %v", m.currentPhase)
	}
}

func TestShowConfirmationSetsAwaitingApproval(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(ShowConfirmationMsg{ToolInfo: map[string]interface{}{"Name": "write_file"}})

	if got := m.status; got != "等待用户确认: write_file" {
		t.Fatalf("expected awaiting approval status, got %q", got)
	}
	if m.currentPhase != phaseAwaitingApproval {
		t.Fatalf("expected phaseAwaitingApproval, got %v", m.currentPhase)
	}
}

func TestHideConfirmationResumesProcessing(t *testing.T) {
	m := newTestModel(80)
	_, _ = m.Update(ShowConfirmationMsg{ToolInfo: map[string]interface{}{"Name": "write_file"}})

	m.hideConfirmation()

	if got := m.status; got != "处理中..." {
		t.Fatalf("expected processing status after hiding confirmation, got %q", got)
	}
	if m.currentPhase != phaseProcessing {
		t.Fatalf("expected phaseProcessing, got %v", m.currentPhase)
	}
}

func TestTaskCompletionMsgForcesPhaseDone(t *testing.T) {
	m := newTestModel(80)
	_, _ = m.Update(ToolUseMsg{ID: "1", ToolName: "read_file", Status: "executing"})

	_, _ = m.Update(TaskCompletionMsg{Reason: "done"})

	if got := m.status; got != "✅ 完成" {
		t.Fatalf("expected done status, got %q", got)
	}
	if m.currentPhase != phaseDone {
		t.Fatalf("expected phaseDone, got %v", m.currentPhase)
	}
}

func TestSubmitForcesProcessingEvenWhenInToolPhase(t *testing.T) {
	m := newTestModel(80)
	var outbound []eventsource.Outbound
	m.BindOutbound(func(o eventsource.Outbound) error {
		outbound = append(outbound, o)
		return nil
	})
	m.currentPhase = phaseToolCall
	m.status = "工具执行中: read_file"
	m.activeToolCalls["1"] = "executing"
	m.input.SetValue("next question")

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := m.status; got != "处理中..." {
		t.Fatalf("expected submit to force processing, got %q", got)
	}
	if m.currentPhase != phaseProcessing {
		t.Fatalf("expected phaseProcessing, got %v", m.currentPhase)
	}
}

func TestStatusNoLongerTicksBackToIdleAfterDone(t *testing.T) {
	m := newTestModel(80)

	_, cmd := m.Update(StatusUpdate("完成"))

	if got := m.status; got != "✅ 完成" {
		t.Fatalf("expected done status, got %q", got)
	}
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected no delayed idle tick command, got %#v", msg)
		}
	}
}

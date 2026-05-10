package bubbletea

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/filesearch"
	"github.com/nano-harness/nano-agent/pkg/slash"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	xansi "github.com/charmbracelet/x/ansi"
)

// newTestModel returns a minimal Model suitable for unit tests.
func newTestModel(termWidth int) *Model {
	ta := textarea.New()
	ta.Placeholder = "输入您的请求..."
	ta.Focus()
	ta.SetWidth(50)
	ta.SetHeight(minInputHeight)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.KeyMap.InsertNewline.SetEnabled(false)
	applyTextareaTheme(&ta)
	return &Model{
		status:           "等待输入",
		termWidth:        termWidth,
		input:            ta,
		historyIndex:     -1,
		activeToolCalls:  make(map[string]string),
		currentPhase:     phaseIdle,
		terminalFocused:  true,
		streamingLineIdx: -1,
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

func teaPrintfBody(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected tea.Printf command, got nil")
	}
	msg := cmd()
	if got := reflect.TypeOf(msg).String(); got != "tea.printLineMessage" {
		t.Fatalf("expected tea.printLineMessage, got %s", got)
	}
	return reflect.ValueOf(msg).FieldByName("messageBody").String()
}

func assertClearScreenCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected ClearScreen command, got nil")
	}
	msg := cmd()
	if got := reflect.TypeOf(msg).String(); got != "tea.clearScreenMsg" {
		t.Fatalf("expected ClearScreen command, got %s", got)
	}
}

// --- WindowSizeMsg ---

func TestUpdate_WindowSizeMsg_UpdatesDimensions(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 24
	m.input.SetHeight(minInputHeight)

	newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	updatedModel := newModel.(*Model)

	assertClearScreenCmd(t, cmd)
	if got := updatedModel.termWidth; got != 50 {
		t.Fatalf("expected termWidth 50, got %d", got)
	}
	if got := updatedModel.termHeight; got != 20 {
		t.Fatalf("expected termHeight 20, got %d", got)
	}
	if got := updatedModel.input.Width(); got != 50-inputContentMargin {
		t.Fatalf("expected input width %d, got %d", 50-inputContentMargin, got)
	}
}

func TestUpdate_WindowSizeMsg_AdjustsInputHeight(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 24
	input := "line 1\nline 2\nline 3\nline 4\nline 5"
	m.input.SetValue(input)
	m.input.SetHeight(minInputHeight)

	expected := newTestModel(80)
	expected.input.SetValue(input)
	expected.input.SetWidth(50 - inputContentMargin + expected.input.Styles().Focused.Base.GetHorizontalFrameSize())
	expected.adjustInputHeight()

	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	updatedModel := newModel.(*Model)

	if got, want := updatedModel.input.Height(), expected.input.Height(); got != want {
		t.Fatalf("expected input height %d, got %d", want, got)
	}
}

func TestUpdate_WindowSizeMsg_NoOpWhenSameSize(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 24

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if cmd != nil {
		t.Fatalf("expected nil command for same-size resize, got %T", cmd)
	}
}

func TestUpdate_WindowSizeMsg_WidthChange_ClearScreenOnly(t *testing.T) {
	// Width changes can make inline input rows shrink; clear the screen once
	// without reprinting history.
	m := newTestModel(80)
	m.termHeight = 24
	m.lines = []string{
		"assistant: " + strings.Repeat("alpha beta ", 12),
		"tool: " + strings.Repeat("0123456789", 10),
	}

	newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	updatedModel := newModel.(*Model)

	if got := updatedModel.termWidth; got != 30 {
		t.Fatalf("expected termWidth 30, got %d", got)
	}
	assertClearScreenCmd(t, cmd)
}

func TestUpdate_WindowSizeMsg_HeightOnlyChange_ClearScreen(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 24
	m.lines = []string{"assistant: " + strings.Repeat("history ", 20)}

	newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	updatedModel := newModel.(*Model)

	if got := updatedModel.termHeight; got != 40 {
		t.Fatalf("expected termHeight 40, got %d", got)
	}
	assertClearScreenCmd(t, cmd)
}

func TestUpdate_WindowSizeMsg_EmptyHistory_ClearScreen(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 24
	m.lines = nil

	newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	updatedModel := newModel.(*Model)

	if got := updatedModel.termWidth; got != 30 {
		t.Fatalf("expected termWidth 30, got %d", got)
	}
	assertClearScreenCmd(t, cmd)
}

func TestView_RequestsBubbleTeaV2Capabilities(t *testing.T) {
	m := newTestModel(80)
	m.currentPhase = phaseThinking

	v := m.View()

	if !v.KeyboardEnhancements.ReportEventTypes {
		t.Fatal("expected keyboard event type reporting to be requested")
	}
	if !v.ReportFocus {
		t.Fatal("expected focus reporting to be requested")
	}
	if got, want := v.WindowTitle, "nano · 思考中..."; got != want {
		t.Fatalf("WindowTitle = %q, want %q", got, want)
	}
}

func TestUpdate_KeyboardEnhancementsMsg_RecordsSupport(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(tea.KeyboardEnhancementsMsg{Flags: 1})

	if !m.keyboardEnhanced {
		t.Fatal("expected keyboardEnhanced to be true")
	}
}

func TestUpdate_ShiftEnterInsertsNewline(t *testing.T) {
	m := newTestModel(80)
	m.input.SetValue("hello")
	m.input.CursorEnd()

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift}))

	if cmd != nil {
		t.Fatalf("expected no command, got %T", cmd)
	}
	if got, want := m.InputValue(), "hello\n"; got != want {
		t.Fatalf("InputValue() = %q, want %q", got, want)
	}
}

func TestUpdate_PasteMsg_InsertsSmallPaste(t *testing.T) {
	m := newTestModel(80)
	m.input.SetValue("hello ")
	m.input.CursorEnd()

	_, cmd := m.Update(tea.PasteMsg{Content: "world"})

	if cmd != nil {
		t.Fatalf("expected no command, got %T", cmd)
	}
	if got, want := m.InputValue(), "hello world"; got != want {
		t.Fatalf("InputValue() = %q, want %q", got, want)
	}
}

func TestUpdate_PasteMsg_ConfirmsLargePaste(t *testing.T) {
	m := newTestModel(80)
	large := strings.Repeat("x", largePasteBytes+1)

	_, _ = m.Update(tea.PasteMsg{Content: large})
	showing, selected := m.ConfirmationState()

	if !showing || selected != 1 {
		t.Fatalf("ConfirmationState() = (%v, %d), want (true, 1)", showing, selected)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if cmd != nil {
		t.Fatalf("expected no command selecting confirm, got %T", cmd)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runTeaCmd(t, cmd)
	if got := m.InputValue(); got != large {
		t.Fatalf("InputValue() length = %d, want %d", len(got), len(large))
	}
}

func TestUpdate_PasteConfirmationUsesTwoChoices(t *testing.T) {
	m := newTestModel(80)
	large := strings.Repeat("x", largePasteBytes+1)

	_, _ = m.Update(tea.PasteMsg{Content: large})
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	_ = m.View()

	if got := m.confirmationSelected; got != 1 {
		t.Fatalf("confirmationSelected = %d, want 1", got)
	}
	if got := len(m.confirmationButtons); got != 2 {
		t.Fatalf("large paste confirmation buttons = %d, want 2", got)
	}
	if strings.Contains(m.View().Content, "始终允许") {
		t.Fatal("large paste confirmation should not render always-allow action")
	}
}

func TestUpdate_MouseClickConfirmationButton(t *testing.T) {
	m := newTestModel(80)
	called := false
	approved := true
	_, _ = m.Update(ShowConfirmationMsg{
		Message:  "confirm?",
		ToolInfo: map[string]interface{}{"Name": "write_file"},
		Callback: func(ok bool) {
			called = true
			approved = ok
		},
	})
	_ = m.View()
	box := m.confirmationButtons[1]

	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: box.x0, Y: box.y, Button: tea.MouseLeft}))
	runTeaCmd(t, cmd)

	if !called || approved {
		t.Fatalf("callback called=%v approved=%v, want true/false", called, approved)
	}
}

func TestUpdate_MouseCommandSelection(t *testing.T) {
	m := newTestModel(80)
	m.showingCommands = true
	m.commands = []slash.Command{
		{Name: "first", Description: "first command"},
		{Name: "second", Description: "second command"},
	}
	_ = m.View()
	box := m.commandItems[1]

	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: box.x0, Y: box.y, Button: tea.MouseLeft}))

	if cmd != nil {
		t.Fatalf("expected no command, got %T", cmd)
	}
	if got, want := m.InputValue(), "/second "; got != want {
		t.Fatalf("InputValue() = %q, want %q", got, want)
	}
	if m.showingCommands {
		t.Fatal("expected command palette to close after mouse selection")
	}
}

func TestUpdate_FullscreenHistoryNavigationAndView(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 6
	m.lines = []string{"one", "two", "three", "four", "five"}

	_, _ = m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if !m.altScreenActive {
		t.Fatal("expected fullscreen history mode to activate")
	}
	v := m.View()
	if !v.AltScreen || v.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("View AltScreen=%v MouseMode=%v, want true/%v", v.AltScreen, v.MouseMode, tea.MouseModeCellMotion)
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got, want := m.historySelected, 3; got != want {
		t.Fatalf("historySelected = %d, want %d", got, want)
	}
	if got, want := m.getSelectedHistoryContent(), "four"; got != want {
		t.Fatalf("getSelectedHistoryContent() = %q, want %q", got, want)
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.altScreenActive {
		t.Fatal("expected fullscreen history mode to exit")
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
	full := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 搜索历史 | Ctrl+L 新会话 | Ctrl+P 命令 | Ctrl+C 退出 | Tab 补全 | ↑↓ 历史"
	if got := m.buildHelpText(); got != full {
		t.Errorf("expected full text for wide terminal, got %q", got)
	}
}

func TestBuildHelpText_NoTermWidth(t *testing.T) {
	m := newTestModel(0)
	full := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 搜索历史 | Ctrl+L 新会话 | Ctrl+P 命令 | Ctrl+C 退出 | Tab 补全 | ↑↓ 历史"
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
	short := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 搜索 | Ctrl+L 新会话 | Ctrl+P 命令 | Tab 补全"
	full := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 搜索历史 | Ctrl+L 新会话 | Ctrl+P 命令 | Ctrl+C 退出 | Tab 补全 | ↑↓ 历史"
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
	minimal := "Enter发送 | ^J换行 | ^R搜索 | ^L新会话"
	short := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 搜索 | Ctrl+L 新会话 | Ctrl+P 命令 | Tab 补全"
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
	if len(lines) != 7 {
		t.Errorf("expected 7 lines, got %d:\n%s", len(lines), output)
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
	if len(lines) != 7 {
		t.Errorf("expected 7 lines, got %d:\n%s", len(lines), output)
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

func TestRenderInputSectionStableHeight_ThinkingShrink(t *testing.T) {
	m := newTestModel(80)
	m.thinkingWindow = []string{"first thought", "second thought", "third thought"}

	var b strings.Builder
	m.renderInputSection(&b)
	tallLines := strings.Count(b.String(), "\n")
	if tallLines == 0 {
		t.Fatal("expected initial render to contain lines")
	}

	m.thinkingWindow = nil
	b.Reset()
	m.renderInputSection(&b)
	shrunkLines := strings.Count(b.String(), "\n")

	if shrunkLines < tallLines {
		t.Fatalf("expected padded render line count >= %d after thinking disappears, got %d:\n%s", tallLines, shrunkLines, b.String())
	}
	if got := m.lastRenderHeight; got != tallLines {
		t.Fatalf("expected lastRenderHeight to remain %d, got %d", tallLines, got)
	}
}

func TestRenderInputSection_ResizeResetsHeight(t *testing.T) {
	m := newTestModel(80)
	m.termHeight = 24
	m.thinkingWindow = []string{"first thought", "second thought"}

	var b strings.Builder
	m.renderInputSection(&b)
	if m.lastRenderHeight == 0 {
		t.Fatal("expected render to record lastRenderHeight")
	}

	newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	updatedModel := newModel.(*Model)

	assertClearScreenCmd(t, cmd)
	if got := updatedModel.lastRenderHeight; got != 0 {
		t.Fatalf("expected resize to reset lastRenderHeight, got %d", got)
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
	if len(lines) != 7 {
		t.Fatalf("expected 7 lines, got %d:\n%s", len(lines), output)
	}
	// Lines 0 (separator), 1 (status), and 2 (token) are explicitly
	// truncated by renderInputSection to termWidth.  Line 3 is the text
	// input widget (width managed separately) and the final line is the help text
	// (handled by buildHelpText).
	for _, i := range []int{0, 1, 2} {
		w := xansi.StringWidth(lines[i])
		if w > termWidth {
			t.Errorf("line %d exceeds termWidth=%d (got width %d): %q", i+1, termWidth, w, lines[i])
		}
	}
}

func TestRenderInputSection_InputLineTruncatedAtNarrowWidth(t *testing.T) {
	const termWidth = 25
	m := newTestModel(termWidth)
	m.input.SetWidth(termWidth - inputContentMargin + m.input.Styles().Focused.Base.GetHorizontalFrameSize())
	m.input.SetValue(strings.Repeat("very-long-input ", 8))

	var b strings.Builder
	m.renderInputSection(&b)

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) < 5 {
		t.Fatalf("expected input section lines, got %d:\n%s", len(lines), b.String())
	}
	for i := 3; i < len(lines)-1; i++ {
		if w := xansi.StringWidth(lines[i]); w > termWidth {
			t.Fatalf("input line %d exceeds termWidth=%d (got width %d): %q", i+1, termWidth, w, lines[i])
		}
	}
}

func TestRenderInputSection_VeryNarrow(t *testing.T) {
	// Even at width=1 the function must not panic, must produce stable lines,
	// and the separator/status/token lines must not exceed 1 column.
	m := newTestModel(1)
	m.status = "busy"
	m.tokenStatus = "tok"

	var b strings.Builder
	m.renderInputSection(&b)
	output := b.String()

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("expected 7 lines at width=1, got %d:\n%s", len(lines), output)
	}
	for _, i := range []int{0, 1, 2} {
		w := xansi.StringWidth(lines[i])
		if w > 1 {
			t.Errorf("line %d exceeds termWidth=1 (got width %d): %q", i+1, w, lines[i])
		}
	}
}

func TestFlushStreamingBuffer_NarrowWidthNoOverflow(t *testing.T) {
	const termWidth = 30
	m := newTestModel(termWidth)
	m.isStreaming = true
	m.streamingBuf.WriteString(strings.Repeat("streaming-response ", 8))

	cmds := m.flushStreamingBuffer()

	if len(cmds) != 1 {
		t.Fatalf("expected one print command, got %d", len(cmds))
	}
	if len(m.lines) != 1 {
		t.Fatalf("expected one scrollback line, got %#v", m.lines)
	}
	wrapped := m.wrapFormattedLine(m.lines[0])
	for i, line := range strings.Split(wrapped, "\n") {
		if w := xansi.StringWidth(line); w > termWidth {
			t.Fatalf("wrapped streaming line %d exceeds termWidth=%d (got width %d): %q", i+1, termWidth, w, line)
		}
	}
}

func TestSpinnerSuppressedAfterFlush(t *testing.T) {
	m := newTestModel(80)
	m.currentPhase = phaseResponse
	m.lastFlushTime = time.Now()
	m.spinnerFrame = 3

	if m.shouldAnimateSpinner() {
		t.Fatal("spinner should be suppressed immediately after a streaming flush")
	}
	updated, cmd := m.Update(SpinnerTickMsg(time.Now()))
	updatedModel := updated.(*Model)
	if updatedModel.spinnerFrame != 3 {
		t.Fatalf("spinner frame should not change during suppression, got %d", updatedModel.spinnerFrame)
	}
	if cmd == nil {
		t.Fatal("spinner tick should still be scheduled while phase is active")
	}

	m.lastFlushTime = time.Now().Add(-spinnerTickInterval)
	if !m.shouldAnimateSpinner() {
		t.Fatal("spinner should animate after the suppression window")
	}
}

// --- q key input fix tests ---

// TestQKeyDoesNotQuit verifies that pressing 'q' in normal input mode
// does not trigger quit, but instead passes the key to the text input.
func TestQKeyDoesNotQuit(t *testing.T) {
	m := newTestModel(80)
	m.input.SetValue("") // Start with empty input

	// Simulate pressing 'q' key
	msg := tea.KeyPressMsg{Code: 'q', Text: "q"}
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

	newModel, cmd := m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("Ctrl+J should not submit or produce a command, got %v", cmd)
	}
	updatedModel := newModel.(*Model)
	if got := updatedModel.input.Value(); got != "hello\n" {
		t.Fatalf("expected Ctrl+J to append newline, got %q", got)
	}
	if got := updatedModel.input.Height(); got != minInputHeight {
		t.Fatalf("expected textarea height to remain at minimum %d, got %d", minInputHeight, got)
	}
}

func TestModel_ThinkingWindowScrolls(t *testing.T) {
	m := newTestModel(80)
	m.appendThinkingDelta(strings.Repeat("word ", 80))

	if len(m.thinkingWindow) != 3 {
		t.Fatalf("expected three thinking lines, got %#v", m.thinkingWindow)
	}
	for _, line := range m.thinkingWindow {
		if xansi.StringWidth(line) > 72 {
			t.Fatalf("expected line width <= 72, got %d for %q", xansi.StringWidth(line), line)
		}
	}
}

func TestThinkingMsg_NoHeaderWrittenToScrollback(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(ThinkingMsg{Title: "正在思考...", ReasoningDelta: "first "})
	if got := strings.Join(m.thinkingWindow, " "); !strings.Contains(got, "first") {
		t.Fatalf("expected in-progress thinking window to contain first delta, got %#v", m.thinkingWindow)
	}
	_, _ = m.Update(ThinkingMsg{ReasoningDelta: "second "})
	if got := strings.Join(m.thinkingWindow, " "); !strings.Contains(got, "first second") {
		t.Fatalf("expected in-progress thinking window to contain accumulated deltas, got %#v", m.thinkingWindow)
	}
	_, _ = m.Update(ThinkingMsg{
		Reasoning: "first second",
		Metadata:  map[string]interface{}{"is_complete": true},
	})

	if len(m.lines) < 1 {
		t.Fatalf("expected completion summary in scrollback, got %#v", m.lines)
	}
	if strings.Contains(strings.Join(m.lines, "\n"), "[进行中]") {
		t.Fatalf("scrollback should not contain in-progress header: %#v", m.lines)
	}
	if !strings.Contains(m.lines[0], "思考完成") {
		t.Fatalf("expected completion summary, got %q", m.lines[0])
	}
}

func TestAppendThinkingDelta_SoftWrap(t *testing.T) {
	m := newTestModel(30)

	m.appendThinkingDelta(strings.Repeat("abcdefghij ", 12))

	if len(m.thinkingWindow) != 3 {
		t.Fatalf("expected three-line thinking window, got %#v", m.thinkingWindow)
	}
	for _, line := range m.thinkingWindow {
		if got := xansi.StringWidth(line); got > 22 {
			t.Fatalf("expected line width <= 22, got %d for %q", got, line)
		}
	}
}

func TestModel_ContextBarColors(t *testing.T) {
	m := newTestModel(80)
	m.contextWindowMax = 100

	m.contextUsedTokens = 50
	if got := m.renderContextBar(); !strings.Contains(got, "50%") {
		t.Fatalf("expected green-range 50%% context bar, got %q", got)
	}
	m.contextUsedTokens = 70
	if got := m.renderContextBar(); !strings.Contains(got, "70%") {
		t.Fatalf("expected yellow-range 70%% context bar, got %q", got)
	}
	m.contextUsedTokens = 90
	if got := m.renderContextBar(); !strings.Contains(got, "90%") {
		t.Fatalf("expected red-range 90%% context bar, got %q", got)
	}
}

func TestModel_HashTriggersFilePicker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/go.mod", []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := filesearch.NewIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Crawl(); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(80)
	m.fileIndex = idx
	m.input.SetValue("#gom")
	m.updateFilePickerState()

	if !m.showingFilePicker {
		t.Fatal("expected file picker to be shown")
	}
	if len(m.filePickerResults) == 0 || m.filePickerResults[0] != "go.mod" {
		t.Fatalf("expected go.mod result, got %#v", m.filePickerResults)
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

	newModel, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

func TestCtrlLClearsAndPrintsFeedback(t *testing.T) {
	m := newTestModel(80)
	m.status = "处理中..."
	m.lines = []string{"old"}
	m.input.SetValue("draft")
	m.newSessionHandler = func() string { return "session_test" }

	newModel, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected Ctrl+L to return clear/print command")
	}
	updatedModel := newModel.(*Model)
	if len(updatedModel.lines) == 0 {
		t.Fatal("expected Ctrl+L to append feedback line")
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

func TestCtrlRStartsHistorySearch(t *testing.T) {
	m := newTestModel(80)
	m.inputHistory = []string{"git status", "go test ./...", "git log"}
	m.input.SetValue("draft")

	newModel, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("expected Ctrl+R history search to be local")
	}
	updatedModel := newModel.(*Model)
	if updatedModel.historySearch == nil || !updatedModel.historySearch.Active() {
		t.Fatal("expected history search to be active")
	}

	newModel, _ = updatedModel.Update(tea.KeyPressMsg{Text: "git"})
	updatedModel = newModel.(*Model)
	if got := updatedModel.input.Value(); got != "git log" {
		t.Fatalf("expected newest matching history entry, got %q", got)
	}

	newModel, _ = updatedModel.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	updatedModel = newModel.(*Model)
	if got := updatedModel.input.Value(); got != "git status" {
		t.Fatalf("expected Ctrl+R to cycle to older match, got %q", got)
	}

	newModel, _ = updatedModel.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	updatedModel = newModel.(*Model)
	if got := updatedModel.input.Value(); got != "draft" {
		t.Fatalf("expected Esc to restore draft, got %q", got)
	}
}

func TestUpDownArrowOnlyBrowsesHistoryAtBoundary(t *testing.T) {
	m := newTestModel(80)
	m.inputHistory = []string{"previous"}
	m.input.SetValue("line1\nline2\nline3")
	m.input.CursorUp()

	newModel, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	updatedModel := newModel.(*Model)
	if got := updatedModel.input.Value(); got != "line1\nline2\nline3" {
		t.Fatalf("up from middle line should move cursor, not browse history; got %q", got)
	}
	if updatedModel.historyIndex != -1 {
		t.Fatalf("expected history browsing to remain inactive, got index %d", updatedModel.historyIndex)
	}

	updatedModel.input.CursorEnd()
	newModel, _ = updatedModel.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	updatedModel = newModel.(*Model)
	if got := updatedModel.input.Value(); got != "line1\nline2\nline3" {
		t.Fatalf("down before last line should move cursor, not browse history; got %q", got)
	}
	if updatedModel.historyIndex != -1 {
		t.Fatalf("expected history browsing to remain inactive, got index %d", updatedModel.historyIndex)
	}

	updatedModel.input.CursorUp()
	updatedModel.input.CursorUp()
	newModel, _ = updatedModel.Update(tea.KeyPressMsg{Code: tea.KeyUp})
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
	msg := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
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

func TestStatusUpdateDoneClearsThinkingState(t *testing.T) {
	m := newTestModel(80)
	m.thinkingTitle = "thinking"
	m.thinkingReasoning = "old thought"
	m.thinkingCompleted = true
	m.thinkingCollapsed = true
	m.thinkingWindow = []string{"old thought"}
	m.thinkingPending = "old"
	m.spinnerFrame = 3
	m.spinnerStage = "thinking"
	_, _ = m.Update(ToolUseMsg{ID: "1", ToolName: "read_file", Status: "executing"})

	_, _ = m.Update(StatusUpdate("完成"))

	if len(m.thinkingWindow) != 0 {
		t.Fatalf("expected thinking window to be cleared, got %#v", m.thinkingWindow)
	}
	if m.thinkingTitle != "" {
		t.Fatalf("expected thinking title to be cleared, got %q", m.thinkingTitle)
	}
	if m.thinkingReasoning != "" {
		t.Fatalf("expected thinking reasoning to be cleared, got %q", m.thinkingReasoning)
	}
	if len(m.activeToolCalls) != 0 {
		t.Fatalf("expected active tool calls to be cleared, got %#v", m.activeToolCalls)
	}
	if m.thinkingPending != "" {
		t.Fatalf("expected thinking pending to be cleared, got %q", m.thinkingPending)
	}
	if m.spinnerStage != "" || m.spinnerFrame != 0 {
		t.Fatalf("expected spinner to be reset, got stage=%q frame=%d", m.spinnerStage, m.spinnerFrame)
	}
	if got := m.status; got != "✅ 完成" {
		t.Fatalf("expected done status, got %q", got)
	}
	if m.currentPhase != phaseDone {
		t.Fatalf("expected phaseDone, got %v", m.currentPhase)
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

func TestTaskCompletionMsgClearsThinkingState(t *testing.T) {
	m := newTestModel(80)
	m.thinkingTitle = "thinking"
	m.thinkingReasoning = "old thought"
	m.thinkingCompleted = true
	m.thinkingCollapsed = true
	m.thinkingWindow = []string{"old thought"}
	m.thinkingPending = "old"
	m.spinnerFrame = 5
	m.spinnerStage = "thinking"
	m.activeToolCalls["1"] = "executing"

	_, _ = m.Update(TaskCompletionMsg{Reason: "done"})

	if len(m.thinkingWindow) != 0 {
		t.Fatalf("expected thinking window to be cleared, got %#v", m.thinkingWindow)
	}
	if m.thinkingTitle != "" {
		t.Fatalf("expected thinking title to be cleared, got %q", m.thinkingTitle)
	}
	if m.thinkingReasoning != "" {
		t.Fatalf("expected thinking reasoning to be cleared, got %q", m.thinkingReasoning)
	}
	if len(m.activeToolCalls) != 0 {
		t.Fatalf("expected active tool calls to be cleared, got %#v", m.activeToolCalls)
	}
	if m.thinkingPending != "" {
		t.Fatalf("expected thinking pending to be cleared, got %q", m.thinkingPending)
	}
	if !m.thinkingCollapsed {
		t.Fatal("expected thinking collapsed to reset to the default collapsed state")
	}
	if m.spinnerStage != "" || m.spinnerFrame != 0 {
		t.Fatalf("expected spinner to be reset, got stage=%q frame=%d", m.spinnerStage, m.spinnerFrame)
	}
	if m.currentPhase != phaseDone {
		t.Fatalf("expected phaseDone, got %v", m.currentPhase)
	}
}

func TestSpinnerTick_AdvancesOnlyInActivePhase(t *testing.T) {
	m := newTestModel(80)
	m.spinnerFrame = 10

	_, _ = m.Update(SpinnerTickMsg(time.Now()))
	if m.spinnerFrame != 10 {
		t.Fatalf("idle tick should not advance frame, got %d", m.spinnerFrame)
	}

	m.currentPhase = phaseThinking
	_, _ = m.Update(SpinnerTickMsg(time.Now()))
	if m.spinnerFrame != 11 {
		t.Fatalf("thinking tick should advance frame, got %d", m.spinnerFrame)
	}

	m.currentPhase = phaseDone
	_, _ = m.Update(SpinnerTickMsg(time.Now()))
	if m.spinnerFrame != 11 {
		t.Fatalf("done tick should not advance frame, got %d", m.spinnerFrame)
	}
}

func TestSpinnerTickStopsWhileBlurredAndRestartsOnFocus(t *testing.T) {
	m := newTestModel(80)
	m.currentPhase = phaseThinking
	m.spinnerFrame = 2

	_, _ = m.Update(tea.BlurMsg{})
	_, cmd := m.Update(SpinnerTickMsg(time.Now()))

	if cmd != nil {
		t.Fatalf("blurred spinner tick should not reschedule, got %T", cmd)
	}
	if got := m.spinnerFrame; got != 2 {
		t.Fatalf("blurred spinner frame = %d, want 2", got)
	}
	_, cmd = m.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("focused active spinner should restart ticking")
	}
}

func TestSpinnerHiddenInIdleAndDonePhase(t *testing.T) {
	m := newTestModel(80)
	m.spinnerStage = "writing"
	m.spinnerFrame = 1

	m.currentPhase = phaseIdle
	if got := m.currentSpinnerFrame(); got != "" {
		t.Fatalf("idle spinner = %q, want hidden", got)
	}

	m.currentPhase = phaseDone
	if got := m.currentSpinnerFrame(); got != "" {
		t.Fatalf("done spinner = %q, want hidden", got)
	}
}

func TestSpinnerVisibleInActivePhases(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase displayPhase
		stage string
	}{
		{name: "thinking", phase: phaseThinking, stage: "thinking"},
		{name: "tool", phase: phaseToolCall, stage: "executing"},
		{name: "response", phase: phaseResponse, stage: "writing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(80)
			m.currentPhase = tc.phase
			m.spinnerStage = tc.stage
			m.spinnerFrame = 1

			got := m.currentSpinnerFrame()
			if got != "⠙" {
				t.Fatalf("currentSpinnerFrame() = %q, want braille frame", got)
			}
		})
	}
}

func TestSpinnerStatusUpdateIdleDoesNotAdvance(t *testing.T) {
	m := newTestModel(80)
	m.currentPhase = phaseResponse
	m.spinnerStage = "writing"
	m.spinnerFrame = 7

	_, _ = m.Update(StatusUpdate("等待输入"))

	if m.spinnerFrame != 0 {
		t.Fatalf("spinner frame after idle status = %d, want reset without advance", m.spinnerFrame)
	}
	if got := m.currentSpinnerFrame(); got != "" {
		t.Fatalf("idle status spinner = %q, want hidden", got)
	}
}

func TestTokenStatsUpdate_ContextTextRendered(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(TokenStatsUpdate{InputTokens: 100, OutputTokens: 150, TotalTokens: 250, ContextWindowMax: 1000, ContextUsedTokens: 250})

	if !strings.Contains(m.tokenStatus, "上下文: 250/1.0K (25%)") {
		t.Fatalf("expected context usage text, got %q", m.tokenStatus)
	}
}

func TestContextBarShowsDegradedWhenNoMax(t *testing.T) {
	m := newTestModel(80)
	_, _ = m.Update(TokenStatsUpdate{InputTokens: 1234, TotalTokens: 1234, ContextUsedTokens: 1234})

	out := m.View().Content
	if !strings.Contains(out, "ctx: 1.2K") {
		t.Fatalf("expected degraded context usage, got:\n%s", out)
	}
}

func TestThinkingCompleteClearsWindowButPreservesReasoning(t *testing.T) {
	m := newTestModel(80)
	m.thinkingTitle = "thinking"
	m.thinkingReasoning = "old thought"
	m.thinkingWindow = []string{"old thought"}

	_, _ = m.Update(ThinkingMsg{Metadata: map[string]interface{}{"is_complete": true}})

	if len(m.thinkingWindow) != 0 {
		t.Fatalf("expected thinking window to be cleared, got %#v", m.thinkingWindow)
	}
	if m.thinkingReasoning != "old thought" {
		t.Fatalf("expected thinking reasoning to be preserved, got %q", m.thinkingReasoning)
	}
}

func TestStreamingFlushPreservesRawMarkdown(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(Message{Role: "assistant_stream", Content: "**bold**\n"})

	if len(m.lines) == 0 {
		t.Fatal("expected streaming line to be flushed")
	}
	if got := m.lines[len(m.lines)-1]; !strings.Contains(got, "**bold**") {
		t.Fatalf("streaming line should preserve raw markdown delimiters, got %q", got)
	}
}

func TestStreamingFlushIncrementalPartialDoesNotPrint(t *testing.T) {
	m := newTestModel(80)
	m.isStreaming = true
	m.streamingBuf.WriteString("partial chunk")

	cmds := m.flushStreamingIncremental()

	if cmds != nil {
		t.Fatalf("expected no printf command for incomplete chunk, got %d", len(cmds))
	}
	if len(m.lines) != 1 {
		t.Fatalf("expected scrollback line to be updated, got %d: %#v", len(m.lines), m.lines)
	}
	if got, want := stripFormatLine(m.lines[0]), "partial chunk"; got != want {
		t.Fatalf("streaming line = %q, want %q", got, want)
	}
	if got, want := m.streamingPrintBuf.String(), "partial chunk"; got != want {
		t.Fatalf("streamingPrintBuf = %q, want %q", got, want)
	}
	if m.streamingBuf.Len() != 0 {
		t.Fatalf("expected streamingBuf to be consumed, got %q", m.streamingBuf.String())
	}
}

func TestStreamingFlushIncrementalPrintsOnlyCompletedLines(t *testing.T) {
	m := newTestModel(80)
	m.isStreaming = true
	m.streamingBuf.WriteString("first line\npartial tail")

	cmds := m.flushStreamingIncremental()

	if len(cmds) != 1 {
		t.Fatalf("expected one printf command, got %d", len(cmds))
	}
	if got, want := teaPrintfBody(t, cmds[0]), renderStreamingChunk("first line", m.termWidth); got != want {
		t.Fatalf("printf body = %q, want %q", got, want)
	}
	if got, want := m.streamingPrintBuf.String(), "partial tail"; got != want {
		t.Fatalf("streamingPrintBuf = %q, want %q", got, want)
	}
	if got, want := stripFormatLine(m.lines[0]), "first line\npartial tail"; got != want {
		t.Fatalf("streaming line = %q, want %q", got, want)
	}
}

func TestStreamingFlushIncrementalHandlesLeadingNewline(t *testing.T) {
	m := newTestModel(80)
	m.isStreaming = true
	m.streamingBuf.WriteString("\npartial tail")

	cmds := m.flushStreamingIncremental()

	if len(cmds) != 1 {
		t.Fatalf("expected one printf command for completed empty line, got %d", len(cmds))
	}
	if got, want := teaPrintfBody(t, cmds[0]), renderStreamingChunk("", m.termWidth); got != want {
		t.Fatalf("printf body = %q, want %q", got, want)
	}
	if got, want := m.streamingPrintBuf.String(), "partial tail"; got != want {
		t.Fatalf("streamingPrintBuf = %q, want %q", got, want)
	}
	if got, want := stripFormatLine(m.lines[0]), "\npartial tail"; got != want {
		t.Fatalf("streaming line = %q, want %q", got, want)
	}
}

func TestStreamingFlushBufferPrintsRemainingTail(t *testing.T) {
	m := newTestModel(80)
	m.isStreaming = true
	m.streamingBuf.WriteString("partial")
	if cmds := m.flushStreamingIncremental(); cmds != nil {
		t.Fatalf("expected no incremental printf command, got %d", len(cmds))
	}
	m.streamingBuf.WriteString(" tail")

	cmds := m.flushStreamingBuffer()

	if len(cmds) != 1 {
		t.Fatalf("expected one final printf command, got %d", len(cmds))
	}
	if got, want := teaPrintfBody(t, cmds[0]), renderStreamingChunk("partial tail", m.termWidth); got != want {
		t.Fatalf("printf body = %q, want %q", got, want)
	}
	if got, want := stripFormatLine(m.lines[0]), "partial tail"; got != want {
		t.Fatalf("streaming line = %q, want %q", got, want)
	}
	if m.isStreaming {
		t.Fatal("expected final flush to terminate streaming state")
	}
	if m.streamingPrintBuf.Len() != 0 {
		t.Fatalf("expected streamingPrintBuf to be reset, got %q", m.streamingPrintBuf.String())
	}
}

func TestStreamingFlushBufferPreservesExtraTrailingNewline(t *testing.T) {
	m := newTestModel(80)
	m.isStreaming = true
	m.streamingBuf.WriteString("tail\n\n")

	cmds := m.flushStreamingBuffer()

	if len(cmds) != 1 {
		t.Fatalf("expected one final printf command, got %d", len(cmds))
	}
	if got, want := teaPrintfBody(t, cmds[0]), renderStreamingChunk("tail\n", m.termWidth); got != want {
		t.Fatalf("printf body = %q, want %q", got, want)
	}
}

func TestStreamingFlushNewlineDoesNotSplitBlock(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(Message{Role: "assistant_stream", Content: "first line\n"})
	_, _ = m.Update(Message{Role: "assistant_stream", Content: "second line"})
	_, _ = m.Update(TaskCompletionMsg{Reason: "done"})

	if len(m.lines) != 1 {
		t.Fatalf("expected one streaming line, got %d: %#v", len(m.lines), m.lines)
	}
	if got, want := stripFormatLine(m.lines[0]), "first line\nsecond line"; got != want {
		t.Fatalf("streaming line = %q, want %q", got, want)
	}
	if m.isStreaming {
		t.Fatal("expected final flush to terminate streaming state")
	}
	if m.streamingLineIdx != -1 {
		t.Fatalf("streamingLineIdx = %d, want -1", m.streamingLineIdx)
	}
}

func TestStreamingFlushMultiChunkWithNewlinesConcats(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(Message{Role: "assistant_stream", Content: "alpha\n"})
	_, _ = m.Update(Message{Role: "assistant_stream", Content: "beta\n"})

	if len(m.lines) != 1 {
		t.Fatalf("expected one streaming line after incremental flushes, got %d: %#v", len(m.lines), m.lines)
	}
	if got, want := stripFormatLine(m.lines[0]), "alpha\nbeta"; got != want {
		t.Fatalf("streaming line = %q, want %q", got, want)
	}
	if !m.isStreaming {
		t.Fatal("expected incremental flush to keep streaming state active")
	}
}

func TestStreamingFlushToolUseTerminatesStream(t *testing.T) {
	m := newTestModel(80)

	_, _ = m.Update(Message{Role: "assistant_stream", Content: "before\n"})
	_, _ = m.Update(Message{Role: "assistant_stream", Content: "tool"})
	_, _ = m.Update(ToolUseMsg{ID: "tool-1", ToolName: "read_file", Status: "executing"})

	if len(m.lines) != 2 {
		t.Fatalf("expected streaming line and tool line, got %d: %#v", len(m.lines), m.lines)
	}
	if got, want := stripFormatLine(m.lines[0]), "before\ntool"; got != want {
		t.Fatalf("streaming line = %q, want %q", got, want)
	}
	if m.isStreaming {
		t.Fatal("expected tool use to terminate streaming state")
	}
	if m.streamingLineIdx != -1 {
		t.Fatalf("streamingLineIdx = %d, want -1", m.streamingLineIdx)
	}
	if got := xansi.Strip(m.lines[1]); !strings.Contains(got, "正在调用工具: read_file") {
		t.Fatalf("expected tool line after stream, got %q", got)
	}
}

func TestThinkingPreviewPersistsAcrossToolCalls(t *testing.T) {
	m := newTestModel(80)
	_, _ = m.Update(ThinkingMsg{Title: "thinking", ReasoningDelta: "first visible thought"})
	_, _ = m.Update(ThinkingMsg{Title: "thinking", Metadata: map[string]interface{}{"is_complete": true}})
	_, _ = m.Update(ThinkingMsg{Title: "thinking"})

	if m.thinkingReasoning == "" {
		t.Fatal("expected bare thinking update to preserve previous reasoning")
	}

	m.thinkingWindow = nil
	_, _ = m.Update(ToolUseMsg{ID: "tool-1", ToolName: "read_file", Status: "success", Result: "ok"})

	if got := strings.Join(m.thinkingWindow, "\n"); !strings.Contains(got, "first visible thought") {
		t.Fatalf("expected thinking window to be restored after tool call, got %#v", m.thinkingWindow)
	}
}

func TestPermissionTagHiddenInDefaultMode(t *testing.T) {
	m := newTestModel(80)
	m.SetPermissionManager(permission.NewManager(permission.ModeDefault, nil))

	if got := m.renderPermissionModeTag(); got != "" {
		t.Fatalf("default permission tag = %q, want hidden", got)
	}
}

func TestPermissionTagVisibleInNonDefaultMode(t *testing.T) {
	m := newTestModel(80)
	m.SetPermissionManager(permission.NewManager(permission.ModePlan, nil))

	if got := m.renderPermissionModeTag(); !strings.Contains(got, "plan") {
		t.Fatalf("plan permission tag = %q, want visible plan tag", got)
	}
}

func TestPermissionModeIconNoVS16(t *testing.T) {
	m := newTestModel(80)
	for _, mode := range []string{"default", "acceptEdits", "plan", "auto", "yolo"} {
		if got := m.permissionModeIcon(mode); strings.Contains(got, "\ufe0f") {
			t.Fatalf("permissionModeIcon(%q) = %q, contains VS-16", mode, got)
		}
	}
}

func TestClearSessionResetsAllThinkingState(t *testing.T) {
	m := newTestModel(80)
	m.thinkingTitle = "thinking"
	m.thinkingReasoning = "old thought"
	m.thinkingCompleted = true
	m.thinkingCollapsed = true
	m.thinkingWindow = []string{"old thought"}
	m.thinkingPending = "old"
	m.spinnerFrame = 3
	m.spinnerStage = "thinking"
	m.swarmLine = "swarm"
	m.notice = "notice"
	m.contextWindowMax = 1000
	m.contextUsedTokens = 250
	m.lastRenderHeight = 12

	_ = m.ClearSession()

	if len(m.thinkingWindow) != 0 {
		t.Fatalf("expected thinking window to be cleared, got %#v", m.thinkingWindow)
	}
	if m.thinkingTitle != "" {
		t.Fatalf("expected thinking title to be cleared, got %q", m.thinkingTitle)
	}
	if m.thinkingReasoning != "" {
		t.Fatalf("expected thinking reasoning to be cleared, got %q", m.thinkingReasoning)
	}
	if m.thinkingCompleted {
		t.Fatal("expected thinking completed to be reset")
	}
	if !m.thinkingCollapsed {
		t.Fatal("expected thinking collapsed to reset to the default collapsed state")
	}
	if m.spinnerFrame != 0 {
		t.Fatalf("expected spinner frame to be reset, got %d", m.spinnerFrame)
	}
	if m.spinnerStage != "" {
		t.Fatalf("expected spinner stage to be reset, got %q", m.spinnerStage)
	}
	if m.swarmLine != "" {
		t.Fatalf("expected swarm line to be cleared, got %q", m.swarmLine)
	}
	if m.notice != "" {
		t.Fatalf("expected notice to be cleared, got %q", m.notice)
	}
	if m.thinkingPending != "" {
		t.Fatalf("expected thinking pending to be cleared, got %q", m.thinkingPending)
	}
	if m.contextWindowMax != 0 || m.contextUsedTokens != 0 {
		t.Fatalf("expected context fields to be reset, got %d/%d", m.contextUsedTokens, m.contextWindowMax)
	}
	if m.lastRenderHeight != 0 {
		t.Fatalf("expected lastRenderHeight to be reset, got %d", m.lastRenderHeight)
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

	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

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

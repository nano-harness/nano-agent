package bubbletea

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
)

// fsKey constructs a KeyPressMsg for a single ASCII rune.
func fsKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func newReadyFullscreenModel() *FullscreenModel {
	m := NewFullscreenModel("", "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(*FullscreenModel)
}

// TestFullscreen_TextareaAcceptsAllLetters verifies the core bug fix: vim-style
// scroll keys (j/k/g/G) and arrow keys must NOT be intercepted at the
// fullscreen model level, so the textarea can record them as regular text.
func TestFullscreen_TextareaAcceptsAllLetters(t *testing.T) {
	m := newReadyFullscreenModel()

	for _, r := range []rune{'h', 'e', 'l', 'l', 'o', 'j', 'k', 'g', 'G'} {
		updated, _ := m.Update(fsKey(r))
		m = updated.(*FullscreenModel)
	}

	got := m.textarea.Value()
	if !strings.Contains(got, "j") || !strings.Contains(got, "k") ||
		!strings.Contains(got, "g") || !strings.Contains(got, "G") {
		t.Fatalf("textarea did not capture vim-style letters; got %q", got)
	}
	if !strings.HasPrefix(got, "hello") {
		t.Fatalf("expected textarea to start with 'hello', got %q", got)
	}
}

// TestFullscreen_EnterSubmits verifies that pressing Enter submits the input
// and clears the textarea.
func TestFullscreen_EnterSubmits(t *testing.T) {
	m := newReadyFullscreenModel()

	var sent string
	m.BindOutbound(func(o eventsource.Outbound) error {
		sent = o.Text
		return nil
	})

	for _, r := range []rune("hi there") {
		updated, _ := m.Update(fsKey(r))
		m = updated.(*FullscreenModel)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*FullscreenModel)

	if sent != "hi there" {
		t.Fatalf("expected outbound text 'hi there', got %q", sent)
	}
	if strings.TrimSpace(m.textarea.Value()) != "" {
		t.Fatalf("textarea should be reset after submit, got %q", m.textarea.Value())
	}
	if m.messages.Len() != 1 || m.messages.Get(0).Role != "user" {
		t.Fatalf("expected a user message to be appended, got len=%d", m.messages.Len())
	}
}

// TestFullscreen_ShiftEnterInsertsNewline verifies that Shift+Enter inserts a
// newline into the textarea instead of submitting.
func TestFullscreen_ShiftEnterInsertsNewline(t *testing.T) {
	m := newReadyFullscreenModel()

	for _, r := range []rune("line1") {
		updated, _ := m.Update(fsKey(r))
		m = updated.(*FullscreenModel)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	m = updated.(*FullscreenModel)
	for _, r := range []rune("line2") {
		updated, _ := m.Update(fsKey(r))
		m = updated.(*FullscreenModel)
	}

	got := m.textarea.Value()
	if !strings.Contains(got, "\n") {
		t.Fatalf("expected textarea to contain a newline, got %q", got)
	}
	if m.messages.Len() != 0 {
		t.Fatalf("Shift+Enter must not submit, but got %d messages", m.messages.Len())
	}
}

// TestFullscreen_MouseWheelScrolls verifies that mouse-wheel messages drive
// the viewport (since vim-style keyboard scrolling was removed).
func TestFullscreen_MouseWheelScrolls(t *testing.T) {
	m := newReadyFullscreenModel()
	// Populate enough messages to make the viewport scrollable.
	for i := 0; i < 50; i++ {
		m.addUserMessage("message")
	}
	// Force sticky off and scroll to top so we can observe a scroll-down.
	m.viewport.ScrollToTop()
	before := m.viewport.ScrollOffset

	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	m = updated.(*FullscreenModel)

	if m.viewport.ScrollOffset <= before {
		t.Fatalf("expected mouse wheel down to advance scroll offset, got %d -> %d",
			before, m.viewport.ScrollOffset)
	}
}

func TestFullscreenView_RendersBannerArtWhenIdle(t *testing.T) {
	m := NewFullscreenModel("", "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m = updated.(*FullscreenModel)
	m.SetBannerArt("TEST BANNER")

	v := m.View()
	if !strings.Contains(v.Content, "TEST BANNER") {
		t.Fatalf("expected idle view to render banner art, got %q", v.Content)
	}
	if !strings.Contains(v.Content, "输入你的第一个问题") {
		t.Fatalf("expected idle view to render welcome hint, got %q", v.Content)
	}
}

func TestFullscreenView_HidesBannerOnceMessagesArrive(t *testing.T) {
	m := newReadyFullscreenModel()
	m.SetBannerArt("TEST BANNER")
	m.handleIncomingMessage(Message{Role: "assistant", Content: "hello"})

	area := m.renderMessageArea()
	if strings.Contains(area, "TEST BANNER") {
		t.Fatalf("expected message area to omit banner art after messages arrive, got %q", area)
	}
	if !strings.Contains(area, "hello") {
		t.Fatalf("expected message content to render, got %q", area)
	}
}

// TestFullscreen_DumpHistoryToScrollback verifies that dump output contains
// each message's content separated by blank lines.
func TestFullscreen_DumpHistoryToScrollback(t *testing.T) {
	m := newReadyFullscreenModel()
	m.addUserMessage("first message")
	m.addUserMessage("second message")

	dump := m.DumpHistoryPlainText()
	if !strings.Contains(dump, "first message") {
		t.Fatalf("dump missing first message: %q", dump)
	}
	if !strings.Contains(dump, "second message") {
		t.Fatalf("dump missing second message: %q", dump)
	}
	// Two messages separated by a blank line — there should be at least one
	// "\n\n" in the dump.
	if !strings.Contains(dump, "\n\n") {
		t.Fatalf("expected blank line between messages, got %q", dump)
	}
}

// TestFullscreen_CtrlCDumpsThenQuits verifies that pressing Ctrl+C triggers
// the dump+quit sequence and marks the model as exiting so View() drops to
// the main screen.
func TestFullscreen_CtrlCDumpsThenQuits(t *testing.T) {
	m := newReadyFullscreenModel()
	m.addUserMessage("bye world")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = updated.(*FullscreenModel)

	if !m.exiting {
		t.Fatal("expected model to be marked exiting after Ctrl+C")
	}
	if cmd == nil {
		t.Fatal("expected a quit command from Ctrl+C")
	}
	// View must drop alt-screen so the dumped content stays in scrollback.
	v := m.View()
	if v.AltScreen {
		t.Fatal("expected View().AltScreen=false after exiting")
	}
}

// TestFullscreen_LeftBracketTogglesDumpView verifies the `[` shortcut
// switches to scrollback view and the next keystroke restores the
// alt-screen.
func TestFullscreen_LeftBracketTogglesDumpView(t *testing.T) {
	m := newReadyFullscreenModel()
	m.addUserMessage("hello dump")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m = updated.(*FullscreenModel)

	if !m.dumpView {
		t.Fatal("expected dumpView=true after `[`")
	}
	if cmd == nil {
		t.Fatal("expected a tea.Printf command emitting the dump")
	}
	v := m.View()
	if v.AltScreen {
		t.Fatal("expected View().AltScreen=false during dump view")
	}

	// Any subsequent keystroke should restore the alt-screen view.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = updated.(*FullscreenModel)
	if m.dumpView {
		t.Fatal("expected dumpView=false after restoring key")
	}
	v = m.View()
	if !v.AltScreen {
		t.Fatal("expected View().AltScreen=true after restoring from dump view")
	}
}

// TestFullscreen_LeftBracketTypableWhenInputNonEmpty verifies that `[` falls
// through to the textarea when there is pending input so it remains typeable
// inside messages.
func TestFullscreen_LeftBracketTypableWhenInputNonEmpty(t *testing.T) {
	m := newReadyFullscreenModel()
	for _, r := range []rune("array") {
		updated, _ := m.Update(fsKey(r))
		m = updated.(*FullscreenModel)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m = updated.(*FullscreenModel)

	if m.dumpView {
		t.Fatal("`[` should not trigger dumpView when textarea is non-empty")
	}
	if !strings.Contains(m.textarea.Value(), "[") {
		t.Fatalf("expected `[` to be typed into textarea, got %q", m.textarea.Value())
	}
}

// TestFullscreen_ResizeRerendersMessagesWithFreshHeights verifies that a
// WindowSizeMsg causes all messages to be re-rendered so that msg.Height
// reflects the new terminal width rather than a stale value from the previous
// layout pass.
func TestFullscreen_ResizeRerendersMessagesWithFreshHeights(t *testing.T) {
	m := newReadyFullscreenModel()

	// Add a message with a long line so its wrapped height differs between
	// 80-column and 40-column renders.
	m.handleIncomingMessage(Message{Role: "assistant", Content: strings.Repeat("word ", 20)})

	// Capture the height at the initial 80-column width.
	first := m.messages.Last()
	if first == nil {
		t.Fatal("expected at least one message after handleIncomingMessage")
	}
	heightAt80 := first.Height
	if heightAt80 == 0 {
		t.Fatal("expected non-zero Height after initial render")
	}

	// Resize to half the width — wrapping should increase line count.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = updated.(*FullscreenModel)

	heightAt40 := m.messages.Last().Height
	if heightAt40 == 0 {
		t.Fatal("expected non-zero Height after resize render")
	}
	if heightAt40 <= heightAt80 {
		t.Fatalf("expected height to increase when terminal narrows (80→40): got %d→%d", heightAt80, heightAt40)
	}
}

// TestFullscreen_StatusBarIsTwoLines verifies the redesigned status bar
// occupies exactly two rows regardless of which optional indicators are
// populated: content plus the separator rule.
func TestFullscreen_StatusBarIsTwoLines(t *testing.T) {
	m := newReadyFullscreenModel()
	// Populate every optional indicator.
	m.tokenStatus = "输入 12 | 输出 34 | 总计 46"
	m.connectionState = "connected"
	m.connectionDetail = "openai"
	m.swarmLine = "swarm test"
	m.contextWindowMax = 200000
	m.contextUsedTokens = 12345
	m.cronIndicator = "cron"
	m.thinkingReasoning = "deeply pondering many things"

	bar := m.renderStatusBar()
	lines := strings.Split(bar, "\n")
	if len(lines) != 2 {
		t.Fatalf("status bar must be two lines, got %d: %q", len(lines), bar)
	}
	if !strings.Contains(lines[1], "─") {
		t.Fatalf("status bar second line must contain separator rule, got %q", lines[1])
	}
	if strings.Contains(bar, "48;5;236") {
		t.Fatalf("status bar must not render the old solid background color, got %q", bar)
	}
}

func TestFullscreen_StatusBarRuleUsesASCIIFallback(t *testing.T) {
	m := newReadyFullscreenModel()
	m.termCap = TermCapability{}

	lines := strings.Split(m.renderStatusBar(), "\n")
	if len(lines) != 2 {
		t.Fatalf("status bar must be two lines, got %d: %q", len(lines), lines)
	}
	if strings.Contains(lines[1], "─") || !strings.Contains(lines[1], "-") {
		t.Fatalf("status bar rule must use ASCII fallback without box drawing support, got %q", lines[1])
	}
}

func TestStatusBarWidth_NoOverflow(t *testing.T) {
	for _, status := range []string{
		strings.Repeat("working", 80),
		strings.Repeat("状态", 80),
		strings.Repeat("mix状态", 80),
	} {
		m := newReadyFullscreenModel()
		m.termWidth = 80
		m.contextWindowMax = 100000
		m.contextUsedTokens = 100000
		m.connectionState = "connected"
		m.status = status
		m.termCap = TermCapability{}

		for _, line := range strings.Split(m.renderStatusBar(), "\n") {
			if got := lipgloss.Width(line); got > m.termWidth {
				t.Fatalf("status bar width overflow for status %q: got %d, want <= %d; line=%q",
					status, got, m.termWidth, line)
			}
		}
	}
}

func TestWelcomePage_BannerSizeAdaptation(t *testing.T) {
	m := newReadyFullscreenModel()
	m.SetBannerArt("FULL BANNER")

	m.viewport.SetViewportHeight(30)
	full := m.renderWelcomePage()
	if !strings.Contains(full, "FULL BANNER") {
		t.Fatalf("expected full banner at height 30, got %q", full)
	}

	m.viewport.SetViewportHeight(15)
	compact := m.renderWelcomePage()
	if strings.Contains(compact, "FULL BANNER") {
		t.Fatalf("expected compact banner instead of full banner at height 15, got %q", compact)
	}
	if !strings.Contains(compact, "nano-agent") || !strings.Contains(compact, "|o.o|") {
		t.Fatalf("expected compact milktea ASCII banner at height 15, got %q", compact)
	}

	m.viewport.SetViewportHeight(14)
	tiny := m.renderWelcomePage()
	if strings.Contains(tiny, "FULL BANNER") || strings.Contains(tiny, "|o.o|") {
		t.Fatalf("expected text-only title below height 15, got %q", tiny)
	}
	if !strings.Contains(tiny, "nano-agent") {
		t.Fatalf("expected text-only nano-agent title below height 15, got %q", tiny)
	}
}

func TestFullscreen_FormatStatusForPhaseDone_UsesSafeChar(t *testing.T) {
	m := newReadyFullscreenModel()
	m.termCap = TermCapability{}

	want := SafeChar("success", m.termCap) + " 完成"
	if got := m.formatStatusForPhase(phaseDone, ""); got != want {
		t.Fatalf("expected safe completion status, got %q", got)
	}
}

// TestFullscreen_NarrowLayoutSelected verifies the layout engine switches
// to a narrow mode on small terminals so callers can render a compact UI
// instead of relying on the default 80-column geometry.
func TestFullscreen_NarrowLayoutSelected(t *testing.T) {
	m := NewFullscreenModel("", "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	m = updated.(*FullscreenModel)
	if m.layout == nil {
		t.Fatal("expected layout engine to be initialized")
	}
	if m.layout.Mode() != LayoutNarrow {
		t.Fatalf("expected LayoutNarrow at 50 cols, got %d", m.layout.Mode())
	}
}

// TestFullscreen_InlineThinkingBlock verifies that recorded thinking
// messages render with the safe-character thinking prefix and an
// indented quote bar, ensuring the reasoning shows up inline in the
// scrollback rather than only in the (now removed) status bar window.
func TestFullscreen_InlineThinkingBlock(t *testing.T) {
	m := newReadyFullscreenModel()
	m.recordThinkingMessage("思考完成 [3 字符]\n这是推理内容")
	last := m.messages.Last()
	if last == nil || last.Role != "thinking" {
		t.Fatal("expected thinking message to be appended")
	}
	if !last.Collapsed {
		t.Fatalf("expected new thinking message to default to collapsed")
	}
	if !strings.Contains(last.Rendered, "思考完成") {
		t.Fatalf("expected thinking summary in collapsed render, got %q", last.Rendered)
	}
	if strings.Contains(last.Rendered, "这是推理内容") {
		t.Fatalf("expected reasoning body to be hidden when collapsed, got %q", last.Rendered)
	}
	if !strings.Contains(last.Rendered, "Ctrl+T 展开") {
		t.Fatalf("expected expand hint in collapsed render, got %q", last.Rendered)
	}

	// Expand and re-render — the body should now be visible.
	last.Toggle()
	m.renderMessage(last)
	if !strings.Contains(last.Rendered, "这是推理内容") {
		t.Fatalf("expected reasoning body to be rendered inline when expanded, got %q", last.Rendered)
	}
	if !strings.Contains(last.Rendered, "Ctrl+T 折叠") {
		t.Fatalf("expected collapse hint in expanded render, got %q", last.Rendered)
	}
}

// TestRenderInlineThinkingBlock_Collapsed verifies the collapsed view
// hides the reasoning body and surfaces an explicit expand hint.
func TestRenderInlineThinkingBlock_Collapsed(t *testing.T) {
	cap := TermCapability{SupportsBoxDraw: true}
	out := renderInlineThinkingBlock("思考完成 [5 字符]\nhello world", 40, cap, true)
	if !strings.Contains(out, "思考完成") {
		t.Fatalf("expected summary in collapsed output, got %q", out)
	}
	if strings.Contains(out, "hello world") {
		t.Fatalf("expected body to be hidden when collapsed, got %q", out)
	}
	if !strings.Contains(out, "Ctrl+T 展开") {
		t.Fatalf("expected expand hint in collapsed output, got %q", out)
	}
}

// TestRenderInlineThinkingBlock_Expanded verifies the expanded view
// includes the full body and a collapse hint.
func TestRenderInlineThinkingBlock_Expanded(t *testing.T) {
	cap := TermCapability{SupportsBoxDraw: true}
	out := renderInlineThinkingBlock("思考完成 [5 字符]\nhello world", 40, cap, false)
	if !strings.Contains(out, "hello world") {
		t.Fatalf("expected body to be visible when expanded, got %q", out)
	}
	if !strings.Contains(out, "Ctrl+T 折叠") {
		t.Fatalf("expected collapse hint in expanded output, got %q", out)
	}
}

// TestCtrlT_TogglesLastThinkingMessage verifies the Ctrl+T shortcut only
// toggles the collapsed state of the **most recent** thinking message,
// leaving earlier blocks unchanged.
func TestCtrlT_TogglesLastThinkingMessage(t *testing.T) {
	m := newReadyFullscreenModel()
	m.recordThinkingMessage("思考完成 [3 字符]\n第一段推理")
	m.recordThinkingMessage("思考完成 [3 字符]\n第二段推理")

	first := m.messages.Get(0)
	second := m.messages.Get(1)
	if !first.Collapsed || !second.Collapsed {
		t.Fatalf("expected both thinking blocks to start collapsed, got first=%v second=%v",
			first.Collapsed, second.Collapsed)
	}

	// Press Ctrl+T — only the last block should expand.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m = updated.(*FullscreenModel)

	if !first.Collapsed {
		t.Fatalf("expected first thinking block to remain collapsed after Ctrl+T")
	}
	if second.Collapsed {
		t.Fatalf("expected last thinking block to be expanded after Ctrl+T")
	}
	if !strings.Contains(second.Rendered, "第二段推理") {
		t.Fatalf("expected last block body to be re-rendered after toggle, got %q", second.Rendered)
	}
	if strings.Contains(first.Rendered, "第一段推理") {
		t.Fatalf("expected first block body to stay hidden, got %q", first.Rendered)
	}

	// Press Ctrl+T again — last block should collapse back.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m = updated.(*FullscreenModel)
	if !second.Collapsed {
		t.Fatalf("expected last block to collapse again on second Ctrl+T")
	}
}

// TestFullscreen_StatusBarMarksMinimalLayout verifies the status bar
// renders a "[窄]" marker so users on very small terminals know the UI
// is in minimal-layout fallback mode.
func TestFullscreen_StatusBarMarksMinimalLayout(t *testing.T) {
	m := NewFullscreenModel("", "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 35, Height: 20})
	m = updated.(*FullscreenModel)
	if m.layout == nil || m.layout.Mode() != LayoutMinimal {
		t.Fatalf("expected LayoutMinimal at 35 cols, got %v", m.layout.Mode())
	}
	bar := m.renderStatusBar()
	if !strings.Contains(bar, "[窄]") {
		t.Fatalf("expected '[窄]' marker in minimal-layout status bar, got %q", bar)
	}

	// Wider terminals must not show the marker.
	updated2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m2 := updated2.(*FullscreenModel)
	if strings.Contains(m2.renderStatusBar(), "[窄]") {
		t.Fatalf("did not expect '[窄]' marker at 80 cols")
	}
}

// TestFullscreen_ThinkingComplete_EmptyReasoningSkipsBlock verifies that a
// complete thinking event with no accumulated reasoning content does NOT
// produce a "思考完成 [0 字符]" placeholder in the message stream.
func TestFullscreen_ThinkingComplete_EmptyReasoningSkipsBlock(t *testing.T) {
	m := newReadyFullscreenModel()
	before := m.messages.Len()
	m.handleThinkingMessage(ThinkingMsg{Metadata: map[string]interface{}{"is_complete": true}})
	if m.messages.Len() != before {
		t.Fatalf("empty complete thinking event should not append a message; got len=%d", m.messages.Len())
	}
}

// TestFullscreen_ThinkingComplete_DeltaOnlyAccumulates verifies the
// "delta-only complete" case: deltas arrive without a cumulative Reasoning
// field, then a complete event arrives carrying only is_complete (no
// Reasoning, no ReasoningDelta). The previously accumulated reasoning must
// be persisted to the MessageStore.
func TestFullscreen_ThinkingComplete_DeltaOnlyAccumulates(t *testing.T) {
	m := newReadyFullscreenModel()
	m.handleThinkingMessage(ThinkingMsg{Title: "thinking", ReasoningDelta: "alpha "})
	m.handleThinkingMessage(ThinkingMsg{ReasoningDelta: "beta"})
	m.handleThinkingMessage(ThinkingMsg{Metadata: map[string]interface{}{"is_complete": true}})

	last := m.messages.Last()
	if last == nil || last.Role != "thinking" {
		t.Fatalf("expected a thinking message in store after delta-only complete; got %+v", last)
	}
	if !strings.Contains(last.Content, "alpha") || !strings.Contains(last.Content, "beta") {
		t.Fatalf("expected accumulated reasoning in stored thinking message, got %q", last.Content)
	}
}

// TestFullscreen_RoleBorderColors_AlignWithInlinePalette guards the
// fullscreen palette. The fullscreen (`--milktea`) AI border uses a
// dedicated softer hue (151) tuned for WCAG AA contrast on the dark
// status bar background, so it intentionally diverges from the inline
// (`--tea`) `colorAssistant` constant. Tool border still mirrors the
// inline palette since the gray hue meets contrast guidelines in both
// surfaces.
func TestFullscreen_RoleBorderColors_AlignWithInlinePalette(t *testing.T) {
	if got, want := fsBorderColorForRole("assistant"), fsColorAIBorder; got != want {
		t.Fatalf("assistant border should be fsColorAIBorder (%s), got %s", want, got)
	}
	if got, want := fsBorderColorForRole("assistant_stream"), fsColorAIBorder; got != want {
		t.Fatalf("assistant_stream border should be fsColorAIBorder (%s), got %s", want, got)
	}
	if got, want := fsBorderColorForRole("tool"), colorTool; got != want {
		t.Fatalf("tool border should match colorTool (%s), got %s", want, got)
	}
}

// TestFullscreen_RoleLabel_PresentEvenWithoutRichGlyphs verifies that role
// identification does not rely solely on color: an ASCII role label is
// always available for every role so a low-color or color-blind user can
// still tell who's speaking.
func TestFullscreen_RoleLabel_PresentEvenWithoutRichGlyphs(t *testing.T) {
	cases := map[string]string{
		"user":             "You",
		"assistant":        "Assistant",
		"assistant_stream": "Assistant",
		"tool":             "Tool",
		"thinking":         "Thinking",
		"error":            "Error",
		"system":           "System",
	}
	for role, want := range cases {
		if got := fsLabelForRole(role); got != want {
			t.Fatalf("fsLabelForRole(%q) = %q, want %q", role, got, want)
		}
	}
}

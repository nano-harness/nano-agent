package bubbletea_test

import (
	"reflect"
	"strings"
	"testing"

	ui "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestThinkingWindow_AccumulatesDeltas(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	sendThinking(m, "one ", "two")

	if got, want := m.ThinkingWindow(), []string{"one two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ThinkingWindow() = %#v, want %#v", got, want)
	}
}

func TestThinkingWindow_SoftWrapsAndKeepsLastThree(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.ThinkingMsg{Title: "thinking", ReasoningDelta: strings.Repeat("abcdefghij ", 12)})

	got := m.ThinkingWindow()
	if len(got) != 3 {
		t.Fatalf("ThinkingWindow() = %#v, want three lines", got)
	}
	for _, line := range got {
		if width := xansi.StringWidth(line); width > 20 {
			t.Fatalf("expected wrapped line width <= 20, got %d for %q", width, line)
		}
	}
}

func TestThinkingWindow_TrimsWhitespace(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	sendThinking(m, "  first\nline  ")

	if got, want := m.ThinkingWindow(), []string{"first line"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ThinkingWindow() = %#v, want %#v", got, want)
	}
}

func TestThinkingWindow_EmptyLineIgnored(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	sendThinking(m, "kept", "   \n  ")

	if got, want := m.ThinkingWindow(), []string{"kept"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ThinkingWindow() = %#v, want %#v", got, want)
	}
}

// TestThinkingWindow_CompletionPreviewRemainsAfterAssistantStream verifies
// that after a complete thinking event the streaming preview is replaced
// with a single-line "思考完成 [N 字符]" hint that survives an immediately
// following assistant_stream message. The hint also mentions Ctrl+T so the
// user knows the full reasoning can be expanded.
func TestThinkingWindow_CompletionPreviewRemainsAfterAssistantStream(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.ThinkingMsg{Title: "thinking", ReasoningDelta: "alpha"})
	_, _ = m.Update(ui.ThinkingMsg{Reasoning: "alpha", Metadata: map[string]interface{}{"is_complete": true}})
	_, _ = m.Update(ui.Message{Role: "assistant_stream", Content: "answer\n"})

	win := m.ThinkingWindow()
	if len(win) == 0 {
		t.Fatal("expected completion preview to remain after assistant_stream")
	}
	if !strings.Contains(win[0], "思考完成") || !strings.Contains(win[0], "Ctrl+T") {
		t.Fatalf("preview should mention 思考完成 and Ctrl+T, got %q", win[0])
	}
}

// TestThinkingWindow_EmptyCompleteDoesNotShowPreview verifies that an
// is_complete event with no reasoning content does not render a stray
// "思考完成 [0 字符]" preview.
func TestThinkingWindow_EmptyCompleteDoesNotShowPreview(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.ThinkingMsg{Title: "thinking", Metadata: map[string]interface{}{"is_complete": true}})

	if got := m.ThinkingWindow(); len(got) != 0 {
		t.Fatalf("expected empty thinking window for empty complete event, got %#v", got)
	}
}

func sendThinking(m *ui.Model, lines ...string) {
	for _, line := range lines {
		_, _ = m.Update(ui.ThinkingMsg{Title: "thinking", ReasoningDelta: line})
	}
}

package bubbletea

import (
	"strings"
	"testing"
)

func TestThinkingMsg_FullReasoningWrittenToScrollback(t *testing.T) {
	m := newTestModel(36)
	reasoning := "第一步：分析问题。\nSecond step: verify wrapping keeps all content.\n最终答案前检查。"
	_, _ = m.Update(ThinkingMsg{Title: "thinking", Reasoning: reasoning, Metadata: map[string]interface{}{"is_complete": true}})

	if m.messages.Len() == 0 {
		t.Fatal("expected thinking message in MessageStore")
	}
	content := m.messages.Last().Content
	for _, want := range []string{"思考完成", "第一步：分析问题。", "Second step", "verify wrapping", "最终答案前检查。"} {
		if !strings.Contains(content, want) {
			t.Fatalf("thinking message missing %q:\n%s", want, content)
		}
	}
}

// TestThinkingMsg_FastReasoningThenQuickReply guards the end-to-end fast
// path: a single reasoning delta, an immediate complete event, then an
// immediate assistant reply. The full reasoning must still land in the
// scrollback (so Ctrl+T can expand it later), and the completion preview
// must remain visible alongside the streaming reply.
func TestThinkingMsg_FastReasoningThenQuickReply(t *testing.T) {
	m := newTestModel(40)
	reasoning := "fast thought before answer"

	_, _ = m.Update(ThinkingMsg{Title: "thinking", ReasoningDelta: reasoning})
	_, _ = m.Update(ThinkingMsg{Reasoning: reasoning, Metadata: map[string]interface{}{"is_complete": true}})
	_, _ = m.Update(Message{Role: "assistant_stream", Content: "ok\n"})

	// The full reasoning must be persisted to the message store so the
	// user can scroll back to it and toggle it with Ctrl+T.
	var thinkingMsg string
	for i := 0; i < m.messages.Len(); i++ {
		msg := m.messages.Get(i)
		if msg.Role == "thinking" {
			thinkingMsg = msg.Content
			break
		}
	}
	if thinkingMsg == "" {
		t.Fatal("expected thinking message to be persisted to MessageStore")
	}
	if !strings.Contains(thinkingMsg, reasoning) {
		t.Fatalf("thinking message missing full reasoning body, got %q", thinkingMsg)
	}
	if !strings.Contains(thinkingMsg, "思考完成") {
		t.Fatalf("thinking message missing completion header, got %q", thinkingMsg)
	}

	// The completion preview should remain in the thinking window after
	// the assistant stream arrives, so the user notices reasoning
	// happened even on the fast path.
	if len(m.thinkingWindow) == 0 || !strings.Contains(m.thinkingWindow[0], "思考完成") {
		t.Fatalf("expected completion preview to remain in thinking window, got %#v", m.thinkingWindow)
	}
}

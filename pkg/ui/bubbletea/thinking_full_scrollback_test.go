package bubbletea

import (
	"strings"
	"testing"
)

func TestThinkingMsg_FullReasoningWrittenToScrollback(t *testing.T) {
	m := newTestModel(36)
	reasoning := "第一步：分析问题。\nSecond step: verify wrapping keeps all content.\n最终答案前检查。"
	_, _ = m.Update(ThinkingMsg{Title: "thinking", Reasoning: reasoning, Metadata: map[string]interface{}{"is_complete": true}})

	scrollback := strings.Join(m.lines, "\n")
	for _, want := range []string{"思考完成", "第一步：分析问题。", "Second step", "verify wrapping", "最终答案前检查。"} {
		if !strings.Contains(scrollback, want) {
			t.Fatalf("scrollback missing %q:\n%s", want, scrollback)
		}
	}
}

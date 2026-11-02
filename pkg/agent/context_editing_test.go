package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

// buildToolTurn builds one conversation turn: user message, assistant tool
// call, and the tool result. toolContent may be empty to skip the tool result.
func buildToolTurn(user, toolCallID, toolName, toolContent string) []llm.Message {
	msgs := []llm.Message{
		{Role: "user", Content: user},
		{Role: "assistant", Content: "calling tool", ToolCalls: []tools.ToolCall{
			{ID: toolCallID, Name: toolName, Arguments: map[string]interface{}{"path": "/tmp/x"}},
		}},
	}
	if toolContent != "" {
		msgs = append(msgs, llm.Message{Role: "tool", Content: toolContent, ToolCallID: toolCallID})
	}
	return msgs
}

func TestContextEditor_ShouldEdit(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "hi"}}

	tests := []struct {
		name          string
		maxTokens     int
		triggerRatio  float64
		messages      []llm.Message
		currentTokens int
		want          bool
	}{
		{name: "empty history", maxTokens: 1000, triggerRatio: 0.5, messages: nil, currentTokens: 900, want: false},
		{name: "below trigger", maxTokens: 1000, triggerRatio: 0.5, messages: msgs, currentTokens: 400, want: false},
		{name: "exactly at trigger", maxTokens: 1000, triggerRatio: 0.5, messages: msgs, currentTokens: 500, want: false},
		{name: "above trigger", maxTokens: 1000, triggerRatio: 0.5, messages: msgs, currentTokens: 501, want: true},
		{name: "zero budget never edits", maxTokens: 0, triggerRatio: 0.5, messages: msgs, currentTokens: 100, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewContextEditorWithConfig(tt.maxTokens, tt.triggerRatio, 4, 3)
			if got := e.ShouldEdit(tt.messages, tt.currentTokens); got != tt.want {
				t.Fatalf("ShouldEdit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContextEditor_EditContext(t *testing.T) {
	bigResult := strings.Repeat("x", 512)

	tests := []struct {
		name            string
		staleAfterTurns int
		keepRecentTurns int
		numTurns        int // turns with tool results, in order oldest→newest
		wantCleared     int
		wantClearedTurn []bool // per-turn: whether its tool result should be cleared
	}{
		{
			name:            "empty history",
			staleAfterTurns: 4, keepRecentTurns: 3,
			numTurns: 0, wantCleared: 0, wantClearedTurn: nil,
		},
		{
			name:            "below stale threshold",
			staleAfterTurns: 4, keepRecentTurns: 3,
			numTurns: 4, wantCleared: 0, wantClearedTurn: []bool{false, false, false, false},
		},
		{
			name:            "exactly at stale boundary",
			staleAfterTurns: 4, keepRecentTurns: 3,
			numTurns: 5, wantCleared: 1, wantClearedTurn: []bool{true, false, false, false, false},
		},
		{
			name:            "many turns clears all stale",
			staleAfterTurns: 4, keepRecentTurns: 3,
			numTurns: 8, wantCleared: 4, wantClearedTurn: []bool{true, true, true, true, false, false, false, false},
		},
		{
			name:            "keep-recent dominates when larger than stale threshold",
			staleAfterTurns: 2, keepRecentTurns: 5,
			numTurns: 7, wantCleared: 2, wantClearedTurn: []bool{true, true, false, false, false, false, false},
		},
		{
			name:            "stale threshold dominates when larger than keep-recent",
			staleAfterTurns: 6, keepRecentTurns: 2,
			numTurns: 8, wantCleared: 2, wantClearedTurn: []bool{true, true, false, false, false, false, false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewContextEditorWithConfig(100000, 0.5, tt.staleAfterTurns, tt.keepRecentTurns)

			messages := []llm.Message{{Role: "system", Content: "sys"}}
			var toolIdx []int
			for i := 0; i < tt.numTurns; i++ {
				turn := buildToolTurn(fmt.Sprintf("task %d", i), fmt.Sprintf("call-%d", i), "read_file", bigResult)
				toolIdx = append(toolIdx, len(messages)+2)
				messages = append(messages, turn...)
			}

			edited, cleared := e.EditContext(messages)
			if cleared != tt.wantCleared {
				t.Fatalf("cleared = %d, want %d", cleared, tt.wantCleared)
			}
			if len(edited) != len(messages) {
				t.Fatalf("message count changed: %d → %d (edits must not alter structure)", len(messages), len(edited))
			}
			if edited[0].Role != "system" || edited[0].Content != "sys" {
				t.Fatalf("system message must be preserved, got %+v", edited[0])
			}
			for i, idx := range toolIdx {
				gotCleared := strings.HasPrefix(edited[idx].Content, "[tool result cleared:")
				if gotCleared != tt.wantClearedTurn[i] {
					t.Errorf("turn %d cleared = %v, want %v (content: %.40q)", i, gotCleared, tt.wantClearedTurn[i], edited[idx].Content)
				}
			}
		})
	}
}

func TestContextEditor_EditContext_PlaceholderAndIdempotency(t *testing.T) {
	e := NewContextEditorWithConfig(100000, 0.5, 2, 1)

	messages := []llm.Message{{Role: "system", Content: "sys"}}
	for i := 0; i < 4; i++ {
		messages = append(messages, buildToolTurn(fmt.Sprintf("t%d", i), fmt.Sprintf("call-%d", i), "read_file", strings.Repeat("y", 300))...)
	}

	edited, cleared := e.EditContext(messages)
	// minAge = max(stale=2, keep=1) = 2 → last editable turn index = 4-1-2 = 1 → turns 0 and 1
	if cleared != 2 {
		t.Fatalf("cleared = %d, want 2", cleared)
	}

	// Placeholder format: "[tool result cleared: <tool_name>, <n> bytes]"
	// Turn 0's tool result: system(0), user(1), assistant(2), tool(3).
	want := fmt.Sprintf("[tool result cleared: read_file, %d bytes]", 300)
	if edited[3].Content != want {
		t.Fatalf("placeholder = %q, want %q", edited[3].Content, want)
	}

	// Idempotency: a second pass must clear nothing and keep bytes identical.
	again, clearedAgain := e.EditContext(edited)
	if clearedAgain != 0 {
		t.Fatalf("second pass cleared %d results, want 0 (not idempotent)", clearedAgain)
	}
	for i := range edited {
		if again[i].Content != edited[i].Content {
			t.Fatalf("message %d changed on second pass: %q → %q", i, edited[i].Content, again[i].Content)
		}
	}
}

func TestContextEditor_EditContext_EdgeCases(t *testing.T) {
	big := strings.Repeat("z", 256)

	tests := []struct {
		name        string
		messages    []llm.Message
		wantCleared int
	}{
		{
			name:        "nil messages",
			messages:    nil,
			wantCleared: 0,
		},
		{
			name: "no user messages means no stable turn boundary",
			messages: []llm.Message{
				{Role: "system", Content: "sys"},
				{Role: "assistant", Content: "a"},
				{Role: "tool", Content: big, ToolCallID: "c1"},
			},
			wantCleared: 0,
		},
		{
			name: "tiny result shorter than placeholder is never grown",
			messages: func() []llm.Message {
				msgs := []llm.Message{{Role: "system", Content: "sys"}}
				for i := 0; i < 5; i++ {
					content := big
					if i == 0 {
						content = "ok" // 2 bytes, shorter than any placeholder
					}
					msgs = append(msgs, buildToolTurn(fmt.Sprintf("t%d", i), fmt.Sprintf("c%d", i), "shell", content)...)
				}
				return msgs
			}(),
			wantCleared: 0, // only turn 0 is stale (minAge=4 of 5 turns); it is too small to clear
		},
		{
			name: "already cleared placeholder is skipped",
			messages: func() []llm.Message {
				msgs := []llm.Message{{Role: "system", Content: "sys"}}
				for i := 0; i < 5; i++ {
					content := big
					if i == 0 {
						content = "[tool result cleared: shell, 999 bytes]"
					}
					msgs = append(msgs, buildToolTurn(fmt.Sprintf("t%d", i), fmt.Sprintf("c%d", i), "shell", content)...)
				}
				return msgs
			}(),
			wantCleared: 0, // only turn 0 is stale and it was already cleared
		},
		{
			name: "unknown tool name falls back to placeholder with unknown",
			messages: func() []llm.Message {
				msgs := []llm.Message{{Role: "system", Content: "sys"}}
				for i := 0; i < 5; i++ {
					turn := buildToolTurn(fmt.Sprintf("t%d", i), fmt.Sprintf("c%d", i), "shell", big)
					if i == 0 {
						turn[2].ToolCallID = "missing-id" // no matching assistant tool call
					}
					msgs = append(msgs, turn...)
				}
				return msgs
			}(),
			wantCleared: 1, // only turn 0 is stale; cleared with tool name "unknown"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewContextEditorWithConfig(100000, 0.5, 4, 3)
			edited, cleared := e.EditContext(tt.messages)
			if cleared != tt.wantCleared {
				t.Fatalf("cleared = %d, want %d", cleared, tt.wantCleared)
			}
			if len(edited) != len(tt.messages) {
				t.Fatalf("message count changed: %d → %d", len(tt.messages), len(edited))
			}
		})
	}
}

func TestNewContextEditor_ConfigDefaultsAndOverrides(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		wantTrigger float64
		wantStale   int
		wantKeep    int
	}{
		{name: "nil config uses defaults", cfg: nil, wantTrigger: defaultEditTriggerRatio, wantStale: defaultEditStaleTurns, wantKeep: defaultEditKeepRecentTurns},
		{
			name: "explicit overrides win",
			cfg: &config.Config{ContextConfig: config.ContextConfig{
				EditTriggerRatio:    0.4,
				EditStaleTurns:      7,
				EditKeepRecentTurns: 2,
			}},
			wantTrigger: 0.4, wantStale: 7, wantKeep: 2,
		},
		{
			name: "invalid values fall back to defaults",
			cfg: &config.Config{ContextConfig: config.ContextConfig{
				EditTriggerRatio:    1.5,
				EditStaleTurns:      -1,
				EditKeepRecentTurns: 0,
			}},
			wantTrigger: defaultEditTriggerRatio, wantStale: defaultEditStaleTurns, wantKeep: defaultEditKeepRecentTurns,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewContextEditor(tt.cfg, nil)
			if e.triggerRatio != tt.wantTrigger || e.staleAfterTurns != tt.wantStale || e.keepRecentTurns != tt.wantKeep {
				t.Fatalf("got (trigger=%v stale=%d keep=%d), want (%v %d %d)",
					e.triggerRatio, e.staleAfterTurns, e.keepRecentTurns, tt.wantTrigger, tt.wantStale, tt.wantKeep)
			}
		})
	}
}

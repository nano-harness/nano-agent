package llm

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/stretchr/testify/assert"
)

func TestBlocksToText(t *testing.T) {
	tests := []struct {
		name   string
		blocks []ReasoningBlock
		want   string
	}{
		{"nil blocks", nil, ""},
		{"empty blocks", []ReasoningBlock{}, ""},
		{"single thinking", []ReasoningBlock{{Type: ReasoningBlockThinking, Text: "hello"}}, "hello"},
		{"single redacted", []ReasoningBlock{{Type: ReasoningBlockRedactedThinking, Data: "abc123"}}, "[redacted]"},
		{"mixed blocks", []ReasoningBlock{
			{Type: ReasoningBlockThinking, Text: "first thought"},
			{Type: ReasoningBlockRedactedThinking, Data: "encrypted"},
			{Type: ReasoningBlockThinking, Text: "second thought"},
		}, "first thought\n[redacted]\nsecond thought"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BlocksToText(tt.blocks)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReasoningBlock_IsRedacted(t *testing.T) {
	assert.True(t, ReasoningBlock{Type: ReasoningBlockRedactedThinking}.IsRedacted())
	assert.False(t, ReasoningBlock{Type: ReasoningBlockThinking}.IsRedacted())
}

func TestReasoningBlock_Empty(t *testing.T) {
	assert.True(t, ReasoningBlock{Type: ReasoningBlockThinking, Text: ""}.Empty())
	assert.False(t, ReasoningBlock{Type: ReasoningBlockThinking, Text: "hi"}.Empty())
	assert.True(t, ReasoningBlock{Type: ReasoningBlockRedactedThinking, Data: ""}.Empty())
	assert.False(t, ReasoningBlock{Type: ReasoningBlockRedactedThinking, Data: "abc"}.Empty())
	assert.True(t, ReasoningBlock{Type: ""}.Empty()) // unknown type
}

func TestFilterTrailingThinkingFromLastAssistant(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		wantLen  int
		checkFn  func(t *testing.T, msgs []Message)
	}{
		{
			name:     "empty messages",
			messages: []Message{},
			wantLen:  0,
		},
		{
			name: "assistant with content keeps reasoning",
			messages: []Message{
				{Role: "assistant", Content: "hello", ReasoningBlocks: []ReasoningBlock{
					{Type: ReasoningBlockThinking, Text: "thinking..."},
				}},
			},
			wantLen: 1,
			checkFn: func(t *testing.T, msgs []Message) {
				assert.Len(t, msgs[0].ReasoningBlocks, 1)
			},
		},
		{
			name: "assistant with tool calls keeps reasoning",
			messages: []Message{
				{Role: "assistant", ReasoningBlocks: []ReasoningBlock{
					{Type: ReasoningBlockThinking, Text: "thinking..."},
				}, ToolCalls: []tools.ToolCall{{ID: "1", Name: "test"}}},
			},
			wantLen: 1,
			checkFn: func(t *testing.T, msgs []Message) {
				assert.Len(t, msgs[0].ReasoningBlocks, 1)
			},
		},
		{
			name: "thinking-only last assistant gets cleared",
			messages: []Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", ReasoningBlocks: []ReasoningBlock{
					{Type: ReasoningBlockThinking, Text: "thinking only"},
				}},
			},
			wantLen: 2,
			checkFn: func(t *testing.T, msgs []Message) {
				assert.Nil(t, msgs[1].ReasoningBlocks)
			},
		},
		{
			name: "non-last assistant thinking-only is not touched",
			messages: []Message{
				{Role: "assistant", ReasoningBlocks: []ReasoningBlock{
					{Type: ReasoningBlockThinking, Text: "old thinking"},
				}},
				{Role: "user", Content: "follow up"},
				{Role: "assistant", Content: "response"},
			},
			wantLen: 3,
			checkFn: func(t *testing.T, msgs []Message) {
				// First assistant still has its thinking (it's not the last)
				assert.Len(t, msgs[0].ReasoningBlocks, 1)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterTrailingThinkingFromLastAssistant(tt.messages)
			assert.Len(t, result, tt.wantLen)
			if tt.checkFn != nil {
				tt.checkFn(t, result)
			}
		})
	}
}

func TestFilterOrphanedThinkingOnlyMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		wantLen  int
	}{
		{
			name:     "empty",
			messages: []Message{},
			wantLen:  0,
		},
		{
			name: "orphan thinking-only removed",
			messages: []Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", ReasoningBlocks: []ReasoningBlock{
					{Type: ReasoningBlockThinking, Text: "orphan"},
				}},
				{Role: "user", Content: "next"},
			},
			wantLen: 2, // orphan removed
		},
		{
			name: "thinking-only with following tool_result kept",
			messages: []Message{
				{Role: "assistant", ReasoningBlocks: []ReasoningBlock{
					{Type: ReasoningBlockThinking, Text: "thinking before tool"},
				}},
				{Role: "tool", Content: "tool result"},
			},
			wantLen: 2, // kept because followed by tool result
		},
		{
			name: "thinking-only at end is orphan",
			messages: []Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", ReasoningBlocks: []ReasoningBlock{
					{Type: ReasoningBlockThinking, Text: "end thinking"},
				}},
			},
			wantLen: 1, // orphan removed
		},
		{
			name: "normal messages unchanged",
			messages: []Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "hello", ReasoningBlocks: []ReasoningBlock{
					{Type: ReasoningBlockThinking, Text: "has content too"},
				}},
			},
			wantLen: 2, // not orphan because has content
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterOrphanedThinkingOnlyMessages(tt.messages)
			assert.Len(t, result, tt.wantLen)
		})
	}
}

func TestStripSignatureBearingBlocks(t *testing.T) {
	messages := []Message{
		{
			Role:    "assistant",
			Content: "hello",
			ReasoningBlocks: []ReasoningBlock{
				{Type: ReasoningBlockThinking, Text: "thought 1", Signature: "sig1"},
				{Type: ReasoningBlockRedactedThinking, Data: "encrypted_data"},
				{Type: ReasoningBlockThinking, Text: "thought 2", Signature: "sig2"},
			},
		},
		{
			Role:    "user",
			Content: "question",
		},
	}

	result := StripSignatureBearingBlocks(messages)

	// User message unchanged
	assert.Equal(t, "question", result[1].Content)
	assert.Nil(t, result[1].ReasoningBlocks)

	// Assistant message: thinking blocks kept with cleared signatures, redacted removed
	blocks := result[0].ReasoningBlocks
	assert.Len(t, blocks, 2)
	assert.Equal(t, ReasoningBlockThinking, blocks[0].Type)
	assert.Equal(t, "thought 1", blocks[0].Text)
	assert.Equal(t, "", blocks[0].Signature)
	assert.Equal(t, ReasoningBlockThinking, blocks[1].Type)
	assert.Equal(t, "thought 2", blocks[1].Text)
	assert.Equal(t, "", blocks[1].Signature)
}

func TestHasSignature(t *testing.T) {
	assert.False(t, HasSignature(nil))
	assert.False(t, HasSignature([]ReasoningBlock{{Type: ReasoningBlockThinking, Text: "hi"}}))
	assert.True(t, HasSignature([]ReasoningBlock{{Type: ReasoningBlockThinking, Text: "hi", Signature: "abc"}}))
}

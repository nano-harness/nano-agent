package llm

import (
	"encoding/json"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

type streamToolCallBuilder struct {
	ID          string
	Name        string
	Arguments   strings.Builder
	NameCounted bool
}

type StreamAssembler struct {
	responseContent  strings.Builder
	reasoningContent strings.Builder
	toolCalls        map[int]*streamToolCallBuilder
	toolCallOrder    []int
}

func NewStreamAssembler() *StreamAssembler {
	return &StreamAssembler{
		toolCalls: make(map[int]*streamToolCallBuilder),
	}
}

func (a *StreamAssembler) AddContent(content string) {
	a.responseContent.WriteString(content)
}

func (a *StreamAssembler) AddReasoning(reasoning string) {
	a.reasoningContent.WriteString(reasoning)
}

func (a *StreamAssembler) Content() string {
	return a.responseContent.String()
}

func (a *StreamAssembler) ContentLen() int {
	return a.responseContent.Len()
}

func (a *StreamAssembler) Reasoning() string {
	return a.reasoningContent.String()
}

func (a *StreamAssembler) AddToolCallDelta(index int, id, name, arguments string) (nameStarted bool) {
	builder := a.ensureToolCall(index)
	if builder.ID == "" && id != "" {
		builder.ID = id
	}
	if name != "" && builder.Name == "" {
		builder.Name = name
		if !builder.NameCounted {
			builder.NameCounted = true
			nameStarted = true
		}
	}
	if arguments != "" {
		builder.Arguments.WriteString(arguments)
	}
	return nameStarted
}

func (a *StreamAssembler) ToolCallName(index int) string {
	if builder := a.toolCalls[index]; builder != nil {
		return builder.Name
	}
	return ""
}

func (a *StreamAssembler) FinalizeToolCalls(toolRequiresParameters func(string) bool) []tools.ToolCall {
	toolCalls := make([]tools.ToolCall, 0, len(a.toolCallOrder))
	for _, idx := range a.toolCallOrder {
		builder := a.toolCalls[idx]
		if builder == nil {
			continue
		}
		if builder.Name == "" {
			logger.Warnf("第%d个工具调用缺少名称，已跳过", idx)
			continue
		}

		toolCall := tools.ToolCall{ID: builder.ID, Name: builder.Name}
		var args map[string]interface{}
		argumentsStr := builder.Arguments.String()
		logger.Debugf("Raw arguments for tool %s: %s", toolCall.Name, argumentsStr)
		if strings.TrimSpace(argumentsStr) != "" {
			if err := json.Unmarshal([]byte(argumentsStr), &args); err != nil {
				logger.Warnf("Failed to parse tool call arguments for %s (raw: %s): %v", toolCall.Name, argumentsStr, err)
				args = make(map[string]interface{})
			}
		} else {
			if toolRequiresParameters != nil && toolRequiresParameters(toolCall.Name) {
				logger.Warnf("Tool %s requires parameters but none provided; proceeding with empty arguments", toolCall.Name)
			}
			args = make(map[string]interface{})
		}
		toolCall.Arguments = args
		toolCalls = append(toolCalls, toolCall)
		logger.Debugf("Successfully parsed tool call: %s with args: %v", toolCall.Name, toolCall.Arguments)
	}
	return toolCalls
}

func (a *StreamAssembler) ensureToolCall(index int) *streamToolCallBuilder {
	if builder := a.toolCalls[index]; builder != nil {
		return builder
	}
	builder := &streamToolCallBuilder{}
	a.toolCalls[index] = builder
	a.toolCallOrder = append(a.toolCallOrder, index)
	return builder
}

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/assert"
)

// AssertEventExists 断言在 Suite 收集到的事件中存在指定类型的事件。
func (s *AgentTestSuite) AssertEventExists(eventType event.EventType) {
	s.T().Helper()
	assert.Truef(s.T(), eventExists(s.Events, eventType), "expected event type %s to exist", eventType)
}

// AssertNoEvent 断言不存在指定类型的事件。
func (s *AgentTestSuite) AssertNoEvent(eventType event.EventType) {
	s.T().Helper()
	assert.Falsef(s.T(), eventExists(s.Events, eventType), "did not expect event type %s", eventType)
}

// AssertEventSequence 断言事件类型按给定顺序至少出现一次（允许出现其他事件）。
func (s *AgentTestSuite) AssertEventSequence(types ...event.EventType) {
	s.T().Helper()
	assert.Truef(s.T(), eventSequenceExists(s.Events, types), "expected event sequence %+v to appear", types)
}

// AssertToolCalled 断言指定工具至少被调用一次。
func (s *AgentTestSuite) AssertToolCalled(toolName string) {
	s.T().Helper()
	assert.Truef(s.T(), toolCalled(s.Events, toolName), "expected tool %q to be called", toolName)
}

// AssertToolCallCount 断言指定工具被调用的次数（按唯一 ToolCall.ID 去重）。
func (s *AgentTestSuite) AssertToolCallCount(toolName string, count int) {
	s.T().Helper()
	actual := toolCallCount(s.Events, toolName)
	assert.Equalf(s.T(), count, actual, "unexpected call count for tool %q", toolName)
}

// AssertToolCalledWithParams 断言某次工具调用包含给定参数键值对（JSON 级别匹配）。
func (s *AgentTestSuite) AssertToolCalledWithParams(toolName string, expected map[string]interface{}) {
	s.T().Helper()
	assert.Truef(s.T(), toolCalledWithParams(s.Events, toolName, expected), "expected tool %q to be called with params %v", toolName, expected)
}

// AssertContentContains 断言 content 型事件中包含指定子串。
func (s *AgentTestSuite) AssertContentContains(substring string) {
	s.T().Helper()
	assert.Truef(s.T(), contentContains(s.Events, substring), "expected content to contain %q", substring)
}

// AssertFileCreated 断言在工作目录下指定路径的文件存在。
func (s *AgentTestSuite) AssertFileCreated(path string) {
	s.T().Helper()
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(s.WorkDir, path)
	}
	_, err := os.Stat(fullPath)
	assert.NoErrorf(s.T(), err, "expected file %s to exist", fullPath)
}

// AssertFileContains 断言文件内容包含指定子串。
func (s *AgentTestSuite) AssertFileContains(path, substring string) {
	s.T().Helper()
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(s.WorkDir, path)
	}
	data, err := os.ReadFile(fullPath)
	if assert.NoErrorf(s.T(), err, "failed to read file %s", fullPath) {
		assert.Containsf(s.T(), string(data), substring, "expected file %s to contain %q", fullPath, substring)
	}
}

// AssertCompletionReason 断言存在 completion 类型事件，并且 reason 匹配（通常放在 Metadata 中）。
func (s *AgentTestSuite) AssertCompletionReason(reason string) {
	s.T().Helper()
	assert.Truef(s.T(), completionReasonEquals(s.Events, reason), "expected completion reason %q", reason)
}

// 工具函数实现

func eventExists(events []event.StreamEvent, eventType event.EventType) bool {
	for _, e := range events {
		if e.Type == eventType {
			return true
		}
	}
	return false
}

func eventSequenceExists(events []event.StreamEvent, types []event.EventType) bool {
	if len(types) == 0 {
		return true
	}
	idx := 0
	for _, e := range events {
		if e.Type == types[idx] {
			idx++
			if idx == len(types) {
				return true
			}
		}
	}
	return false
}

func toolCalled(events []event.StreamEvent, toolName string) bool {
	return toolCallCount(events, toolName) > 0
}

// toolCallCount 按唯一 Tool 调用 ID 统计调用次数，避免同一次调用的多种事件被重复计数。
func toolCallCount(events []event.StreamEvent, toolName string) int {
	seen := make(map[string]struct{})

	for _, e := range events {
		if e.Type == event.EventTypeToolCall && len(e.ToolCalls) > 0 {
			for _, tc := range e.ToolCalls {
				if tc == nil || tc.Name != toolName {
					continue
				}
				if tc.ID != "" {
					seen[tc.ID] = struct{}{}
				}
			}
		}
		if e.Type == event.EventTypeToolUse && e.ToolUse != nil {
			if e.ToolUse.ToolName == toolName && e.ToolUse.ID != "" {
				seen[e.ToolUse.ID] = struct{}{}
			}
		}
	}

	return len(seen)
}

func toolCalledWithParams(events []event.StreamEvent, toolName string, expected map[string]interface{}) bool {
	// 将 expected 编码为 JSON，做弱匹配（只要求期望键值对存在）。
	expectedJSON, _ := json.Marshal(expected)

	for _, e := range events {
		// EventTypeToolCall 中的参数
		if e.Type == event.EventTypeToolCall && len(e.ToolCalls) > 0 {
			for _, tc := range e.ToolCalls {
				if tc == nil || tc.Name != toolName {
					continue
				}
				if matchesJSONSubset(tc.Arguments, expectedJSON) {
					return true
				}
			}
		}

		// EventTypeToolUse 中的参数
		if e.Type == event.EventTypeToolUse && e.ToolUse != nil && e.ToolUse.ToolName == toolName {
			if matchesJSONSubset(e.ToolUse.Parameters, expectedJSON) {
				return true
			}
		}
	}
	return false
}

func contentContains(events []event.StreamEvent, substring string) bool {
	for _, e := range events {
		if (e.Type == event.EventTypeContent || e.Type == event.EventTypeStreamContent) && strings.Contains(e.Content, substring) {
			return true
		}
	}
	return false
}

func completionReasonEquals(events []event.StreamEvent, reason string) bool {
	for _, e := range events {
		if e.Type == event.EventTypeTaskCompletion {
			if e.Content == reason {
				return true
			}
			if e.Metadata != nil {
				if val, ok := e.Metadata["reason"]; ok {
					if s, ok := val.(string); ok && s == reason {
						return true
					}
				}
			}
		}
	}
	return false
}

// matchesJSONSubset 判断给定参数 map 是否至少包含 expectedJSON 中的键值。
func matchesJSONSubset(params interface{}, expectedJSON []byte) bool {
	if params == nil {
		return false
	}

	// 统一转换为 map[string]interface{} 再比较
	var paramsMap map[string]interface{}

	switch v := params.(type) {
	case map[string]interface{}:
		paramsMap = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		_ = json.Unmarshal(b, &paramsMap)
	}

	var expectedMap map[string]interface{}
	if err := json.Unmarshal(expectedJSON, &expectedMap); err != nil {
		return false
	}

	for k, v := range expectedMap {
		actual, ok := paramsMap[k]
		if !ok || !valuesEqual(actual, v) {
			return false
		}
	}
	return true
}

// valuesEqual 做一个宽松的 JSON 值比较（目前简单使用 testify 的 ObjectsAreEqual）。
func valuesEqual(a, b interface{}) bool {
	return assert.ObjectsAreEqual(a, b)
}

// 帮助在非 Suite 场景下对事件数组进行断言（少数组件级测试可能会直接复用）。

func AssertEventExists(t *testing.T, events []event.StreamEvent, eventType event.EventType) {
	t.Helper()
	assert.Truef(t, eventExists(events, eventType), "expected event type %s to exist", eventType)
}

func AssertToolCalled(t *testing.T, events []event.StreamEvent, toolName string) {
	t.Helper()
	assert.Truef(t, toolCalled(events, toolName), "expected tool %q to be called", toolName)
}

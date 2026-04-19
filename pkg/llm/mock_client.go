package llm

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

// MockResponse represents a predefined response from the mock LLM
type MockResponse struct {
	Content   string
	Reasoning string
	ToolCalls []tools.ToolCall
	Error     error
	Delay     time.Duration // Simulate network delay
}

// MockClient is a configurable mock implementation of LLMClient
type MockClient struct {
	mu    sync.Mutex
	tools []interfaces.Tool

	// Sequential responses: returns the next response in the list
	Responses   []MockResponse
	responseIdx int

	// Rule-based responses: match against the last user message
	Rules map[string]MockResponse

	// Default response if no rules match and no sequential responses left
	DefaultResp MockResponse

	// Record of all calls made to this mock
	Calls         [][]Message
	GenerateCalls []string
}

// NewMockClient creates a new mock LLM client
func NewMockClient() *MockClient {
	return &MockClient{
		Rules: make(map[string]MockResponse),
		DefaultResp: MockResponse{
			Content: "Mock response",
		},
	}
}

// Ensure MockClient implements LLMClient
var _ LLMClient = (*MockClient)(nil)

// GenerateContent generates mock content based on rules or defaults
func (m *MockClient) GenerateContent(ctx context.Context, prompt string) (string, error) {
	m.mu.Lock()
	m.GenerateCalls = append(m.GenerateCalls, prompt)
	m.mu.Unlock()

	resp := m.getNextResponse(prompt)

	if resp.Error != nil {
		return "", resp.Error
	}

	if resp.Delay > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(resp.Delay):
		}
	}

	return resp.Content, nil
}

// StreamCompletion streams mock completion
func (m *MockClient) StreamCompletion(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	return m.stream(ctx, messages, true, onEvent)
}

// StreamCompletionWithoutReasoning streams mock completion without reasoning
func (m *MockClient) StreamCompletionWithoutReasoning(ctx context.Context, messages []Message, onEvent func(event.StreamEvent)) error {
	return m.stream(ctx, messages, false, onEvent)
}

// UpdateTools updates the tools available to the mock client
func (m *MockClient) UpdateTools(t []interfaces.Tool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools = t
}

func (m *MockClient) stream(ctx context.Context, messages []Message, includeReasoning bool, onEvent func(event.StreamEvent)) error {
	m.mu.Lock()
	m.Calls = append(m.Calls, messages)
	m.mu.Unlock()

	// Extract the last user message for rule matching
	var lastUserMsg string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserMsg = messages[i].Content
			break
		}
	}

	resp := m.getNextResponse(lastUserMsg)

	if resp.Delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(resp.Delay):
		}
	}

	if resp.Error != nil {
		onEvent(event.StreamEvent{
			Type:  event.EventTypeError,
			Error: resp.Error.Error(),
		})
		return resp.Error
	}

	// Stream reasoning if enabled
	if includeReasoning && resp.Reasoning != "" {
		words := strings.Split(resp.Reasoning, " ")
		for i, w := range words {
			if i > 0 {
				w = " " + w
			}
			onEvent(event.StreamEvent{
				Type:    event.EventTypeThinking,
				Content: w,
			})
		}
	}

	// Stream content
	if resp.Content != "" {
		words := strings.Split(resp.Content, " ")
		for i, w := range words {
			if i > 0 {
				w = " " + w
			}
			onEvent(event.StreamEvent{
				Type:    event.EventTypeContent,
				Content: w,
			})
		}
		// stream_content for real-time rendering
		onEvent(event.StreamEvent{
			Type:    event.EventTypeStreamContent,
			Content: resp.Content,
		})
	}

	// Send tool calls
	if len(resp.ToolCalls) > 0 {
		var toolCallPointers []*tools.ToolCall
		for i := range resp.ToolCalls {
			toolCallPointers = append(toolCallPointers, &resp.ToolCalls[i])
		}
		onEvent(event.StreamEvent{
			Type:      event.EventTypeToolCall,
			ToolCalls: toolCallPointers,
		})
	}

	// Send done event
	onEvent(event.StreamEvent{
		Type: event.EventTypeDone,
		Done: true,
	})

	return nil
}

func (m *MockClient) getNextResponse(input string) MockResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check rules first
	for keyword, resp := range m.Rules {
		if strings.Contains(input, keyword) {
			return resp
		}
	}

	// Check sequential responses
	if m.responseIdx < len(m.Responses) {
		resp := m.Responses[m.responseIdx]
		m.responseIdx++
		return resp
	}

	return m.DefaultResp
}

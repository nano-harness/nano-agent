package permission

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

// fakeLLMClient is a test double for llm.LLMClient
type fakeLLMClient struct {
	response   string
	err        error
	callCount  int
	lastPrompt string
}

func (f *fakeLLMClient) GenerateContent(ctx context.Context, prompt string) (string, error) {
	f.callCount++
	f.lastPrompt = prompt
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func (f *fakeLLMClient) StreamCompletion(ctx context.Context, messages []llm.Message, onEvent func(event.StreamEvent)) error {
	return errors.New("not implemented")
}

func (f *fakeLLMClient) StreamCompletionWithoutReasoning(ctx context.Context, messages []llm.Message, onEvent func(event.StreamEvent)) error {
	return errors.New("not implemented")
}

func (f *fakeLLMClient) UpdateTools(tools []interfaces.Tool) {
	// No-op for testing
}

func TestLLMClassifier_Classify_AutoApproveReadOnly(t *testing.T) {
	responseJSON := `{
		"should_block": false,
		"reason": "read-only file access",
		"confidence": 0.95,
		"stage": "stage1"
	}`

	fakeClient := &fakeLLMClient{
		response: responseJSON,
	}

	classifier := &LLMClassifier{
		Client:       fakeClient,
		Model:        "test-model",
		SystemPrompt: "test prompt",
		Timeout_:     5 * time.Second,
	}

	ctx := context.Background()
	req := ClassifyRequest{
		ToolName: "read_file",
		Params:   map[string]interface{}{"file_path": "/repo/main.go"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}

	result, err := classifier.Classify(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ShouldBlock {
		t.Errorf("expected ShouldBlock=false, got true")
	}
	if result.Confidence != 0.95 {
		t.Errorf("expected Confidence=0.95, got %.2f", result.Confidence)
	}
	if result.Reason != "read-only file access" {
		t.Errorf("expected Reason='read-only file access', got '%s'", result.Reason)
	}
	if fakeClient.callCount != 1 {
		t.Errorf("expected 1 LLM call, got %d", fakeClient.callCount)
	}
}

func TestLLMClassifier_Classify_BlockDestructive(t *testing.T) {
	responseJSON := `{
		"should_block": true,
		"reason": "destructive command",
		"confidence": 1.0,
		"stage": "stage1"
	}`

	fakeClient := &fakeLLMClient{
		response: responseJSON,
	}

	classifier := &LLMClassifier{
		Client:       fakeClient,
		Model:        "test-model",
		SystemPrompt: "test prompt",
		Timeout_:     5 * time.Second,
	}

	ctx := context.Background()
	req := ClassifyRequest{
		ToolName: "run_shell_command",
		Params:   map[string]interface{}{"command": "rm -rf /"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}

	result, err := classifier.Classify(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.ShouldBlock {
		t.Errorf("expected ShouldBlock=true, got false")
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected Confidence=1.0, got %.2f", result.Confidence)
	}
}

func TestLLMClassifier_Classify_InvalidJSON(t *testing.T) {
	// Non-JSON response should cause error
	fakeClient := &fakeLLMClient{
		response: "This is not JSON",
	}

	classifier := &LLMClassifier{
		Client:       fakeClient,
		Model:        "test-model",
		SystemPrompt: "test prompt",
		Timeout_:     5 * time.Second,
	}

	ctx := context.Background()
	req := ClassifyRequest{
		ToolName: "read_file",
		Params:   map[string]interface{}{"file_path": "/test"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}

	_, err := classifier.Classify(ctx, req)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestLLMClassifier_Classify_LLMError(t *testing.T) {
	fakeClient := &fakeLLMClient{
		err: errors.New("network timeout"),
	}

	classifier := &LLMClassifier{
		Client:       fakeClient,
		Model:        "test-model",
		SystemPrompt: "test prompt",
		Timeout_:     5 * time.Second,
	}

	ctx := context.Background()
	req := ClassifyRequest{
		ToolName: "write_file",
		Params:   map[string]interface{}{"file_path": "/test"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}

	_, err := classifier.Classify(ctx, req)
	if err == nil {
		t.Error("expected error from LLM call, got nil")
	}
}

func TestLLMClassifier_Classify_WithMarkdownFences(t *testing.T) {
	// Test that we can handle markdown code fences
	responseJSON := "```json\n" + `{
		"should_block": false,
		"reason": "safe operation",
		"confidence": 0.9,
		"stage": "stage1"
	}` + "\n```"

	fakeClient := &fakeLLMClient{
		response: responseJSON,
	}

	classifier := &LLMClassifier{
		Client:       fakeClient,
		Model:        "test-model",
		SystemPrompt: "test prompt",
		Timeout_:     5 * time.Second,
	}

	ctx := context.Background()
	req := ClassifyRequest{
		ToolName: "ls",
		Params:   map[string]interface{}{"path": "/repo"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}

	result, err := classifier.Classify(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ShouldBlock {
		t.Errorf("expected ShouldBlock=false, got true")
	}
	if result.Confidence != 0.9 {
		t.Errorf("expected Confidence=0.9, got %.2f", result.Confidence)
	}
}

func TestLLMClassifier_Timeout(t *testing.T) {
	classifier := &LLMClassifier{
		Client:       &fakeLLMClient{},
		Model:        "test-model",
		SystemPrompt: "test prompt",
		Timeout_:     10 * time.Second,
	}

	timeout := classifier.Timeout()
	if timeout != 10*time.Second {
		t.Errorf("expected timeout=10s, got %v", timeout)
	}
}

func TestCachingClassifier_CacheHit(t *testing.T) {
	responseJSON := `{
		"should_block": false,
		"reason": "cached decision",
		"confidence": 0.95,
		"stage": "stage1"
	}`

	fakeClient := &fakeLLMClient{
		response: responseJSON,
	}

	llmClassifier := &LLMClassifier{
		Client:       fakeClient,
		Model:        "test-model",
		SystemPrompt: "test prompt",
		Timeout_:     5 * time.Second,
	}

	cachingClassifier := &CachingClassifier{
		Delegate: llmClassifier,
		TTL:      5 * time.Minute,
		MaxSize:  10,
	}

	ctx := context.Background()
	req := ClassifyRequest{
		ToolName: "read_file",
		Params:   map[string]interface{}{"file_path": "/test"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}

	// First call should hit the delegate
	result1, err := cachingClassifier.Classify(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	if result1.CachedHit {
		t.Error("first call should not be a cache hit")
	}
	if fakeClient.callCount != 1 {
		t.Errorf("expected 1 LLM call after first request, got %d", fakeClient.callCount)
	}

	// Second call with same request should use cache
	result2, err := cachingClassifier.Classify(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if !result2.CachedHit {
		t.Error("second call should be a cache hit")
	}
	if fakeClient.callCount != 1 {
		t.Errorf("expected 1 LLM call after cache hit, got %d", fakeClient.callCount)
	}

	// Should have same decision
	if result1.ShouldBlock != result2.ShouldBlock {
		t.Error("cached result should match original decision")
	}
}

func TestStripMarkdownFences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with json fence",
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "with generic fence",
			input:    "```\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "no fence",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "only opening fence",
			input:    "```json\n{\"key\": \"value\"}",
			expected: `{"key": "value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripMarkdownFences(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestClassifyRequest_CacheKey(t *testing.T) {
	req1 := ClassifyRequest{
		ToolName: "read_file",
		Params:   map[string]interface{}{"file_path": "/test"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}

	req2 := ClassifyRequest{
		ToolName: "read_file",
		Params:   map[string]interface{}{"file_path": "/test"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}

	req3 := ClassifyRequest{
		ToolName: "write_file",
		Params:   map[string]interface{}{"file_path": "/test"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}

	key1 := req1.CacheKey()
	key2 := req2.CacheKey()
	key3 := req3.CacheKey()

	if key1 != key2 {
		t.Error("identical requests should produce identical cache keys")
	}

	if key1 == key3 {
		t.Error("different requests should produce different cache keys")
	}

	// Keys should be non-empty hex strings
	if len(key1) != 40 { // SHA1 produces 40 hex chars
		t.Errorf("expected 40-character hex key, got %d: %s", len(key1), key1)
	}
}

func TestClassifyResultMarshaling(t *testing.T) {
	result := &ClassifyResult{
		ShouldBlock: true,
		Reason:      "test reason",
		Confidence:  0.95,
		Stage:       "stage1",
		CachedHit:   false,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}

	var decoded ClassifyResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if decoded.ShouldBlock != result.ShouldBlock {
		t.Errorf("ShouldBlock mismatch after marshal/unmarshal")
	}
	if decoded.Confidence != result.Confidence {
		t.Errorf("Confidence mismatch after marshal/unmarshal")
	}
}

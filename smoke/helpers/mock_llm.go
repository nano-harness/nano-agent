//go:build smoke

package helpers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// MockLLMServer wraps a simple mock LLM server for smoke tests.
// It reuses patterns from e2e/enhanced_mock_server.go but simplified for smoke testing.
type MockLLMServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []map[string]interface{}
}

// NewMockLLMServer creates and starts a new mock LLM server.
func NewMockLLMServer(t *testing.T) *MockLLMServer {
	m := &MockLLMServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", m.handleChatCompletions)
	mux.HandleFunc("/v1/models", m.handleModels)

	m.server = httptest.NewServer(mux)
	t.Cleanup(func() { m.Close() })

	return m
}

// URL returns the base URL for the mock server (with /v1 prefix).
func (m *MockLLMServer) URL() string {
	return m.server.URL + "/v1"
}

// Close shuts down the mock server.
func (m *MockLLMServer) Close() {
	if m.server != nil {
		m.server.Close()
	}
}

// GetRequests returns all recorded requests.
func (m *MockLLMServer) GetRequests() []map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]interface{}{}, m.requests...)
}

func (m *MockLLMServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Record request
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.requests = append(m.requests, body)
	m.mu.Unlock()

	// Check if streaming is requested
	stream := false
	if s, ok := body["stream"].(bool); ok {
		stream = s
	}

	// Simple mock response
	if stream {
		m.handleStreamingResponse(w)
	} else {
		m.handleNonStreamingResponse(w)
	}
}

func (m *MockLLMServer) handleStreamingResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send a simple streaming response
	chunks := []string{
		"Mock ",
		"response ",
		"from ",
		"LLM ",
		"server",
	}

	for i, chunk := range chunks {
		delta := map[string]interface{}{
			"id":      "chatcmpl-mock",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   "gpt-4",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"delta": map[string]interface{}{
						"role":    "assistant",
						"content": chunk,
					},
					"finish_reason": nil,
				},
			},
		}

		if i == len(chunks)-1 {
			delta["choices"].([]map[string]interface{})[0]["finish_reason"] = "stop"
		}

		data, _ := json.Marshal(delta)
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()

		time.Sleep(10 * time.Millisecond)
	}

	// Send done signal
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

func (m *MockLLMServer) handleNonStreamingResponse(w http.ResponseWriter) {
	response := map[string]interface{}{
		"id":      "chatcmpl-mock",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "gpt-4",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Mock response from LLM server",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (m *MockLLMServer) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{
				"id":       "gpt-4",
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "openai",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

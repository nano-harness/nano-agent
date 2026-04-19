package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// MockToolCall 表示在 Mock 响应中返回的工具调用元数据。
type MockToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// MockResponse 是增强版的 LLM 响应配置。
// 为了兼容旧测试，这里同时作为 MockServerResponse 的底层实现。
type MockResponse struct {
	Content          string
	Reasoning        string
	ToolCalls        []MockToolCall
	Error            int           // HTTP 状态码（非 0 时直接返回错误码）
	Delay            time.Duration // 返回前的固定延迟
	StreamBreakAt    int           // 在第 N 个内容 chunk 后中断流式传输（不发送 DONE）
	StreamBreakError string        // 中断时用于日志/断言的错误信息（HTTP 层面仍为正常 200）
}

// MockServerResponse 向后兼容旧 e2e 测试使用的类型名。
// 新代码建议直接使用 MockResponse。
type MockServerResponse = MockResponse

// MockRule 表示一条基于请求内容的动态匹配规则。
// Matcher 接收 OpenAI 兼容格式的 messages 列表。
type MockRule struct {
	Name     string
	Matcher  func(messages []map[string]interface{}) bool
	Response MockResponse
}

// RecordedRequest 记录一次完整的请求信息，便于调试和断言。
type RecordedRequest struct {
	Body      map[string]interface{}
	Timestamp time.Time
}

// EnhancedMockServer 是新的可配置 Mock LLM 服务器。
// 它兼容 openai-go SDK 的 /v1/chat/completions 接口，并提供：
//   - 队列响应（AddResponse）
//   - 动态规则匹配（AddRule）
//   - 失败/成功模式（SetFailurePattern）
//   - 流式中断模拟（通过 MockResponse.StreamBreakAt）
//   - 请求历史记录（Requests / RecordedRequests）。
type EnhancedMockServer struct {
	mu sync.Mutex

	server *httptest.Server

	// 响应配置
	responses       []MockResponse
	rules           []MockRule
	defaultResponse *MockResponse

	// 请求记录（Requests 为旧字段名，保留用于兼容）
	Requests         []map[string]interface{}
	RecordedRequests []RecordedRequest

	// 失败模式：true=成功，false=失败
	failurePattern []bool
	failureIndex   int
}

// NewEnhancedMockServer 创建并启动一个新的 EnhancedMockServer。
func NewEnhancedMockServer() *EnhancedMockServer {
	m := &EnhancedMockServer{}

	mux := http.NewServeMux()
	// openai-go 默认以 baseURL + "/chat/completions" 访问；
	// 我们在 Config 中将 BaseURL 设置为 <server>/v1，因此这里注册 /v1/chat/completions。
	mux.HandleFunc("/v1/chat/completions", m.handleChatCompletions)

	m.server = httptest.NewServer(mux)
	return m
}

// URL 返回用于配置 Config.BaseURL 的地址（包含 /v1 前缀）。
func (m *EnhancedMockServer) URL() string {
	return m.server.URL + "/v1"
}

// Close 关闭底层 httptest.Server。
func (m *EnhancedMockServer) Close() {
	if m.server != nil {
		m.server.Close()
	}
}

// AddResponse 向队列追加一个静态响应（按请求顺序消费）。
func (m *EnhancedMockServer) AddResponse(resp MockResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, resp)
}

// SetDefaultResponse 设置兜底响应，当规则和队列都无法匹配时使用。
func (m *EnhancedMockServer) SetDefaultResponse(resp MockResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultResponse = &resp
}

// AddRule 添加一条基于请求 messages 的动态规则。
func (m *EnhancedMockServer) AddRule(rule MockRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules, rule)
}

// SetFailurePattern 设置按顺序应用的失败/成功模式。
// 例如 []bool{false, false, true} 表示前两次请求失败，第三次成功。
func (m *EnhancedMockServer) SetFailurePattern(pattern []bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failurePattern = append([]bool(nil), pattern...)
	m.failureIndex = 0
}

// Reset 清空内部状态（响应队列、规则、失败模式和请求记录）。
func (m *EnhancedMockServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = nil
	m.rules = nil
	m.defaultResponse = nil
	m.Requests = nil
	m.RecordedRequests = nil
	m.failurePattern = nil
	m.failureIndex = 0
}

func (m *EnhancedMockServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 记录请求
	m.mu.Lock()
	m.Requests = append(m.Requests, reqBody)
	m.RecordedRequests = append(m.RecordedRequests, RecordedRequest{
		Body:      reqBody,
		Timestamp: time.Now(),
	})

	// 解析 messages 供规则匹配使用
	var messages []map[string]interface{}
	if rawMsgs, ok := reqBody["messages"].([]interface{}); ok {
		for _, v := range rawMsgs {
			if mMap, ok := v.(map[string]interface{}); ok {
				messages = append(messages, mMap)
			}
		}
	}

	// 先应用 failurePattern 决定是否直接返回错误
	shouldFail, failureIndex := m.nextFailureDecisionLocked()

	// 匹配规则或队列响应
	resp := m.selectResponseLocked(messages)
	m.mu.Unlock()

	if resp.Delay > 0 {
		time.Sleep(resp.Delay)
	}

	if shouldFail {
		// 失败分支：优先使用响应中指定的错误码，否则默认 500
		status := resp.Error
		if status == 0 {
			status = http.StatusInternalServerError
		}
		http.Error(w, fmt.Sprintf("mock failure at pattern index %d", failureIndex), status)
		return
	}

	// 如果响应自身带 Error，则直接返回该状态码
	if resp.Error != 0 {
		http.Error(w, "mock error", resp.Error)
		return
	}

	// 判断是否为流式请求
	isStream := false
	if streamVal, ok := reqBody["stream"].(bool); ok && streamVal {
		isStream = true
	}

	if isStream {
		m.streamResponse(w, resp)
	} else {
		m.jsonResponse(w, resp)
	}
}

// nextFailureDecisionLocked 根据 failurePattern 决定当前请求是否失败。
// 必须在持有 m.mu 的情况下调用。
func (m *EnhancedMockServer) nextFailureDecisionLocked() (shouldFail bool, index int) {
	if len(m.failurePattern) == 0 {
		return false, -1
	}
	idx := m.failureIndex
	if idx >= len(m.failurePattern) {
		// 超出长度后默认使用最后一个值
		idx = len(m.failurePattern) - 1
	}
	shouldSucceed := m.failurePattern[idx]
	m.failureIndex++
	return !shouldSucceed, idx
}

// selectResponseLocked 在规则和队列之间选择一个响应。
// 必须在持有 m.mu 的情况下调用。
func (m *EnhancedMockServer) selectResponseLocked(messages []map[string]interface{}) MockResponse {
	// 1. 规则优先
	for _, rule := range m.rules {
		if rule.Matcher == nil {
			continue
		}
		if rule.Matcher(messages) {
			return rule.Response
		}
	}

	// 2. 队列响应
	if len(m.responses) > 0 {
		resp := m.responses[0]
		m.responses = m.responses[1:]
		return resp
	}

	// 3. 兜底响应
	if m.defaultResponse != nil {
		return *m.defaultResponse
	}

	// 4. 最终 fallback
	return MockResponse{Content: "End of mock responses."}
}

// streamResponse 以 SSE 形式返回流式响应，兼容 openai-go Streaming API。
func (m *EnhancedMockServer) streamResponse(w http.ResponseWriter, resp MockResponse) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	chunksSent := 0

	sendChunk := func(delta map[string]interface{}, finishReason *string) bool {
		chunksSent++
		// 如果配置了 StreamBreakAt，并且已达到阈值，则模拟早停（不再发送更多数据/终止标记）。
		if resp.StreamBreakAt > 0 && chunksSent > resp.StreamBreakAt {
			// 提前终止，不再写入更多数据。
			return false
		}

		chunk := map[string]interface{}{
			"id":      "chatcmpl-mock",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   "mock-model",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"delta":         delta,
					"finish_reason": finishReason,
				},
			},
		}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
		return true
	}

	// 初始空 delta，携带 role 信息
	if !sendChunk(map[string]interface{}{"role": "assistant", "content": ""}, nil) {
		return
	}

	// 如果有 Reasoning，可以先发送一段“思考中”的内容，方便测试认知事件
	if strings.TrimSpace(resp.Reasoning) != "" {
		_ = sendChunk(map[string]interface{}{"content": resp.Reasoning}, nil)
	}

	// 将内容按空格分词逐 chunk 发送
	if resp.Content != "" {
		words := strings.Split(resp.Content, " ")
		for i, word := range words {
			content := word
			if i > 0 {
				content = " " + word
			}
			if !sendChunk(map[string]interface{}{"content": content}, nil) {
				// 流式中断：不再发送 DONE 或 [DONE]
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	// 工具调用 chunk
	if len(resp.ToolCalls) > 0 {
		var toolCallChunks []map[string]interface{}
		for i, tc := range resp.ToolCalls {
			toolCallChunks = append(toolCallChunks, map[string]interface{}{
				"index": i,
				"id":    tc.ID,
				"type":  "function",
				"function": map[string]interface{}{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			})
		}
		if !sendChunk(map[string]interface{}{"tool_calls": toolCallChunks}, nil) {
			return
		}
	}

	// 结束 chunk
	finishReason := "stop"
	if len(resp.ToolCalls) > 0 {
		finishReason = "tool_calls"
	}
	if !sendChunk(map[string]interface{}{}, &finishReason) {
		return
	}

	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// jsonResponse 返回一次性 JSON 响应，兼容 OpenAI chat completion 结构。
func (m *EnhancedMockServer) jsonResponse(w http.ResponseWriter, resp MockResponse) {
	w.Header().Set("Content-Type", "application/json")

	msg := map[string]interface{}{
		"role":    "assistant",
		"content": resp.Content,
	}

	if len(resp.ToolCalls) > 0 {
		var tcs []map[string]interface{}
		for _, tc := range resp.ToolCalls {
			tcs = append(tcs, map[string]interface{}{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			})
		}
		msg["tool_calls"] = tcs
	}

	finishReason := "stop"
	if len(resp.ToolCalls) > 0 {
		finishReason = "tool_calls"
	}

	response := map[string]interface{}{
		"id":      "chatcmpl-mock",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "mock-model",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
			},
		},
	}

	_ = json.NewEncoder(w).Encode(response)
}

// Package llm provides Large Language Model integration and error handling
package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// APIErrorCategory API错误分类
type APIErrorCategory string

const (
	// APIErrorCategoryRateLimit indicates rate limit errors
	APIErrorCategoryRateLimit APIErrorCategory = "rate_limit"
	// APIErrorCategoryAuthentication indicates authentication errors
	APIErrorCategoryAuthentication APIErrorCategory = "authentication"
	// APIErrorCategoryQuota indicates quota errors
	APIErrorCategoryQuota APIErrorCategory = "quota"
	// APIErrorCategoryNetwork indicates network errors
	APIErrorCategoryNetwork APIErrorCategory = "network"
	// APIErrorCategoryServer indicates server errors
	APIErrorCategoryServer APIErrorCategory = "server"
	// APIErrorCategoryClient indicates client errors
	APIErrorCategoryClient APIErrorCategory = "client"
	// APIErrorCategoryTimeout indicates timeout errors
	APIErrorCategoryTimeout APIErrorCategory = "timeout"
	// APIErrorCategoryUnknown indicates unknown errors
	APIErrorCategoryUnknown APIErrorCategory = "unknown"
)

// APIErrorSeverity API错误严重性
type APIErrorSeverity string

const (
	// APIErrorSeverityLow indicates low severity errors
	APIErrorSeverityLow APIErrorSeverity = "low"
	// APIErrorSeverityMedium indicates medium severity errors
	APIErrorSeverityMedium APIErrorSeverity = "medium"
	// APIErrorSeverityHigh indicates high severity errors
	APIErrorSeverityHigh APIErrorSeverity = "high"
	// APIErrorSeverityCritical indicates critical severity errors
	APIErrorSeverityCritical APIErrorSeverity = "critical"
)

// APIErrorInfo API错误信息
type APIErrorInfo struct {
	Category   APIErrorCategory
	Severity   APIErrorSeverity
	Retryable  bool
	RetryAfter time.Duration
	Message    string
	HTTPStatus int
	ErrorCode  string
	Suggestion string
}

// APIErrorHandler API错误处理器
type APIErrorHandler struct {
	maxRetries     int
	baseRetryDelay time.Duration
	maxRetryDelay  time.Duration
	backoffFactor  float64
	eventHandler   func(event.StreamEvent)
	errorStats     map[APIErrorCategory]int
	lastErrorTime  time.Time
}

// NewAPIErrorHandler 创建新的API错误处理器
func NewAPIErrorHandler(eventHandler func(event.StreamEvent)) *APIErrorHandler {
	return &APIErrorHandler{
		maxRetries:     5,
		baseRetryDelay: time.Second,
		maxRetryDelay:  time.Minute,
		backoffFactor:  2.0,
		eventHandler:   eventHandler,
		errorStats:     make(map[APIErrorCategory]int),
	}
}

// AnalyzeError 分析API错误
func (aeh *APIErrorHandler) AnalyzeError(err error, httpStatus int) *APIErrorInfo {
	if err == nil {
		return nil
	}

	errorMsg := strings.ToLower(err.Error())
	errorInfo := &APIErrorInfo{
		Message:    err.Error(),
		HTTPStatus: httpStatus,
	}

	// 根据HTTP状态码分类
	switch httpStatus {
	case http.StatusTooManyRequests:
		if strings.Contains(errorMsg, "insufficient_quota") ||
			strings.Contains(errorMsg, "exceeded your current quota") ||
			strings.Contains(errorMsg, "billing") ||
			strings.Contains(errorMsg, "token-limit") {
			errorInfo.Category = APIErrorCategoryQuota
			errorInfo.Severity = APIErrorSeverityHigh
			errorInfo.Retryable = false
			errorInfo.Suggestion = "配额不足或已触发供应商限额，请检查账户状态或更换模型/密钥"
		} else {
			errorInfo.Category = APIErrorCategoryRateLimit
			errorInfo.Severity = APIErrorSeverityMedium
			errorInfo.Retryable = true
			errorInfo.RetryAfter = 30 * time.Second
			errorInfo.Suggestion = "请求频率过高，建议降低请求频率或等待后重试"
		}

	case http.StatusUnauthorized:
		errorInfo.Category = APIErrorCategoryAuthentication
		errorInfo.Severity = APIErrorSeverityHigh
		errorInfo.Retryable = false
		errorInfo.Suggestion = "认证失败，请检查API密钥是否正确"

	case http.StatusForbidden:
		errorInfo.Category = APIErrorCategoryQuota
		errorInfo.Severity = APIErrorSeverityHigh
		errorInfo.Retryable = false
		errorInfo.Suggestion = "配额不足或权限不够，请检查账户状态"

	case http.StatusInternalServerError:
		errorInfo.Category = APIErrorCategoryServer
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = true
		errorInfo.RetryAfter = 10 * time.Second
		errorInfo.Suggestion = "服务器内部错误，建议稍后重试"

	case http.StatusBadGateway:
		errorInfo.Category = APIErrorCategoryServer
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = true
		errorInfo.RetryAfter = 15 * time.Second
		errorInfo.Suggestion = "网关错误，建议稍后重试"

	case http.StatusServiceUnavailable:
		errorInfo.Category = APIErrorCategoryServer
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = true
		errorInfo.RetryAfter = 20 * time.Second
		errorInfo.Suggestion = "服务暂时不可用，建议稍后重试"

	case http.StatusGatewayTimeout:
		errorInfo.Category = APIErrorCategoryTimeout
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = true
		errorInfo.RetryAfter = 5 * time.Second
		errorInfo.Suggestion = "网关超时，建议重试"

	case http.StatusBadRequest:
		errorInfo.Category = APIErrorCategoryClient
		errorInfo.Severity = APIErrorSeverityHigh
		errorInfo.Retryable = false
		errorInfo.Suggestion = "请求格式错误，请检查请求参数"

	default:
		// 根据错误消息内容进一步分类
		errorInfo = aeh.analyzeByErrorMessage(errorInfo, errorMsg)
	}

	// 记录错误统计
	aeh.errorStats[errorInfo.Category]++
	aeh.lastErrorTime = time.Now()

	return errorInfo
}

// analyzeByErrorMessage 根据错误消息分析错误
func (aeh *APIErrorHandler) analyzeByErrorMessage(errorInfo *APIErrorInfo, errorMsg string) *APIErrorInfo {
	switch {
	case strings.Contains(errorMsg, "timeout"), strings.Contains(errorMsg, "deadline exceeded"):
		errorInfo.Category = APIErrorCategoryTimeout
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = true
		errorInfo.RetryAfter = 5 * time.Second
		errorInfo.Suggestion = "请求超时，建议重试"

	case strings.Contains(errorMsg, "connection"), strings.Contains(errorMsg, "network"):
		errorInfo.Category = APIErrorCategoryNetwork
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = true
		errorInfo.RetryAfter = 3 * time.Second
		errorInfo.Suggestion = "网络连接问题，建议检查网络后重试"

	case strings.Contains(errorMsg, "rate limit"), strings.Contains(errorMsg, "too many requests"):
		errorInfo.Category = APIErrorCategoryRateLimit
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = true
		errorInfo.RetryAfter = 30 * time.Second
		errorInfo.Suggestion = "请求频率过高，建议降低请求频率"

	case strings.Contains(errorMsg, "quota"), strings.Contains(errorMsg, "limit exceeded"):
		errorInfo.Category = APIErrorCategoryQuota
		errorInfo.Severity = APIErrorSeverityHigh
		errorInfo.Retryable = false
		errorInfo.Suggestion = "配额已用完，请检查账户状态"

	case strings.Contains(errorMsg, "unauthorized"), strings.Contains(errorMsg, "invalid api key"):
		errorInfo.Category = APIErrorCategoryAuthentication
		errorInfo.Severity = APIErrorSeverityHigh
		errorInfo.Retryable = false
		errorInfo.Suggestion = "认证失败，请检查API密钥"

	case strings.Contains(errorMsg, "context canceled"):
		errorInfo.Category = APIErrorCategoryClient
		errorInfo.Severity = APIErrorSeverityLow
		errorInfo.Retryable = false
		errorInfo.Suggestion = "请求被取消"
	case strings.Contains(errorMsg, "reasoning"), strings.Contains(errorMsg, "o1"):
		errorInfo.Category = APIErrorCategoryClient
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = true
		errorInfo.RetryAfter = 2 * time.Second
		errorInfo.Suggestion = "推理模式请求失败，系统将自动切换到标准模式重试"
	case strings.Contains(errorMsg, "model not found"), strings.Contains(errorMsg, "invalid model"):
		errorInfo.Category = APIErrorCategoryClient
		errorInfo.Severity = APIErrorSeverityHigh
		errorInfo.Retryable = false
		errorInfo.Suggestion = "模型不存在或无权限访问，请检查模型名称和API权限"
	case strings.Contains(errorMsg, "max tokens"), strings.Contains(errorMsg, "token limit"):
		errorInfo.Category = APIErrorCategoryClient
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = false
		errorInfo.Suggestion = "请求超出模型token限制，请减少输入内容或调整max_tokens参数"
	default:
		errorInfo.Category = APIErrorCategoryUnknown
		errorInfo.Severity = APIErrorSeverityMedium
		errorInfo.Retryable = true
		errorInfo.RetryAfter = 5 * time.Second
		errorInfo.Suggestion = "未知错误，建议重试"
	}

	return errorInfo
}

// HandleAPIError 处理API错误
func (aeh *APIErrorHandler) HandleAPIError(ctx context.Context, err error, httpStatus int, attempt int) (*APIErrorInfo, bool) {
	errorInfo := aeh.AnalyzeError(err, httpStatus)
	if errorInfo == nil {
		return nil, true // 没有错误，继续执行
	}

	// 发送错误事件
	if aeh.eventHandler != nil {
		errorEvent := event.NewStreamEvent(event.EventTypeError, "api_error_handler")
		errorEvent = errorEvent.WithError(errorInfo.Message, string(errorInfo.Severity)).
			WithMetadata("category", errorInfo.Category).
			WithMetadata("http_status", errorInfo.HTTPStatus).
			WithMetadata("retryable", errorInfo.Retryable).
			WithMetadata("attempt", attempt).
			WithMetadata("suggestion", errorInfo.Suggestion)
		aeh.eventHandler(errorEvent)
	}

	// 记录错误日志
	logger.Errorf("API错误 [%s]: %s (HTTP %d, 尝试 %d)", errorInfo.Category, errorInfo.Message, errorInfo.HTTPStatus, attempt)

	// 检查是否应该重试
	if !errorInfo.Retryable || attempt >= aeh.maxRetries {
		return errorInfo, false // 不重试
	}

	// 计算重试延迟
	retryDelay := aeh.calculateRetryDelay(errorInfo, attempt)

	// 发送重试事件
	if aeh.eventHandler != nil {
		retryEvent := event.NewStreamEvent(event.EventTypeRetry, "api_error_handler")
		retryEvent = retryEvent.WithContent(
			fmt.Sprintf("API请求失败，%v后重试 (第%d/%d次)", retryDelay, attempt+1, aeh.maxRetries),
		).WithMetadata("retry_delay_ms", retryDelay.Milliseconds()).
			WithMetadata("next_attempt", attempt+1).
			WithMetadata("max_attempts", aeh.maxRetries).
			WithRetryCount(attempt)
		aeh.eventHandler(retryEvent)
	}

	// 等待重试
	select {
	case <-ctx.Done():
		return errorInfo, false // 上下文取消，不重试
	case <-time.After(retryDelay):
		return errorInfo, true // 继续重试
	}
}

// calculateRetryDelay 计算重试延迟
func (aeh *APIErrorHandler) calculateRetryDelay(errorInfo *APIErrorInfo, attempt int) time.Duration {
	// 如果错误信息中指定了重试延迟，优先使用
	if errorInfo.RetryAfter > 0 {
		return errorInfo.RetryAfter
	}

	// 指数退避算法
	delay := time.Duration(float64(aeh.baseRetryDelay) * float64(attempt) * aeh.backoffFactor)

	// 限制最大延迟
	if delay > aeh.maxRetryDelay {
		delay = aeh.maxRetryDelay
	}

	// 根据错误类型调整延迟
	switch errorInfo.Category {
	case APIErrorCategoryRateLimit:
		delay = delay * 2 // 频率限制错误延迟更长
	case APIErrorCategoryNetwork:
		delay = delay / 2 // 网络错误延迟较短
	case APIErrorCategoryTimeout:
		delay = delay / 3 // 超时错误延迟最短
	}

	return delay
}

// GetErrorStats 获取错误统计
func (aeh *APIErrorHandler) GetErrorStats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["error_counts"] = aeh.errorStats
	stats["last_error_time"] = aeh.lastErrorTime
	stats["total_errors"] = aeh.getTotalErrors()
	return stats
}

// getTotalErrors 获取总错误数
func (aeh *APIErrorHandler) getTotalErrors() int {
	total := 0
	for _, count := range aeh.errorStats {
		total += count
	}
	return total
}

// ResetStats 重置错误统计
func (aeh *APIErrorHandler) ResetStats() {
	aeh.errorStats = make(map[APIErrorCategory]int)
	aeh.lastErrorTime = time.Time{}
}

// UpdateConfig 更新配置
func (aeh *APIErrorHandler) UpdateConfig(maxRetries int, baseRetryDelay, maxRetryDelay time.Duration, backoffFactor float64) {
	aeh.maxRetries = maxRetries
	aeh.baseRetryDelay = baseRetryDelay
	aeh.maxRetryDelay = maxRetryDelay
	aeh.backoffFactor = backoffFactor
}

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/middleware"
)

// ToolResultFailureError represents a tool result failure error
type ToolResultFailureError struct {
	ToolName string
	Code     string
	Message  string
}

func (e *ToolResultFailureError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ToolRetryPolicy defines the retry policy for a tool
type ToolRetryPolicy struct {
	MaxRetries        int
	RetryDelay        time.Duration
	BackoffMultiplier float64
	MaxDelay          time.Duration
	JitterRatio       float64
}

// ToolExecutor is a minimal executor abstraction to decouple recovery from scheduler
type ToolExecutor interface {
	Execute(ctx context.Context, name string, params map[string]interface{}) (*interfaces.ToolResult, error)
}

// ToolRecoveryStrategy 定义工具执行错误恢复策略
type ToolRecoveryStrategy struct {
	maxRetries        int
	retryDelay        time.Duration
	backoffMultiplier float64
	maxDelay          time.Duration
	jitterRatio       float64
	recoverableErrors []string
	eventHandler      func(event.StreamEvent)
	perToolPolicies   map[string]ToolRetryPolicy
}

// NewToolRecoveryStrategy 创建新的工具恢复策略
func NewToolRecoveryStrategy(eventHandler func(event.StreamEvent)) *ToolRecoveryStrategy {
	return &ToolRecoveryStrategy{
		maxRetries:        3,
		retryDelay:        time.Second,
		backoffMultiplier: 2.0,
		maxDelay:          30 * time.Second,
		jitterRatio:       0.2,
		recoverableErrors: []string{
			"timeout",
			"connection",
			"temporary",
			"rate limit",
			"network",
			"service unavailable",
		},
		eventHandler:    eventHandler,
		perToolPolicies: make(map[string]ToolRetryPolicy),
	}
}

// ErrorCategory 错误分类
type ErrorCategory string

const (
	// ErrorCategoryRecoverable indicates a recoverable error
	ErrorCategoryRecoverable ErrorCategory = "recoverable"
	// ErrorCategoryUnrecoverable indicates an unrecoverable error
	ErrorCategoryUnrecoverable ErrorCategory = "unrecoverable"
	// ErrorCategoryRetryable indicates a retryable error
	ErrorCategoryRetryable ErrorCategory = "retryable"
	// ErrorCategoryFatal indicates a fatal error
	ErrorCategoryFatal ErrorCategory = "fatal"
	// ErrorCategoryBusinessFailure indicates a tool executed but returned a
	// business-level failure (e.g. file not found, binary content, validation error).
	// These should NOT be retried and should NOT count toward error thresholds.
	ErrorCategoryBusinessFailure ErrorCategory = "business_failure"
)

// ToolExecutionResult 工具执行结果
type ToolExecutionResult struct {
	Result        *interfaces.ToolResult
	Error         error
	Attempts      int
	TotalTime     time.Duration
	ErrorCategory ErrorCategory
	RecoveryInfo  map[string]interface{}
}

// ExecuteWithRecovery 带恢复策略的工具执行
func (trs *ToolRecoveryStrategy) ExecuteWithRecovery(
	ctx context.Context,
	executor ToolExecutor,
	toolToExecute ToolToExecute,
) *ToolExecutionResult {
	startTime := time.Now()
	policy := trs.policyForTool(toolToExecute.Name)
	result := &ToolExecutionResult{
		RecoveryInfo: make(map[string]interface{}),
	}

	var lastErr error
	var lastToolResult *interfaces.ToolResult
	maxAttempts := policy.MaxRetries
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result.Attempts = attempt

		select {
		case <-ctx.Done():
			lastErr = ctx.Err()
			result.Error = lastErr
			result.TotalTime = time.Since(startTime)
			result.ErrorCategory = ErrorCategoryFatal
			return result
		default:
		}

		// 发送重试事件（除了第一次尝试）
		if attempt > 1 {
			delay := computeBackoffDelay(policy, attempt)
			result.RecoveryInfo[fmt.Sprintf("attempt_%d_delay_ms", attempt)] = delay.Milliseconds()
			if trs.eventHandler != nil {
				retryEvent := event.NewStreamEvent(event.EventTypeRetry, "tool_recovery")
				retryEvent = retryEvent.WithContent(
					fmt.Sprintf("重试执行工具 %s (第 %d/%d 次尝试)", toolToExecute.Name, attempt, maxAttempts),
				).WithMetadata("tool_name", toolToExecute.Name).
					WithMetadata("attempt", attempt).
					WithMetadata("delay_ms", delay.Milliseconds()).
					WithRetryCount(attempt - 1)
				trs.eventHandler(retryEvent)
			}

			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					lastErr = ctx.Err()
					lastToolResult = result.Result //nolint:ineffassign,staticcheck
					result.Error = lastErr
					result.TotalTime = time.Since(startTime)
					result.ErrorCategory = ErrorCategoryFatal
					return result
				}
			}
		}

		// 执行工具（通过抽象的执行器执行）
		toolResult, err := executor.Execute(ctx, toolToExecute.Name, toolToExecute.Parameters)
		lastToolResult = toolResult
		result.Result = toolResult

		if err == nil && toolResult == nil {
			err = &ToolResultFailureError{
				ToolName: toolToExecute.Name,
				Code:     "empty_tool_result",
				Message:  "tool returned nil result",
			}
		}
		if err == nil && toolResult != nil && !toolResult.Success {
			err = &ToolResultFailureError{
				ToolName: toolToExecute.Name,
				Code:     getToolResultCode(toolResult),
				Message:  getToolResultMessage(toolResult),
			}
		}

		if err == nil {
			// 成功执行
			result.TotalTime = time.Since(startTime)
			result.ErrorCategory = ""

			// 发送成功恢复事件（如果之前有失败）
			if result.Attempts > 1 && trs.eventHandler != nil {
				successEvent := event.NewStreamEvent(event.EventTypeContent, "tool_recovery")
				successEvent = successEvent.WithContent(
					fmt.Sprintf("工具 %s 在第 %d 次尝试后成功", toolToExecute.Name, attempt),
				).WithMetadata("tool_name", toolToExecute.Name).
					WithMetadata("attempts", attempt)
				trs.eventHandler(successEvent)
			}

			return result
		}

		// 记录错误并分类
		lastErr = err
		category := trs.categorizeError(err)
		result.ErrorCategory = category
		result.RecoveryInfo[fmt.Sprintf("attempt_%d_error", attempt)] = err.Error()
		result.RecoveryInfo[fmt.Sprintf("attempt_%d_error_category", attempt)] = category

		// 发送错误事件
		if trs.eventHandler != nil {
			errorEvent := event.NewStreamEvent(event.EventTypeError, "tool_recovery")
			errorEvent = errorEvent.WithContent(
				fmt.Sprintf("工具 %s 执行失败: %v", toolToExecute.Name, err),
			).WithError(err.Error(), "medium").
				WithMetadata("tool_name", toolToExecute.Name).
				WithMetadata("attempt", attempt).
				WithMetadata("error_category", category)
			trs.eventHandler(errorEvent)
		}

		logger.Warnf("工具 %s 执行失败 (第 %d/%d 次尝试): %v", toolToExecute.Name, attempt, maxAttempts, err)

		if !trs.IsRecoverable(err) {
			break
		}
	}

	// 所有重试都失败了
	result.Error = lastErr
	result.Result = lastToolResult
	result.TotalTime = time.Since(startTime)

	// 发送最终失败事件
	if trs.eventHandler != nil {
		errorEvent := event.NewStreamEvent(event.EventTypeError, "tool_recovery")
		errorEvent = errorEvent.WithContent(
			fmt.Sprintf("工具 %s 在 %d 次尝试后最终失败", toolToExecute.Name, result.Attempts),
		).WithError(lastErr.Error(), "high").
			WithMetadata("tool_name", toolToExecute.Name).
			WithMetadata("total_attempts", result.Attempts).
			WithMetadata("error_category", result.ErrorCategory)
		trs.eventHandler(errorEvent)
	}

	return result
}

func (trs *ToolRecoveryStrategy) policyForTool(toolName string) ToolRetryPolicy {
	if trs.perToolPolicies != nil {
		if p, ok := trs.perToolPolicies[toolName]; ok {
			if p.MaxRetries <= 0 {
				p.MaxRetries = trs.maxRetries
			}
			if p.RetryDelay <= 0 {
				p.RetryDelay = trs.retryDelay
			}
			if p.BackoffMultiplier <= 0 {
				p.BackoffMultiplier = trs.backoffMultiplier
			}
			if p.MaxDelay <= 0 {
				p.MaxDelay = trs.maxDelay
			}
			if p.JitterRatio <= 0 {
				p.JitterRatio = trs.jitterRatio
			}
			return p
		}
	}
	return ToolRetryPolicy{
		MaxRetries:        trs.maxRetries,
		RetryDelay:        trs.retryDelay,
		BackoffMultiplier: trs.backoffMultiplier,
		MaxDelay:          trs.maxDelay,
		JitterRatio:       trs.jitterRatio,
	}
}

// SetToolPolicy sets the retry policy for a specific tool
func (trs *ToolRecoveryStrategy) SetToolPolicy(toolName string, policy ToolRetryPolicy) {
	if toolName == "" {
		return
	}
	if trs.perToolPolicies == nil {
		trs.perToolPolicies = make(map[string]ToolRetryPolicy)
	}
	trs.perToolPolicies[toolName] = policy
}

// UpdateBackoffOptions updates backoff options
func (trs *ToolRecoveryStrategy) UpdateBackoffOptions(maxDelay time.Duration, jitterRatio float64) {
	trs.maxDelay = maxDelay
	trs.jitterRatio = jitterRatio
}

// computeBackoffDelay delegates delay calculation to middleware.BackoffConfig.
func computeBackoffDelay(policy ToolRetryPolicy, attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	multiplier := policy.BackoffMultiplier
	if multiplier <= 1 {
		multiplier = 2.0
	}
	cfg := middleware.BackoffConfig{
		Strategy:     middleware.BackoffExponential,
		InitialDelay: policy.RetryDelay,
		MaxDelay:     policy.MaxDelay,
		Multiplier:   multiplier,
		JitterRatio:  policy.JitterRatio,
		MaxAttempts:  policy.MaxRetries,
	}
	return cfg.ComputeDelay(attempt - 1) // attempt is 1-based; ComputeDelay expects 1-based too
}

func getToolResultCode(tr *interfaces.ToolResult) string {
	if tr == nil || tr.Metadata == nil {
		return ""
	}
	if v, ok := tr.Metadata["code"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getToolResultMessage(tr *interfaces.ToolResult) string {
	if tr == nil {
		return ""
	}
	if tr.Error != "" {
		return tr.Error
	}
	if tr.UserContent != "" {
		return tr.UserContent
	}
	return tr.LLMContent
}

// categorizeError 对错误进行分类
func (trs *ToolRecoveryStrategy) categorizeError(err error) ErrorCategory {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorCategoryFatal
	}
	var trfe *ToolResultFailureError
	if errors.As(err, &trfe) {
		return categorizeToolResultFailureCode(trfe.Code)
	}
	msg := strings.ToLower(err.Error())

	// Check for non-retryable errors first
	nonRetryablePatterns := []string{
		"invalid argument", "invalid parameter",
		"not found", "permission denied",
		"unsupported", "validation failed",
		"invalid input", "bad request",
		"unauthorized", "forbidden",
		"not implemented", "method not allowed",
	}
	for _, pattern := range nonRetryablePatterns {
		if strings.Contains(msg, pattern) {
			return ErrorCategoryUnrecoverable
		}
	}

	// Then check for recoverable errors
	for _, recoverable := range trs.recoverableErrors {
		if strings.Contains(msg, recoverable) {
			return ErrorCategoryRecoverable
		}
	}

	// Heuristic categorization: retryable for rate limits/timeouts
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "temporar") {
		return ErrorCategoryRetryable
	}

	return ErrorCategoryUnrecoverable
}

// IsRecoverable 检查错误是否可恢复
func (trs *ToolRecoveryStrategy) IsRecoverable(err error) bool {
	category := trs.categorizeError(err)
	return category == ErrorCategoryRecoverable || category == ErrorCategoryRetryable
}

// IsBusinessFailure returns true if the error is a business-level failure
// that should be reported to the LLM but not counted as an infrastructure error.
func (trs *ToolRecoveryStrategy) IsBusinessFailure(err error) bool {
	return trs.categorizeError(err) == ErrorCategoryBusinessFailure
}

func categorizeToolResultFailureCode(code string) ErrorCategory {
	c := strings.ToLower(strings.TrimSpace(code))
	if c == "" {
		// No code provided — treat as business failure (tool ran but failed)
		return ErrorCategoryBusinessFailure
	}
	switch c {
	case "missing_required_parameters", "not_allowed", "tool_not_found":
		return ErrorCategoryBusinessFailure
	case "empty_tool_result":
		return ErrorCategoryRetryable
	case "file_not_found", "no_such_file", "permission_denied",
		"validation_error", "invalid_input", "invalid_arguments",
		"execution_failed", "command_failed", "syntax_error":
		return ErrorCategoryBusinessFailure
	}
	if strings.Contains(c, "timeout") || strings.Contains(c, "rate_limit") || strings.Contains(c, "ratelimit") || strings.Contains(c, "network") || strings.Contains(c, "temporary") || strings.Contains(c, "temporar") {
		return ErrorCategoryRetryable
	}
	// Default: unknown codes are business failures, not infrastructure errors
	return ErrorCategoryBusinessFailure
}

// SetEventHandler replaces the event handler used to emit retry/error events.
// This allows the ToolScheduler (or any caller) to wire recovery events into
// the same pipeline that other agent events flow through.
func (trs *ToolRecoveryStrategy) SetEventHandler(handler func(event.StreamEvent)) {
	trs.eventHandler = handler
}

// GenerateRecoveryGuidance generates context-specific recovery suggestions for tool failures
// This helps the LLM understand what went wrong and what alternative approaches to try
func (trs *ToolRecoveryStrategy) GenerateRecoveryGuidance(toolName string, err error, category ErrorCategory) string {
	if err == nil {
		return ""
	}

	var guidance strings.Builder
	fmt.Fprintf(&guidance, "\n\n[Recovery Guidance] Tool '%s' failed: %v\n", toolName, err)

	switch category {
	case ErrorCategoryBusinessFailure:
		var trfe *ToolResultFailureError
		if errors.As(err, &trfe) {
			switch {
			case strings.Contains(trfe.Code, "file_not_found"), strings.Contains(trfe.Code, "no_such_file"):
				guidance.WriteString("Suggestion: The file path may be incorrect. Try using search tools to find the correct path, or check if the file was moved or renamed.")
			case strings.Contains(trfe.Code, "permission_denied"):
				guidance.WriteString("Suggestion: You don't have permission for this operation. Try an alternative approach that doesn't require elevated permissions, or inform the user about the permission requirement.")
			case strings.Contains(trfe.Code, "validation_error"), strings.Contains(trfe.Code, "invalid_input"), strings.Contains(trfe.Code, "invalid_arguments"):
				guidance.WriteString("Suggestion: The input parameters have validation errors. Review the error message, correct the invalid parameters, and retry with valid inputs.")
			case strings.Contains(trfe.Code, "syntax_error"):
				guidance.WriteString("Suggestion: There's a syntax error in the input. Review the syntax requirements for this tool and correct the error before retrying.")
			default:
				guidance.WriteString("Suggestion: This operation failed due to business logic constraints. Consider using an alternative tool or approach to accomplish the task.")
			}
		} else {
			guidance.WriteString("Suggestion: Consider using an alternative tool or approach to accomplish the task.")
		}
	case ErrorCategoryRetryable:
		guidance.WriteString("This error may be temporary (e.g., resource conflicts, transient failures). The system will retry automatically.")
	case ErrorCategoryRecoverable:
		guidance.WriteString("This is a transient error. The system is attempting recovery with exponential backoff.")
	case ErrorCategoryUnrecoverable, ErrorCategoryFatal:
		guidance.WriteString("This error cannot be automatically recovered. You may need to adjust your approach or inform the user.")
	}

	return guidance.String()
}

// UpdateStrategy 更新恢复策略参数
func (trs *ToolRecoveryStrategy) UpdateStrategy(maxRetries int, retryDelay time.Duration, backoffMultiplier float64) {
	trs.maxRetries = maxRetries
	trs.retryDelay = retryDelay
	trs.backoffMultiplier = backoffMultiplier
}

// GetRecoveryStats 获取恢复统计信息
func (ter *ToolExecutionResult) GetRecoveryStats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["attempts"] = ter.Attempts
	stats["total_time_ms"] = ter.TotalTime.Milliseconds()
	stats["success"] = ter.Error == nil
	stats["error_category"] = ter.ErrorCategory
	stats["recovery_info"] = ter.RecoveryInfo
	return stats
}

// ToolRecoveryManager manages tool recovery
type ToolRecoveryManager struct {
	agent *Agent
}

// NewToolRecoveryManager creates a new tool recovery manager
func NewToolRecoveryManager(agent *Agent) *ToolRecoveryManager {
	return &ToolRecoveryManager{
		agent: agent,
	}
}

// HandleToolError handles tool errors and attempts recovery
func (m *ToolRecoveryManager) HandleToolError(_ string, err error, _ []llm.Message) (string, error) {
	// Simple heuristic recovery strategies
	return "", err
}

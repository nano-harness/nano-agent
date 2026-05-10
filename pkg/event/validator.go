package event

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ValidationResult 验证结果
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// EventValidator 事件验证器
type EventValidator struct { //nolint:revive
	requiredFields  map[EventType][]string
	validSeverities []string

	// 机密数据编校配置
	redactionPatterns []redactPattern
	redactionKeys     []string
}

// NewEventValidator 创建新的事件验证器
func NewEventValidator() *EventValidator {
	v := &EventValidator{
		requiredFields: map[EventType][]string{
			EventTypeContent:             {"Content"},
			EventTypeStreamContent:       {"Content"},
			EventTypeToolCall:            {"ToolCalls"},
			EventTypeToolResult:          {"ToolResult"},
			EventTypeError:               {"Error"},
			EventTypeTokenStats:          {"TokenStats"},
			EventTypeWaitingForUser:      {},
			EventTypeTodoListUpdate:      {"TaskList"},
			EventTypeFinalSummary:        {"Content"},
			EventTypeThinking:            {},
			EventTypeCompression:         {"Content"},
			EventTypeTaskStart:           {"Content"},
			EventTypeTaskProgress:        {"Progress", "TaskID"},
			EventTypeTaskCancel:          {"Content"},
			EventTypeRetry:               {"Content"},
			EventTypeWarning:             {"Content"},
			EventTypeDebug:               {"Content"},
			EventTypeProviderFallback:    {},
			EventTypeRouteSelected:       {},
			EventTypePlannerPlanSnapshot: {"Content"},
			EventTypePlannerPlanUpdate:   {"Content"},
			EventTypePlannerDecision:     {"Content"},
			EventTypeExecutorState:       {"Content"},
			EventTypeExecutorSchedule:    {"Content"},
			EventTypeWorkerStart:         {"Content"},
			EventTypeWorkerUpdate:        {"Content"},
			EventTypeWorkerLog:           {"Content"},
			EventTypeWorkerEnd:           {"Content"},
			EventTypeRalphIteration:      {"Content"},
			EventTypeRalphStopped:        {"Content"},
		},
		validSeverities: []string{"low", "medium", "high", "critical"},
	}

	// 预编译常见密钥/令牌/凭据的掩码规则
	v.redactionPatterns = []redactPattern{
		{re: regexp.MustCompile(`AKIA[0-9A-Z]{16}`), replacement: "[REDACTED:AWS_ACCESS_KEY_ID]"},
		{re: regexp.MustCompile(`(?i)aws(.{0,20})?(secret|access).{0,20}?[:=]\s*([A-Za-z0-9/+=]{40})`), replacement: "[REDACTED:AWS_SECRET_ACCESS_KEY]"},
		{re: regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`), replacement: "[REDACTED:GOOGLE_API_KEY]"},
		{re: regexp.MustCompile(`(?i)(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}`), replacement: "[REDACTED:GITHUB_TOKEN]"},
		{re: regexp.MustCompile(`xox[baprs]-[0-9A-Za-z\-]{10,48}`), replacement: "[REDACTED:SLACK_TOKEN]"},
		{re: regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), replacement: "[REDACTED:PRIVATE_KEY]"},
		{re: regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-\._~\+\/]+=*`), replacement: "Bearer [REDACTED]"},
		{re: regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key|token|authorization)\s*[:=]\s*["']?([^\s"']{4,})["']?`), replacement: "$1: [REDACTED]"},
		{re: regexp.MustCompile(`(?i)\bset-cookie\s*:\s*[^;\,\n]+`), replacement: "Set-Cookie: [REDACTED]"},
		{re: regexp.MustCompile(`(?i)\bcookie\s*:\s*[^\n]+`), replacement: "Cookie: [REDACTED]"},
	}
	v.redactionKeys = []string{
		"password", "passwd", "secret", "api_key", "apikey", "token", "access_token",
		"authorization", "auth", "cookie", "set-cookie", "session", "private_key", "client_secret",
	}

	return v
}

// ValidateEvent 验证单个事件
func (v *EventValidator) ValidateEvent(event StreamEvent) ValidationResult {
	result := ValidationResult{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// 验证事件类型
	if event.Type == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "Event type is required")
		return result
	}

	// 验证必需字段
	if requiredFields, exists := v.requiredFields[event.Type]; exists {
		for _, field := range requiredFields {
			switch field {
			case "Content":
				if event.Content == "" {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf("Content is required for event type %s", event.Type))
				}
			case "ToolCalls":
				if len(event.ToolCalls) == 0 {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf("ToolCalls is required for event type %s", event.Type))
				}
			case "ToolResult":
				if event.ToolResult == nil {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf("ToolResult is required for event type %s", event.Type))
				}
			case "Error":
				if event.Error == "" {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf("Error is required for event type %s", event.Type))
				}
			case "TokenStats":
				if event.TokenStats == nil {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf("TokenStats is required for event type %s", event.Type))
				}
			case "TaskList":
				if event.TaskList == nil {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf("TaskList is required for event type %s", event.Type))
				}
			case "TaskID":
				if strings.TrimSpace(event.TaskID) == "" {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf("TaskID is required for event type %s", event.Type))
				}
			}
		}
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Unknown event type: %s", event.Type))
	}

	// 验证时间戳
	if event.Timestamp != 0 {
		ts := event.Timestamp
		var eventTime time.Time
		switch {
		case ts > 1e17:
			eventTime = time.Unix(0, ts)
		case ts > 1e14:
			eventTime = time.Unix(0, ts*int64(time.Microsecond))
		case ts > 1e11:
			eventTime = time.Unix(0, ts*int64(time.Millisecond))
		default:
			eventTime = time.Unix(ts, 0)
		}
		now := time.Now()
		if eventTime.After(now.Add(time.Minute)) {
			result.Warnings = append(result.Warnings, "Event timestamp is in the future")
		}
		if eventTime.Before(now.Add(-24 * time.Hour)) {
			result.Warnings = append(result.Warnings, "Event timestamp is more than 24 hours old")
		}
	}

	// 验证严重性级别
	if event.Severity != "" {
		validSeverity := false
		for _, severity := range v.validSeverities {
			if event.Severity == severity {
				validSeverity = true
				break
			}
		}
		if !validSeverity {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Invalid severity level: %s", event.Severity))
		}
	}

	// 验证进度值
	if event.Progress < 0 || event.Progress > 1 {
		result.Valid = false
		result.Errors = append(result.Errors, "Progress must be between 0.0 and 1.0")
	}

	// 验证重试次数
	if event.RetryCount < 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "RetryCount cannot be negative")
	}

	// 验证源字段
	if event.Source == "" {
		result.Warnings = append(result.Warnings, "Source field is empty")
	}

	// 验证关联ID格式
	if event.CorrelationID != "" {
		if len(event.CorrelationID) < 8 {
			result.Warnings = append(result.Warnings, "CorrelationID seems too short")
		}
	}

	// 验证内容长度
	if event.Content != "" && len(event.Content) > 10000 {
		result.Warnings = append(result.Warnings, "Content is very long, consider truncating")
	}

	// 验证错误消息格式
	if event.Error != "" {
		if !strings.Contains(event.Error, ":") && len(event.Error) > 100 {
			result.Warnings = append(result.Warnings, "Error message might benefit from structured format")
		}
	}

	return result
}

// ValidateEventSequence 验证事件序列的逻辑一致性
func (v *EventValidator) ValidateEventSequence(events []StreamEvent) ValidationResult {
	result := ValidationResult{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	if len(events) == 0 {
		return result
	}

	// 检查时间戳顺序
	for i := 1; i < len(events); i++ {
		if events[i].Timestamp != 0 && events[i-1].Timestamp != 0 {
			if events[i].Timestamp < events[i-1].Timestamp {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Event %d has earlier timestamp than event %d", i, i-1))
			}
		}
	}

	// 检查事件流的逻辑顺序
	hasThinking := false
	hasContent := false
	hasDone := false

	for i, event := range events {
		switch event.Type {
		case EventTypeThinking:
			hasThinking = true
		case EventTypeContent, EventTypeStreamContent:
			hasContent = true
			if !hasThinking {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Content event at index %d without preceding thinking event", i))
			}
		case EventTypeDone:
			hasDone = true
			if !hasContent {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Done event at index %d without any content events", i))
			}
		case EventTypeError:
			if event.RetryCount > 5 {
				result.Warnings = append(result.Warnings, fmt.Sprintf("High retry count (%d) at event %d", event.RetryCount, i))
			}
		}
	}

	// 检查是否有未完成的序列
	if hasThinking && !hasDone && !hasContent {
		result.Warnings = append(result.Warnings, "Sequence has thinking but no content or completion")
	}

	return result
}

// SanitizeEvent 清理和标准化事件
func (v *EventValidator) SanitizeEvent(event StreamEvent) StreamEvent {
	// 清理内容
	if event.Content != "" {
		if event.Type == EventTypeStreamContent {
			// 对于流式内容，保留首尾空白和换行，仅进行敏感信息编校（不进行 Trim）
			event.Content = v.sanitizeStringNoTrim(event.Content)
		} else {
			// 非流式内容仍按原逻辑处理
			event.Content = strings.TrimSpace(event.Content)
			// 机密编校
			event.Content = v.redactJSONishString(event.Content)
		}
	}

	// 清理错误消息
	if event.Error != "" {
		event.Error = strings.TrimSpace(event.Error)
		event.Error = v.sanitizeString(event.Error)
	}

	// 标准化严重性级别
	if event.Severity != "" {
		event.Severity = strings.ToLower(strings.TrimSpace(event.Severity))
	}

	// 标准化源字段
	if event.Source != "" {
		event.Source = strings.ToLower(strings.TrimSpace(event.Source))
	}

	// 对 ToolCalls 参数进行编校（JSON 优先，其次正则）
	if len(event.ToolCalls) > 0 {
		for _, tc := range event.ToolCalls {
			if tc == nil {
				continue
			}
			if tc.Arguments != nil {
				if m, ok := v.redactAny(tc.Arguments).(map[string]interface{}); ok {
					tc.Arguments = m
				}
			}
		}
	}

	// 对 ToolResult 内容进行编校
	if event.ToolResult != nil {
		if event.ToolResult.Content != "" {
			event.ToolResult.Content = v.redactJSONishString(event.ToolResult.Content)
		}
		if event.ToolResult.Error != "" {
			event.ToolResult.Error = v.sanitizeString(event.ToolResult.Error)
		}
	}

	// 对 ToolUse 参数和结果进行编校
	if event.ToolUse != nil {
		if event.ToolUse.Parameters != nil {
			if m, ok := v.redactAny(event.ToolUse.Parameters).(map[string]interface{}); ok {
				event.ToolUse.Parameters = m
			}
		}
		if event.ToolUse.Result != "" {
			event.ToolUse.Result = v.redactJSONishString(event.ToolUse.Result)
		}
	}

	// 对 Metadata 进行编校
	if event.Metadata != nil {
		if m, ok := v.redactAny(event.Metadata).(map[string]interface{}); ok {
			event.Metadata = m
		}
	}

	// 确保时间戳存在
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	// 限制进度值范围
	if event.Progress < 0 {
		event.Progress = 0
	} else if event.Progress > 1 {
		event.Progress = 1
	}

	// 确保重试次数非负
	if event.RetryCount < 0 {
		event.RetryCount = 0
	}

	return event
}

type redactPattern struct {
	re          *regexp.Regexp
	replacement string
}

func (v *EventValidator) sanitizeString(s string) string {
	if s == "" {
		return s
	}
	redacted := s
	for _, p := range v.redactionPatterns {
		redacted = p.re.ReplaceAllString(redacted, p.replacement)
	}
	return strings.TrimSpace(redacted)
}

// 新增：不去除首尾空白的编校，用于保留流式内容里的换行与空格
func (v *EventValidator) sanitizeStringNoTrim(s string) string {
	if s == "" {
		return s
	}
	redacted := s
	for _, p := range v.redactionPatterns {
		redacted = p.re.ReplaceAllString(redacted, p.replacement)
	}
	return redacted
}

// 尝试解析 JSON，并对键/值做深度编校；失败则回退为字符串基于正则的编校
func (v *EventValidator) redactJSONishString(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	var any interface{} //nolint:revive
	if json.Unmarshal([]byte(s), &any) == nil {
		any = v.redactAny(any) //nolint:revive
		if b, err := json.Marshal(any); err == nil {
			return string(b)
		}
	}
	return v.sanitizeString(s)
}

func (v *EventValidator) redactAny(x interface{}) interface{} {
	switch val := x.(type) {
	case map[string]interface{}:
		for k, v2 := range val {
			if v.isSensitiveKey(k) {
				val[k] = "[REDACTED]"
				continue
			}
			// 递归处理嵌套
			val[k] = v.redactAny(v2)
		}
		return val
	case []interface{}:
		for i, elem := range val {
			val[i] = v.redactAny(elem)
		}
		return val
	case string:
		return v.sanitizeString(val)
	default:
		return val
	}
}

func (v *EventValidator) isSensitiveKey(k string) bool {
	lk := strings.ToLower(strings.TrimSpace(k))
	for _, key := range v.redactionKeys {
		if lk == key {
			return true
		}
	}
	return false
}

// SetSensitiveKeys replaces the current sensitive key list
func (v *EventValidator) SetSensitiveKeys(keys []string) {
	if keys == nil {
		v.redactionKeys = []string{}
		return
	}
	// normalize to lowercase and de-duplicate
	seen := make(map[string]struct{})
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "" {
			continue
		}
		if _, ok := seen[lk]; ok {
			continue
		}
		seen[lk] = struct{}{}
		out = append(out, lk)
	}
	v.redactionKeys = out
}

// MergeSensitiveKeys appends new keys avoiding duplicates
func (v *EventValidator) MergeSensitiveKeys(keys []string) {
	if len(keys) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(v.redactionKeys))
	for _, k := range v.redactionKeys {
		seen[k] = struct{}{}
	}
	for _, k := range keys {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "" {
			continue
		}
		if _, ok := seen[lk]; ok {
			continue
		}
		seen[lk] = struct{}{}
		v.redactionKeys = append(v.redactionKeys, lk)
	}
}

// ClearRedactionPatterns removes all regex-based redaction patterns
func (v *EventValidator) ClearRedactionPatterns() {
	v.redactionPatterns = nil
}

// AddRedactionPatternString compiles and adds a regex pattern with replacement
func (v *EventValidator) AddRedactionPatternString(regex, replacement string) error {
	if strings.TrimSpace(regex) == "" {
		return fmt.Errorf("empty regex")
	}
	re, err := regexp.Compile(regex)
	if err != nil {
		return err
	}
	v.redactionPatterns = append(v.redactionPatterns, redactPattern{re: re, replacement: replacement})
	return nil
}

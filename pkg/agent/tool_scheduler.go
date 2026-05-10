package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"strings"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/nano-harness/nano-agent/pkg/tools/system"
)

// toolCallIDKey is the context key for storing the tool call ID
type toolCallIDKey struct{}

// WithToolCallID injects the tool call ID into the context
func WithToolCallID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallIDKey{}, id)
}

// ToolCallIDFromContext extracts the tool call ID from the context
func ToolCallIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(toolCallIDKey{}).(string); ok {
		return id
	}
	return ""
}

// ToolCallStatus represents the status of a tool call
type ToolCallStatus string

const (
	// StatusValidating indicates tool call is being validated
	StatusValidating ToolCallStatus = "validating"
	// StatusScheduled indicates tool call is scheduled
	StatusScheduled ToolCallStatus = "scheduled"
	// StatusExecuting indicates tool call is executing
	StatusExecuting ToolCallStatus = "executing"
	// StatusAwaitingApproval indicates tool call is awaiting approval
	StatusAwaitingApproval ToolCallStatus = "awaiting_approval"
	// StatusSuccess indicates tool call succeeded
	StatusSuccess ToolCallStatus = "success"
	// StatusError indicates tool call failed
	StatusError ToolCallStatus = "error"
	// StatusCancelled indicates tool call was cancelled
	StatusCancelled ToolCallStatus = "cancelled"
)

// approvalWaitExtension is the duration added to the partition timeout when
// one or more tool calls are still awaiting user approval.
const approvalWaitExtension = 10 * time.Minute

// ApprovalDecision describes a user's approval response.
type ApprovalDecision int

const (
	ApprovalReject ApprovalDecision = iota
	ApprovalApproveOnce
	ApprovalApproveAlways
)

// ToolCallInfo represents detailed information about a tool call
type ToolCallInfo struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Parameters       map[string]interface{} `json:"parameters"`
	Status           ToolCallStatus         `json:"status"`
	StartTime        *time.Time             `json:"start_time,omitempty"`
	DurationMs       *int64                 `json:"duration_ms,omitempty"`
	Result           *interfaces.ToolResult `json:"result,omitempty"`
	Error            error                  `json:"error,omitempty"`
	LiveOutput       string                 `json:"live_output,omitempty"`
	RequiresApproval bool                   `json:"requires_approval,omitempty"`
	cancel           context.CancelFunc
}

// ToolScheduler manages parallel execution of tools with advanced state management
type ToolScheduler struct {
	toolbox           *tools.Toolbox
	eventHandler      func(event.StreamEvent)
	recovery          *ToolRecoveryStrategy
	toolCalls         map[string]*ToolCallInfo
	mutex             sync.RWMutex
	onUpdate          func([]*ToolCallInfo)
	onComplete        func([]*ToolCallInfo)
	outputHandler     func(string, string)     // callID, output chunk
	approvalHandler   func(*ToolCallInfo) bool // returns true if approved
	approvalHandlerV2 func(*ToolCallInfo) ApprovalDecision
	allowedExact      map[string]struct{}
	allowedPatterns   []string
	permissionManager *permission.Manager
	// securityDecisions caches the pre-computed security Decision for each tool
	// call ID so it can be injected into the execution context.
	securityDecisions map[string]*middleware.Decision
	// agentConfig is used to check daemon confirm policy when no approval handler is present
	agentConfig *config.Config
	// hookEngine dispatches lifecycle hook events
	hookEngine *middleware.HookEngine
	// workDir is the working directory for hook execution context
	workDir string
}

// SetEventHandler sets the event handler for the tool scheduler and propagates
// it to the underlying ToolRecoveryStrategy so that retry/error events are
// visible through the same event pipeline.
func (ts *ToolScheduler) SetEventHandler(handler func(event.StreamEvent)) {
	ts.eventHandler = handler
	if ts.recovery != nil {
		ts.recovery.SetEventHandler(handler)
	}
}

// SetApprovalHandler sets the approval handler for the tool scheduler.
func (ts *ToolScheduler) SetApprovalHandler(handler func(*ToolCallInfo) bool) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.approvalHandler = handler
}

// SetApprovalHandlerV2 sets an approval handler that can approve once or always.
func (ts *ToolScheduler) SetApprovalHandlerV2(handler func(*ToolCallInfo) ApprovalDecision) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.approvalHandlerV2 = handler
}

// GetApprovalHandler returns the current approval handler.
func (ts *ToolScheduler) GetApprovalHandler() func(*ToolCallInfo) bool {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()
	return ts.approvalHandler
}

// SetPermissionManager sets the permission manager used to bypass confirmations.
func (ts *ToolScheduler) SetPermissionManager(mgr *permission.Manager) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.permissionManager = mgr
}

// SetAgentConfig sets the agent config used to check daemon confirm policy.
func (ts *ToolScheduler) SetAgentConfig(cfg *config.Config) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.agentConfig = cfg
}

// SetHookEngine sets the hook engine for lifecycle event dispatch.
func (ts *ToolScheduler) SetHookEngine(engine *middleware.HookEngine) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.hookEngine = engine
}

// SetWorkDir sets the working directory for hook execution context.
func (ts *ToolScheduler) SetWorkDir(dir string) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.workDir = dir
}

func (ts *ToolScheduler) addAllowlistRules(call *ToolCallInfo) {
	ts.mutex.RLock()
	pm := ts.permissionManager
	ts.mutex.RUnlock()
	if pm == nil || call == nil {
		return
	}
	for _, rule := range permission.BuildAllowlistRules(call.Name, call.Parameters) {
		pm.GetSessionAllowlist().AddRule(rule)
	}
}

// ToolSchedulerOptions contains configuration options for the tool scheduler
type ToolSchedulerOptions struct {
	Toolbox           *tools.Toolbox
	EventHandler      func(event.StreamEvent)
	RecoveryStrategy  *ToolRecoveryStrategy
	OnUpdate          func([]*ToolCallInfo)
	OnComplete        func([]*ToolCallInfo)
	OutputHandler     func(string, string) // callID, output chunk
	ApprovalHandler   func(*ToolCallInfo) bool
	ApprovalHandlerV2 func(*ToolCallInfo) ApprovalDecision
}

// NewToolScheduler creates a new tool scheduler with enhanced capabilities
func NewToolScheduler(toolbox *tools.Toolbox, eventHandler func(event.StreamEvent)) *ToolScheduler {
	return NewToolSchedulerWithOptions(ToolSchedulerOptions{
		Toolbox:      toolbox,
		EventHandler: eventHandler,
	})
}

// NewToolSchedulerWithOptions creates a new tool scheduler with custom options
func NewToolSchedulerWithOptions(opts ToolSchedulerOptions) *ToolScheduler {
	recovery := opts.RecoveryStrategy
	if recovery == nil {
		recovery = NewToolRecoveryStrategy(opts.EventHandler)
	}
	return &ToolScheduler{
		toolbox:           opts.Toolbox,
		eventHandler:      opts.EventHandler,
		recovery:          recovery,
		toolCalls:         make(map[string]*ToolCallInfo),
		onUpdate:          opts.OnUpdate,
		onComplete:        opts.OnComplete,
		outputHandler:     opts.OutputHandler,
		approvalHandler:   opts.ApprovalHandler,
		approvalHandlerV2: opts.ApprovalHandlerV2,
		allowedExact:      nil,
		allowedPatterns:   nil,
		securityDecisions: make(map[string]*middleware.Decision),
	}
}

// SetAllowedTools restricts execution to specific tools
func (ts *ToolScheduler) SetAllowedTools(names []string) {
	ts.mutex.Lock()
	if len(names) == 0 {
		ts.allowedExact = nil
		ts.allowedPatterns = nil
		ts.mutex.Unlock()
		return
	}
	m := make(map[string]struct{}, len(names))
	var pats []string
	for _, n := range names {
		if n == "" {
			continue
		}
		if strings.ContainsAny(n, "*?") {
			pats = append(pats, n)
		} else {
			m[n] = struct{}{}
		}
	}
	ts.allowedExact = m
	ts.allowedPatterns = pats
	ts.mutex.Unlock()
}

// ClearAllowedTools clears tool restrictions
func (ts *ToolScheduler) ClearAllowedTools() {
	ts.mutex.Lock()
	ts.allowedExact = nil
	ts.allowedPatterns = nil
	ts.mutex.Unlock()
}

// setStatus updates the status of a tool call and triggers callbacks
func (ts *ToolScheduler) setStatus(callID string, status ToolCallStatus, result *interfaces.ToolResult, err error) {
	ts.mutex.Lock()

	toolCall, exists := ts.toolCalls[callID]
	if !exists {
		ts.mutex.Unlock()
		logger.Warnf("DEBUG setStatus: toolCall %s not found", callID)
		return
	}

	oldStatus := toolCall.Status
	if status == StatusError {
		errMsg := "unknown"
		if result != nil && result.Error != "" {
			errMsg = result.Error
		} else if err != nil {
			errMsg = err.Error()
		}
		logger.Infof("DEBUG setStatus: %s %s -> %s (tool: %s, error: %s)", callID, oldStatus, status, toolCall.Name, errMsg)
	} else {
		logger.Infof("DEBUG setStatus: %s %s -> %s (tool: %s)", callID, oldStatus, status, toolCall.Name)
	}

	if (toolCall.Status == StatusSuccess || toolCall.Status == StatusError || toolCall.Status == StatusCancelled) && toolCall.Result != nil {
		ts.mutex.Unlock()
		return
	}

	// Calculate duration if transitioning to terminal state
	if toolCall.StartTime != nil && (status == StatusSuccess || status == StatusError || status == StatusCancelled) {
		duration := time.Since(*toolCall.StartTime).Milliseconds()
		toolCall.DurationMs = &duration
	}

	toolCall.Status = status
	if result != nil {
		toolCall.Result = result
	}
	if err != nil {
		toolCall.Error = err
	}

	var toolName string
	var durationMs *int64
	var success *bool
	var errorStr string
	var parameters map[string]interface{}
	if toolCall != nil {
		toolName = toolCall.Name
		parameters = toolCall.Parameters
		durationMs = toolCall.DurationMs
		if toolCall.Result != nil {
			s := toolCall.Result.Success
			success = &s
			errorStr = toolCall.Result.Error
		}
		if errorStr == "" && toolCall.Error != nil {
			errorStr = toolCall.Error.Error()
		}
	}

	// Capture callback and data before releasing lock
	onUpdate := ts.onUpdate
	onComplete := ts.onComplete

	// Create copies of tool calls for callbacks
	var updateCalls []*ToolCallInfo
	var completeCalls []*ToolCallInfo

	if onUpdate != nil {
		updateCalls = make([]*ToolCallInfo, 0, len(ts.toolCalls))
		for _, call := range ts.toolCalls {
			// Create a shallow copy to avoid data races
			callCopy := *call
			// Deep copy pointer fields to avoid data race
			if call.Result != nil {
				resultCopy := *call.Result
				callCopy.Result = &resultCopy
			}
			updateCalls = append(updateCalls, &callCopy)
		}
	}

	// Check if all calls are complete
	allComplete := true
	for _, call := range ts.toolCalls {
		if call.Status != StatusSuccess && call.Status != StatusError && call.Status != StatusCancelled {
			allComplete = false
			break
		}
	}

	if allComplete && len(ts.toolCalls) > 0 && onComplete != nil {
		completeCalls = make([]*ToolCallInfo, 0, len(ts.toolCalls))
		for _, call := range ts.toolCalls {
			// Create a shallow copy to avoid data races
			callCopy := *call
			// Deep copy pointer fields to avoid data race
			if call.Result != nil {
				resultCopy := *call.Result
				callCopy.Result = &resultCopy
			}
			completeCalls = append(completeCalls, &callCopy)
		}
	}

	ts.mutex.Unlock()

	if ts.eventHandler != nil {
		isTerminal := status == StatusSuccess || status == StatusError || status == StatusCancelled
		evType := event.EventTypeWorkerUpdate
		if isTerminal {
			evType = event.EventTypeWorkerEnd
		}
		ev := event.NewStreamEvent(evType, "agent_tool_scheduler")
		ev.WorkerID = callID
		ev = ev.WithContent(string(status))
		ev = ev.WithMetadata("tool_name", toolName)
		if durationMs != nil {
			ev = ev.WithMetadata("duration_ms", *durationMs)
		}
		if success != nil {
			ev = ev.WithMetadata("success", *success)
		}
		if errorStr != "" {
			ev = ev.WithMetadata("error", errorStr)
		}
		ts.eventHandler(ev)
		if status == StatusAwaitingApproval {
			ts.eventHandler(event.StreamEvent{
				Type: event.EventTypeWaitingForUser,
				Metadata: map[string]interface{}{
					"kind":       "tool_approval_request",
					"call_id":    callID,
					"tool_name":  toolName,
					"parameters": parameters,
					"status":     string(status),
				},
			})
		}
	}

	// Call callbacks outside of lock
	if onUpdate != nil && updateCalls != nil {
		onUpdate(updateCalls)
	}

	if onComplete != nil && completeCalls != nil {
		onComplete(completeCalls)
	}
}

// Schedule schedules tool calls for execution with validation and approval flow
func (ts *ToolScheduler) Schedule(ctx context.Context, calls []ToolToExecute) error {
	logger.Infof("Scheduling %d tools for execution", len(calls))

	if len(calls) == 0 {
		return nil
	}

	// Allow concurrent execution - remove the blocking check
	// This enables parallel tool execution as intended

	// Create tool call info for each tool
	for _, tool := range calls {
		now := time.Now()
		toolCall := &ToolCallInfo{
			ID:         tool.ID,
			Name:       tool.Name,
			Parameters: tool.Parameters,
			Status:     StatusValidating,
			StartTime:  &now,
		}

		// Check if tool exists
		toolInstance, exists := ts.toolbox.Get(tool.Name)
		if !exists {
			tr := &interfaces.ToolResult{
				Success:     false,
				Error:       "tool not found",
				Metadata:    map[string]interface{}{"code": "tool_not_found", "tool_name": tool.Name},
				LLMContent:  fmt.Sprintf("Tool '%s' call failed: tool not found.", tool.Name),
				UserContent: fmt.Sprintf("工具 '%s' 不存在，无法执行。", tool.Name),
			}
			ts.mutex.Lock()
			ts.toolCalls[tool.ID] = toolCall
			ts.mutex.Unlock()
			if ts.eventHandler != nil {
				start := event.NewStreamEvent(event.EventTypeWorkerStart, "agent_tool_scheduler")
				start.WorkerID = toolCall.ID
				start = start.WithContent(toolCall.Name)
				start = start.WithMetadata("status", string(toolCall.Status))
				ts.eventHandler(start)

				ts.eventHandler(event.StreamEvent{
					Type: event.EventTypeToolCall,
					ToolCalls: []*tools.ToolCall{{
						ID:        toolCall.ID,
						Name:      toolCall.Name,
						Arguments: toolCall.Parameters,
					}},
				})
				ts.eventHandler(event.StreamEvent{
					Type: event.EventTypeToolResult,
					ToolResult: &tools.ToolResult{
						ID:      toolCall.ID,
						Content: tr.LLMContent,
						Error:   tr.Error,
					},
				})
				ts.eventHandler(event.StreamEvent{
					Type:   event.EventTypeToolUse,
					Source: "agent_turn",
					ToolUse: &event.ToolUse{
						ID:         toolCall.ID,
						ToolName:   toolCall.Name,
						Parameters: toolCall.Parameters,
						Status:     string(StatusError),
						Result:     tr.UserContent,
					},
				})
			}
			ts.setStatus(tool.ID, StatusError, tr, fmt.Errorf("tool '%s' not found", tool.Name))
			continue
		}

		// Dispatch PreToolUse hook
		ts.mutex.RLock()
		hookEngine := ts.hookEngine
		ts.mutex.RUnlock()
		hookRequiresApproval := false
		if hookEngine != nil {
			hookDecision, err := hookEngine.Execute(ctx, middleware.HookPreToolUse, tool.Name, tool.Parameters)
			if err != nil {
				logger.Warnf("PreToolUse hook execution error for tool %s: %v", tool.Name, err)
			}
			if hookDecision != nil {
				switch hookDecision.Action {
				case middleware.ActionBlock:
					// Hook blocked the tool execution
					tr := &interfaces.ToolResult{
						Success:     false,
						Error:       "blocked by hook",
						Metadata:    map[string]interface{}{"code": "hook_blocked", "tool_name": tool.Name, "hook_rule": hookDecision.Rule},
						LLMContent:  fmt.Sprintf("Tool '%s' call blocked by hook: %s", tool.Name, hookDecision.Reason),
						UserContent: fmt.Sprintf("工具 '%s' 被 hook 阻止: %s", tool.Name, hookDecision.Reason),
					}
					ts.mutex.Lock()
					ts.toolCalls[tool.ID] = toolCall
					ts.mutex.Unlock()
					if ts.eventHandler != nil {
						ts.eventHandler(event.StreamEvent{
							Type: event.EventTypeToolCall,
							ToolCalls: []*tools.ToolCall{{
								ID:        toolCall.ID,
								Name:      toolCall.Name,
								Arguments: toolCall.Parameters,
							}},
						})
						ts.eventHandler(event.StreamEvent{
							Type: event.EventTypeToolResult,
							ToolResult: &tools.ToolResult{
								ID:      toolCall.ID,
								Content: tr.LLMContent,
								Error:   tr.Error,
							},
						})
						ts.eventHandler(event.StreamEvent{
							Type:   event.EventTypeToolUse,
							Source: "agent_turn",
							ToolUse: &event.ToolUse{
								ID:         toolCall.ID,
								ToolName:   toolCall.Name,
								Parameters: toolCall.Parameters,
								Status:     string(StatusError),
								Result:     tr.UserContent,
							},
						})
					}
					ts.setStatus(tool.ID, StatusError, tr, fmt.Errorf("blocked by hook: %s", hookDecision.Reason))
					continue
				case middleware.ActionConfirm:
					// Hook requires confirmation - mark for approval
					toolCall.RequiresApproval = true
					hookRequiresApproval = true
				case middleware.ActionAllow:
					// Hook allows - apply any parameter modifications
					if len(hookDecision.ModifiedParams) > 0 {
						tool.Parameters = middleware.MergeDecisionParams(tool.Parameters, hookDecision.ModifiedParams)
						toolCall.Parameters = tool.Parameters
					}
				}
			}
		}

		// Enforce allowed tools whitelist if set
		policy := ts.policyEngine()
		preflight := policy.PreflightTool(ctx, tool.Name, tool.Parameters, toolInstance)
		if preflight.HasAllowPolicy && !preflight.Allowed {
			tr := &interfaces.ToolResult{
				Success:     false,
				Error:       "tool not allowed",
				Metadata:    map[string]interface{}{"code": "not_allowed", "tool_name": tool.Name},
				LLMContent:  fmt.Sprintf("Tool '%s' call rejected: not allowed in current command context.", tool.Name),
				UserContent: fmt.Sprintf("工具 '%s' 在当前命令上下文不被允许。", tool.Name),
			}
			ts.mutex.Lock()
			ts.toolCalls[tool.ID] = toolCall
			ts.mutex.Unlock()
			// Emit events
			if ts.eventHandler != nil {
				start := event.NewStreamEvent(event.EventTypeWorkerStart, "agent_tool_scheduler")
				start.WorkerID = toolCall.ID
				start = start.WithContent(toolCall.Name)
				start = start.WithMetadata("status", string(toolCall.Status))
				ts.eventHandler(start)

				ts.eventHandler(event.StreamEvent{
					Type: event.EventTypeToolCall,
					ToolCalls: []*tools.ToolCall{{
						ID:        toolCall.ID,
						Name:      toolCall.Name,
						Arguments: toolCall.Parameters,
					}},
				})
				ts.eventHandler(event.StreamEvent{
					Type: event.EventTypeToolResult,
					ToolResult: &tools.ToolResult{
						ID:      toolCall.ID,
						Content: tr.LLMContent,
						Error:   tr.Error,
					},
				})
				ts.eventHandler(event.StreamEvent{
					Type:   event.EventTypeToolUse,
					Source: "agent_turn",
					ToolUse: &event.ToolUse{
						ID:         toolCall.ID,
						ToolName:   toolCall.Name,
						Parameters: toolCall.Parameters,
						Status:     string(StatusError),
						Result:     tr.UserContent,
					},
				})
			}
			ts.setStatus(tool.ID, StatusError, tr, fmt.Errorf("not allowed"))
			continue
		}

		// Check if tool requires confirmation.
		toolCall.RequiresApproval = hookRequiresApproval || preflight.RequiresApproval

		// Unified security pre-analysis for tools that support it.
		// This is the single source of truth: if the tool provides a security
		// decision here we store it for later context injection, immediately reject
		// ActionBlock commands, and override RequiresApproval for ActionAllow.
		analysis := preflight.SecurityAnalysis
		if analysis.Supported {
			if analysis.Err != nil {
				// Analysis error: treat as a hard failure, do not schedule execution.
				err := analysis.Err
				tr := &interfaces.ToolResult{
					Success:     false,
					Error:       "command security analysis failed: " + err.Error(),
					Metadata:    map[string]interface{}{"code": "security_analysis_error", "tool_name": tool.Name},
					LLMContent:  fmt.Sprintf("Tool '%s' security analysis failed: %v", tool.Name, err),
					UserContent: fmt.Sprintf("工具 '%s' 安全分析失败: %v", tool.Name, err),
				}
				ts.mutex.Lock()
				ts.toolCalls[tool.ID] = toolCall
				ts.mutex.Unlock()
				if ts.eventHandler != nil {
					ts.eventHandler(event.StreamEvent{
						Type: event.EventTypeToolCall,
						ToolCalls: []*tools.ToolCall{{
							ID:        toolCall.ID,
							Name:      toolCall.Name,
							Arguments: toolCall.Parameters,
						}},
					})
					ts.eventHandler(event.StreamEvent{
						Type: event.EventTypeToolResult,
						ToolResult: &tools.ToolResult{
							ID:      toolCall.ID,
							Content: tr.LLMContent,
							Error:   tr.Error,
						},
					})
					ts.eventHandler(event.StreamEvent{
						Type:   event.EventTypeToolUse,
						Source: "agent_turn",
						ToolUse: &event.ToolUse{
							ID:         toolCall.ID,
							ToolName:   toolCall.Name,
							Parameters: toolCall.Parameters,
							Status:     string(StatusError),
							Result:     tr.UserContent,
						},
					})
				}
				ts.setStatus(tool.ID, StatusError, tr, err)
				continue
			}

			decision := analysis.Decision
			switch decision.Action {
			case middleware.ActionBlock:
				// Immediately reject – no confirmation dialog.
				tr := &interfaces.ToolResult{
					Success:     false,
					Error:       "command blocked by security policy: " + decision.Reason,
					Metadata:    map[string]interface{}{"code": "security_blocked", "tool_name": tool.Name},
					LLMContent:  fmt.Sprintf("run_shell_command blocked by security: %s", decision.Reason),
					UserContent: fmt.Sprintf("❌ Command blocked: %s", decision.Reason),
				}
				ts.mutex.Lock()
				ts.toolCalls[tool.ID] = toolCall
				ts.mutex.Unlock()
				if ts.eventHandler != nil {
					start := event.NewStreamEvent(event.EventTypeWorkerStart, "agent_tool_scheduler")
					start.WorkerID = toolCall.ID
					start = start.WithContent(toolCall.Name)
					start = start.WithMetadata("status", string(toolCall.Status))
					ts.eventHandler(start)
					ts.eventHandler(event.StreamEvent{
						Type: event.EventTypeToolCall,
						ToolCalls: []*tools.ToolCall{{
							ID:        toolCall.ID,
							Name:      toolCall.Name,
							Arguments: toolCall.Parameters,
						}},
					})
					ts.eventHandler(event.StreamEvent{
						Type: event.EventTypeToolResult,
						ToolResult: &tools.ToolResult{
							ID:      toolCall.ID,
							Content: tr.LLMContent,
							Error:   tr.Error,
						},
					})
					ts.eventHandler(event.StreamEvent{
						Type:   event.EventTypeToolUse,
						Source: "agent_turn",
						ToolUse: &event.ToolUse{
							ID:         toolCall.ID,
							ToolName:   toolCall.Name,
							Parameters: toolCall.Parameters,
							Status:     string(StatusError),
							Result:     tr.UserContent,
						},
					})
				}
				ts.setStatus(tool.ID, StatusError, tr, fmt.Errorf("command blocked: %s", decision.Reason))
				continue
			case middleware.ActionAllow:
				// Safe command: no user confirmation needed.
				if !hookRequiresApproval {
					toolCall.RequiresApproval = false
				}
			case middleware.ActionConfirm:
				// Check if confidence is high enough to skip confirmation.
				ts.mutex.RLock()
				pm := ts.permissionManager
				ts.mutex.RUnlock()
				if pm != nil && pm.GetSessionAllowlist().IsAllowed(tool.Name, tool.Parameters) {
					toolCall.RequiresApproval = false
				} else if pm != nil && decision.Confidence >= pm.GetConfidenceThreshold() {
					// High-confidence confirm → auto-approve, add to session allowlist
					toolCall.RequiresApproval = false
					if decision.AutoWhitelist {
						// Auto-add to session allowlist for future calls
						pm.GetSessionAllowlist().AddRule(permission.PermissionRule{
							ToolName:   tool.Name,
							RawPattern: tool.Name,
						})
					}
				} else {
					// Confirmation required: explicitly enforce approval even if earlier checks did not.
					toolCall.RequiresApproval = true
				}
			}
			if len(decision.ModifiedParams) > 0 && decision.Action != middleware.ActionBlock {
				tool.Parameters = middleware.MergeDecisionParams(tool.Parameters, decision.ModifiedParams)
				toolCall.Parameters = tool.Parameters
			}
			// Store decision for context injection during execution.
			ts.mutex.Lock()
			ts.securityDecisions[tool.ID] = decision
			ts.mutex.Unlock()
		}

		// Store the call first so that any events can reference it by ID
		ts.mutex.Lock()
		ts.toolCalls[tool.ID] = toolCall
		ts.mutex.Unlock()
		if ts.eventHandler != nil {
			start := event.NewStreamEvent(event.EventTypeWorkerStart, "agent_tool_scheduler")
			start.WorkerID = toolCall.ID
			start = start.WithContent(toolCall.Name)
			start = start.WithMetadata("status", string(toolCall.Status))
			ts.eventHandler(start)
		}

		// Validate required parameters using tool schema
		schema := toolInstance.Schema()
		if schema != nil && len(schema.Required) > 0 {
			missing := make([]string, 0)
			for _, req := range schema.Required {
				if tool.Parameters == nil {
					missing = append(missing, req)
					continue
				}
				if v, ok := tool.Parameters[req]; !ok || v == nil {
					missing = append(missing, req)
				}
			}
			if len(missing) > 0 {
				// Build structured ToolResult error
				tr := &interfaces.ToolResult{
					Success:     false,
					Error:       "missing required parameters",
					Metadata:    map[string]interface{}{"code": "missing_required_parameters", "missing_fields": missing, "tool_name": tool.Name},
					LLMContent:  fmt.Sprintf("Tool '%s' call failed: missing required parameters: %v. Please provide the missing fields in arguments.", tool.Name, missing),
					UserContent: fmt.Sprintf("Tool %s cannot run because required parameters are missing: %v", tool.Name, missing),
				}

				// Emit tool call event so the LLM sees the attempted call
				if ts.eventHandler != nil {
					ts.eventHandler(event.StreamEvent{
						Type: event.EventTypeToolCall,
						ToolCalls: []*tools.ToolCall{{
							ID:        toolCall.ID,
							Name:      toolCall.Name,
							Arguments: toolCall.Parameters,
						}},
					})
				}

				// Emit immediate tool result with structured error
				if ts.eventHandler != nil {
					ts.eventHandler(event.StreamEvent{
						Type: event.EventTypeToolResult,
						ToolResult: &tools.ToolResult{
							ID:      toolCall.ID,
							Content: tr.LLMContent,
							Error:   tr.Error,
						},
					})
				}

				// Update TUI tool use entry with error
				if ts.eventHandler != nil {
					ts.eventHandler(event.StreamEvent{
						Type:   event.EventTypeToolUse,
						Source: "agent_turn",
						ToolUse: &event.ToolUse{
							ID:         toolCall.ID,
							ToolName:   toolCall.Name,
							Parameters: toolCall.Parameters,
							Status:     string(StatusError),
							Result:     tr.LLMContent,
						},
					})
				}

				// Set terminal error status and do not schedule execution
				ts.setStatus(tool.ID, StatusError, tr, fmt.Errorf("%s: %v", tr.Error, tr.Metadata["missing_fields"]))
				continue
			}
		}

		// Handle approval flow
		if toolCall.RequiresApproval {
			if ts.approvalHandlerV2 != nil {
				ts.setStatus(tool.ID, StatusAwaitingApproval, nil, nil)
				switch ts.approvalHandlerV2(toolCall) {
				case ApprovalApproveAlways:
					ts.addAllowlistRules(toolCall)
					ts.setStatus(tool.ID, StatusScheduled, nil, nil)
				case ApprovalApproveOnce:
					ts.setStatus(tool.ID, StatusScheduled, nil, nil)
				case ApprovalReject:
					tr := &interfaces.ToolResult{
						Success:     false,
						Error:       "cancelled by user",
						Metadata:    map[string]interface{}{"status": "cancelled", "reason": "user_declined"},
						LLMContent:  "Tool execution was cancelled by user.",
						UserContent: "Tool execution was cancelled by user.",
					}
					tr = ts.normalizeToolResultFields(toolCall.Name, tr, nil)
					ts.setStatus(tool.ID, StatusCancelled, tr, fmt.Errorf("cancelled by user"))
				}
			} else if ts.approvalHandler != nil {
				ts.setStatus(tool.ID, StatusAwaitingApproval, nil, nil)
				// In TUI mode the handler is async; true means sync-approved.
				if ts.approvalHandler(toolCall) {
					ts.setStatus(tool.ID, StatusScheduled, nil, nil)
				}
				// else: remain in StatusAwaitingApproval until HandleConfirmationResponse is called
			} else {
				// No approval handler: check daemon confirm policy
				ts.mutex.RLock()
				cfg := ts.agentConfig
				ts.mutex.RUnlock()

				if cfg != nil && cfg.Daemon != nil {
					switch cfg.Daemon.ConfirmPolicy {
					case config.ConfirmPolicyAllow:
						// Auto-approve
						logger.Debugf("Tool %s auto-approved by daemon confirm policy: allow", tool.Name)
						ts.setStatus(tool.ID, StatusScheduled, nil, nil)
					case config.ConfirmPolicyBlock:
						// Reject
						tr := &interfaces.ToolResult{
							Success:     false,
							Error:       "tool requires approval but daemon confirm policy is 'block'",
							Metadata:    map[string]interface{}{"code": "approval_blocked", "tool_name": tool.Name},
							LLMContent:  fmt.Sprintf("Tool '%s' call rejected: requires approval but daemon confirm policy is 'block'.", tool.Name),
							UserContent: fmt.Sprintf("工具 '%s' 需要确认但守护进程确认策略为 'block'，已拒绝。", tool.Name),
						}
						if ts.eventHandler != nil {
							ts.eventHandler(event.StreamEvent{
								Type: event.EventTypeToolResult,
								ToolResult: &tools.ToolResult{
									ID:      toolCall.ID,
									Content: tr.LLMContent,
									Error:   tr.Error,
								},
							})
							ts.eventHandler(event.StreamEvent{
								Type:   event.EventTypeToolUse,
								Source: "agent_turn",
								ToolUse: &event.ToolUse{
									ID:         toolCall.ID,
									ToolName:   toolCall.Name,
									Parameters: toolCall.Parameters,
									Status:     string(StatusError),
									Result:     tr.UserContent,
								},
							})
						}
						ts.setStatus(tool.ID, StatusError, tr, fmt.Errorf("approval blocked by daemon policy"))
						continue
					case config.ConfirmPolicyAllowlist:
						// Check if tool is in allowlist
						allowed := false
						for _, allowedTool := range cfg.Daemon.AllowlistedTools {
							if allowedTool == tool.Name {
								allowed = true
								break
							}
						}
						if allowed {
							logger.Debugf("Tool %s auto-approved by daemon allowlist", tool.Name)
							ts.setStatus(tool.ID, StatusScheduled, nil, nil)
						} else {
							// Not in allowlist: reject
							tr := &interfaces.ToolResult{
								Success:     false,
								Error:       "tool requires approval but is not in daemon allowlist",
								Metadata:    map[string]interface{}{"code": "not_in_allowlist", "tool_name": tool.Name},
								LLMContent:  fmt.Sprintf("Tool '%s' call rejected: requires approval but is not in daemon allowlist.", tool.Name),
								UserContent: fmt.Sprintf("工具 '%s' 需要确认但不在守护进程白名单中，已拒绝。", tool.Name),
							}
							if ts.eventHandler != nil {
								ts.eventHandler(event.StreamEvent{
									Type: event.EventTypeToolResult,
									ToolResult: &tools.ToolResult{
										ID:      toolCall.ID,
										Content: tr.LLMContent,
										Error:   tr.Error,
									},
								})
								ts.eventHandler(event.StreamEvent{
									Type:   event.EventTypeToolUse,
									Source: "agent_turn",
									ToolUse: &event.ToolUse{
										ID:         toolCall.ID,
										ToolName:   toolCall.Name,
										Parameters: toolCall.Parameters,
										Status:     string(StatusError),
										Result:     tr.UserContent,
									},
								})
							}
							ts.setStatus(tool.ID, StatusError, tr, fmt.Errorf("not in allowlist"))
							continue
						}
					default:
						// Unknown policy: fail-safe to allow (backward compatibility)
						logger.Warnf("Unknown daemon confirm policy %s, defaulting to allow", cfg.Daemon.ConfirmPolicy)
						ts.setStatus(tool.ID, StatusScheduled, nil, nil)
					}
				} else {
					// No daemon config: default to allow for backward compatibility
					logger.Debugf("Tool %s auto-approved (no daemon config)", tool.Name)
					ts.setStatus(tool.ID, StatusScheduled, nil, nil)
				}
			}
		} else {
			ts.setStatus(tool.ID, StatusScheduled, nil, nil)
		}
	}

	// Execute scheduled tools
	ts.executeScheduledCalls(ctx)
	return nil
}

// ExecuteParallel executes multiple tools respecting concurrency safety.
//
// Tools are grouped into sequential "partitions":
//   - A run of consecutive concurrency-safe tools forms one parallel partition.
//   - Every unsafe tool forms its own singleton partition.
//
// Partitions are executed serially; within each parallel partition all tools run
// concurrently. This prevents data races (e.g. simultaneous file writes) while
// still exploiting parallelism for read-only operations.
func (ts *ToolScheduler) ExecuteParallel(ctx context.Context, toolsToExec []ToolToExecute) (map[string]*interfaces.ToolResult, error) {
	logger.Infof("Executing %d tools with concurrency-aware partitioning", len(toolsToExec))

	if len(toolsToExec) == 0 {
		return make(map[string]*interfaces.ToolResult), nil
	}

	// Build partitions
	partitions := ts.partitionTools(toolsToExec)
	logger.Infof("Partitioned %d tools into %d partition(s)", len(toolsToExec), len(partitions))

	allResults := make(map[string]*interfaces.ToolResult)

	for pIdx, partition := range partitions {
		logger.Infof("Executing partition %d/%d (%d tool(s), parallel=%t)",
			pIdx+1, len(partitions), len(partition), len(partition) > 1)

		results, err := ts.executePartition(ctx, partition)
		if err != nil {
			return nil, err
		}
		for id, r := range results {
			allResults[id] = r
		}
	}

	return allResults, nil
}

// partitionTools groups tools into sequential partitions based on ConcurrencySafe().
// Consecutive safe tools are merged into one parallel partition; each unsafe tool
// becomes its own singleton partition.
func (ts *ToolScheduler) partitionTools(toolsToExec []ToolToExecute) [][]ToolToExecute {
	var partitions [][]ToolToExecute
	var currentSafe []ToolToExecute

	for _, t := range toolsToExec {
		safe := false
		if toolInstance, exists := ts.toolbox.Get(t.Name); exists {
			safe = toolInstance.ConcurrencySafe()
		}

		if safe {
			currentSafe = append(currentSafe, t)
		} else {
			// Flush any pending safe batch first
			if len(currentSafe) > 0 {
				partitions = append(partitions, currentSafe)
				currentSafe = nil
			}
			// Unsafe tool is its own singleton partition
			partitions = append(partitions, []ToolToExecute{t})
		}
	}
	// Flush remaining safe batch
	if len(currentSafe) > 0 {
		partitions = append(partitions, currentSafe)
	}

	return partitions
}

// executePartition schedules and waits for a single partition of tools.
func (ts *ToolScheduler) executePartition(ctx context.Context, partition []ToolToExecute) (map[string]*interfaces.ToolResult, error) {
	logger.Infof("Executing %d tools in partition using polling", len(partition))

	// Use the scheduling system to start the tools
	err := ts.Schedule(ctx, partition)
	if err != nil {
		return nil, err
	}

	// --- Polling mechanism to wait for completion ---

	ourToolIDs := make(map[string]struct{}, len(partition))
	for _, t := range partition {
		ourToolIDs[t.ID] = struct{}{}
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Use context timeout if available, otherwise default to 10 minutes
	timeoutDuration := 10 * time.Minute
	if deadline, ok := ctx.Deadline(); ok {
		timeoutDuration = time.Until(deadline)
	}
	timeout := time.After(timeoutDuration)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			// Check if any tools are still awaiting approval - if so, extend the wait.
			ts.mutex.RLock()
			hasAwaitingApproval := false
			for id := range ourToolIDs {
				if call, exists := ts.toolCalls[id]; exists {
					if call.Status == StatusAwaitingApproval {
						hasAwaitingApproval = true
						break
					}
				}
			}
			ts.mutex.RUnlock()

			if hasAwaitingApproval {
				// Reset timeout and keep waiting for user approval.
				timeout = time.After(approvalWaitExtension)
				continue
			}

			// No awaiting-approval tools: handle execution timeout for remaining tools.
			// We do NOT propagate a hard error here. Instead we generate synthetic failure results
			// for every incomplete tool so that:
			//   1. The LLM context stays consistent (every tool_call has a matching tool message).
			//   2. The Turn loop can continue – the LLM can decide how to handle the failures.
			type timeoutUpdate struct {
				callID string
				status ToolCallStatus
				result *interfaces.ToolResult
				err    error
			}
			updates := make([]timeoutUpdate, 0, len(ourToolIDs))
			cancels := make([]context.CancelFunc, 0)

			ts.mutex.RLock()
			for id := range ourToolIDs {
				if call, exists := ts.toolCalls[id]; exists {
					if call.Status != StatusSuccess && call.Status != StatusError && call.Status != StatusCancelled {
						if call.cancel != nil {
							cancels = append(cancels, call.cancel)
						}
						tr := &interfaces.ToolResult{
							Success:     false,
							Error:       "execution timeout",
							Metadata:    map[string]interface{}{"code": "execution_timeout", "status": "timeout", "tool_name": call.Name},
							LLMContent:  fmt.Sprintf("Tool '%s' execution timed out.", call.Name),
							UserContent: fmt.Sprintf("工具 '%s' 执行超时。", call.Name),
						}

						if ts.eventHandler != nil {
							ts.eventHandler(event.StreamEvent{
								Type: event.EventTypeToolResult,
								ToolResult: &tools.ToolResult{
									ID:      call.ID,
									Content: tr.LLMContent,
									Error:   tr.Error,
								},
							})
							ts.eventHandler(event.StreamEvent{
								Type:   event.EventTypeToolUse,
								Source: "agent_turn",
								ToolUse: &event.ToolUse{
									ID:         call.ID,
									ToolName:   call.Name,
									Parameters: call.Parameters,
									Status:     string(StatusError),
									Result:     tr.UserContent,
								},
							})
						}

						updates = append(updates, timeoutUpdate{
							callID: call.ID,
							status: StatusError,
							result: tr,
							err:    fmt.Errorf("execution timeout"),
						})
					}
				}
			}
			ts.mutex.RUnlock()
			for _, cancel := range cancels {
				cancel()
			}
			for _, u := range updates {
				ts.setStatus(u.callID, u.status, u.result, u.err)
			}

			// Collect all results (completed before timeout + synthetic timeout results).
			// Return them so the Turn can add them to the LLM context and keep running.
			ts.mutex.RLock()
			timeoutResults := make(map[string]*interfaces.ToolResult)
			for id := range ourToolIDs {
				if call, ok := ts.toolCalls[id]; ok && call.Result != nil {
					timeoutResults[id] = call.Result
				}
			}
			ts.mutex.RUnlock()

			// Best-effort clean-up for this partition.
			toolIDsToClean := make([]string, 0, len(ourToolIDs))
			for id := range ourToolIDs {
				toolIDsToClean = append(toolIDsToClean, id)
			}
			go ts.ClearSpecificToolCalls(toolIDsToClean)

			logger.Warnf("Tool partition timed out after %v; returning %d partial results to LLM context",
				timeoutDuration, len(timeoutResults))
			return timeoutResults, nil
		case <-ticker.C:
			ts.mutex.RLock()
			results := make(map[string]*interfaces.ToolResult)
			completedCount := 0

			for id := range ourToolIDs {
				call, ok := ts.toolCalls[id]
				if !ok {
					completedCount++
					continue
				}
				if call.Status == StatusSuccess || call.Status == StatusError || call.Status == StatusCancelled {
					completedCount++
					if call.Result != nil {
						results[id] = call.Result
					}
				}
			}
			ts.mutex.RUnlock()

			if completedCount >= len(partition) {
				toolIDsToClean := make([]string, 0, len(ourToolIDs))
				for id := range ourToolIDs {
					toolIDsToClean = append(toolIDsToClean, id)
				}
				go ts.ClearSpecificToolCalls(toolIDsToClean)
				return results, nil
			}
		}
	}
}

func (ts *ToolScheduler) executeScheduledCalls(ctx context.Context) {
	ts.mutex.RLock()
	scheduledCalls := make([]*ToolCallInfo, 0)
	for _, call := range ts.toolCalls {
		if call.Status == StatusScheduled {
			scheduledCalls = append(scheduledCalls, call)
		}
	}
	ts.mutex.RUnlock()

	logger.Infof("DEBUG executeScheduledCalls: launching %d tool goroutines", len(scheduledCalls))
	for _, call := range scheduledCalls {
		logger.Infof("DEBUG executeScheduledCalls: launching goroutine for tool %s (ID: %s)", call.Name, call.ID)
		go ts.executeSingleToolCall(ctx, call)
	}
}

// normalizeToolResultFields ensures ToolResult has consistent LLMContent/UserContent defaults and enriched metadata
func (ts *ToolScheduler) normalizeToolResultFields(toolName string, tr *interfaces.ToolResult, execRes *ToolExecutionResult) *interfaces.ToolResult {
	if tr == nil {
		tr = &interfaces.ToolResult{}
	}
	if tr.Metadata == nil {
		tr.Metadata = map[string]interface{}{}
	}
	if execRes != nil && execRes.Error != nil {
		var trfe *ToolResultFailureError
		if errors.As(execRes.Error, &trfe) && trfe != nil && trfe.Code != "" {
			if _, ok := tr.Metadata["code"]; !ok {
				tr.Metadata["code"] = trfe.Code
			}
		}
	}
	// Always attach tool name
	if _, ok := tr.Metadata["tool_name"]; !ok {
		tr.Metadata["tool_name"] = toolName
	}
	// Attach recovery stats if available (non-destructively)
	if execRes != nil {
		if execRes.Attempts > 0 {
			if _, ok := tr.Metadata["attempts"]; !ok {
				tr.Metadata["attempts"] = execRes.Attempts
			}
		}
		if execRes.TotalTime > 0 {
			if _, ok := tr.Metadata["total_time_ms"]; !ok {
				tr.Metadata["total_time_ms"] = execRes.TotalTime.Milliseconds()
			}
		}
		if execRes.ErrorCategory != "" {
			if _, ok := tr.Metadata["error_category"]; !ok {
				tr.Metadata["error_category"] = execRes.ErrorCategory
			}
		}
		if execRes.RecoveryInfo != nil {
			if _, ok := tr.Metadata["recovery_info"]; !ok {
				tr.Metadata["recovery_info"] = execRes.RecoveryInfo
			}
		}
	}

	// Ensure status present in metadata
	if tr.Success {
		if _, ok := tr.Metadata["status"]; !ok {
			tr.Metadata["status"] = "success"
		}
	} else {
		if _, ok := tr.Metadata["status"]; !ok {
			tr.Metadata["status"] = "error"
		}
	}

	// Propagate error string from execRes if result indicates failure and Error empty
	if !tr.Success && tr.Error == "" && execRes != nil && execRes.Error != nil {
		tr.Error = execRes.Error.Error()
	}

	// Fill missing content fields consistently without overriding existing ones
	if tr.LLMContent == "" && tr.UserContent != "" {
		tr.LLMContent = tr.UserContent
	}
	if tr.UserContent == "" && tr.LLMContent != "" {
		tr.UserContent = tr.LLMContent
	}

	// Inject recovery guidance for failed tools to help the LLM understand and recover.
	// This is done after fill-missing so the LLM receives both the original tool output and the guidance.
	if !tr.Success && ts.recovery != nil && execRes != nil && execRes.Error != nil {
		guidance := ts.recovery.GenerateRecoveryGuidance(toolName, execRes.Error, execRes.ErrorCategory)
		if guidance != "" {
			tr.LLMContent = tr.LLMContent + guidance
		}
	}

	// If both are still empty, create concise defaults
	if tr.LLMContent == "" && tr.UserContent == "" {
		if tr.Success {
			msg := fmt.Sprintf("Tool '%s' completed successfully.", toolName)
			tr.LLMContent = msg
			tr.UserContent = msg
		} else {
			errMsg := tr.Error
			if errMsg == "" {
				errMsg = "unknown error"
			}
			msg := fmt.Sprintf("Tool '%s' failed: %s", toolName, errMsg)
			tr.LLMContent = msg
			tr.UserContent = msg
		}
	}

	return tr
}

// executeSingleToolCall executes a single tool call with enhanced error handling and live output
func (ts *ToolScheduler) executeSingleToolCall(ctx context.Context, toolCall *ToolCallInfo) {
	logger.Infof("Executing tool: %s (ID: %s)", toolCall.Name, toolCall.ID)

	ts.mutex.RLock()
	current, ok := ts.toolCalls[toolCall.ID]
	status := StatusCancelled
	if ok {
		status = current.Status
	}
	ts.mutex.RUnlock()
	if !ok || status != StatusScheduled {
		return
	}

	// Set status to executing
	ts.setStatus(toolCall.ID, StatusExecuting, nil, nil)

	callCtx, cancel := context.WithCancel(ctx)

	// Inject tool call ID into context for sub-agent WorkerID derivation
	callCtx = WithToolCallID(callCtx, toolCall.ID)

	ts.mutex.Lock()
	if call, ok := ts.toolCalls[toolCall.ID]; ok {
		call.cancel = cancel
	}
	// Inject pre-computed security decision into the execution context so
	// downstream layers (SecurityMiddleware, ShellTool.Execute) can skip
	// redundant re-analysis.
	if d, ok := ts.securityDecisions[toolCall.ID]; ok {
		callCtx = middleware.WithSecurityDecision(callCtx, d)
		delete(ts.securityDecisions, toolCall.ID)
	}

	// Inject output streaming callback if outputHandler is configured
	if ts.outputHandler != nil {
		cb := func(stream, chunk string) {
			// Update live output in tool call info
			ts.mutex.Lock()
			if c, ok := ts.toolCalls[toolCall.ID]; ok {
				c.LiveOutput += chunk
				// Keep live output under 64KB to avoid memory issues
				if len(c.LiveOutput) > 64*1024 {
					c.LiveOutput = c.LiveOutput[len(c.LiveOutput)-64*1024:]
				}
			}
			ts.mutex.Unlock()

			// Forward to output handler
			ts.outputHandler(toolCall.ID, chunk)

			// Emit streaming event
			if ts.eventHandler != nil {
				ev := event.NewStreamEvent(event.EventTypeWorkerUpdate, "tool_scheduler")
				ev.WorkerID = toolCall.ID
				ev = ev.WithContent(chunk).WithMetadata("stream", stream)
				ts.eventHandler(ev)
			}
		}
		callCtx = system.WithOutputCallback(callCtx, cb)
	}
	if ts.eventHandler != nil {
		callCtx = sandbox.WithEventPublisher(callCtx, sandboxEventPublisher{handler: ts.eventHandler})
	}

	ts.mutex.Unlock()
	defer func() {
		cancel()
		ts.mutex.Lock()
		if call, ok := ts.toolCalls[toolCall.ID]; ok {
			call.cancel = nil
		}
		ts.mutex.Unlock()
	}()

	// Emit tool start event
	if ts.eventHandler != nil {
		ts.eventHandler(event.StreamEvent{
			Type: event.EventTypeToolCall,
			ToolCalls: []*tools.ToolCall{{
				ID:        toolCall.ID,
				Name:      toolCall.Name,
				Arguments: toolCall.Parameters,
			}},
		})
	}

	// Execute with recovery strategy
	toolToExecute := ToolToExecute{
		ID:         toolCall.ID,
		Name:       toolCall.Name,
		Parameters: toolCall.Parameters,
	}

	// Add tool use message to TUI
	if ts.eventHandler != nil {
		ts.eventHandler(event.StreamEvent{
			Type:   event.EventTypeToolUse,
			Source: "agent_turn",
			ToolUse: &event.ToolUse{
				ID:         toolCall.ID,
				ToolName:   toolCall.Name,
				Parameters: toolCall.Parameters,
				Status:     string(StatusExecuting),
			},
		})
	}

	execResult := ts.recovery.ExecuteWithRecovery(callCtx, ts.toolbox, toolToExecute)

	// Dispatch PostToolUse or PostToolUseFailure hook
	ts.mutex.RLock()
	hookEngine := ts.hookEngine
	ts.mutex.RUnlock()
	if hookEngine != nil {
		hookEvent := middleware.HookPostToolUse
		if execResult.Error != nil {
			hookEvent = middleware.HookPostToolUseFailure
		}
		hookParams := make(map[string]interface{})
		for k, v := range toolCall.Parameters {
			hookParams[k] = v
		}
		if execResult.Result != nil {
			hookParams["_result"] = execResult.Result
		}
		if execResult.Error != nil {
			hookParams["_error"] = execResult.Error.Error()
		}
		_, err := hookEngine.Execute(callCtx, hookEvent, toolCall.Name, hookParams)
		if err != nil {
			logger.Warnf("PostToolUse hook execution error for tool %s: %v", toolCall.Name, err)
		}
	}

	if execResult.Error != nil {
		logger.Errorf("Tool %s failed after recovery: %v", toolCall.Name, execResult.Error)
		// Build a normalized error ToolResult so both UI and LLM get consistent content
		norm := ts.normalizeToolResultFields(toolCall.Name, nil, execResult)
		ts.setStatus(toolCall.ID, StatusError, norm, execResult.Error)

		// Emit tool result event for failure (LLM-oriented content)
		if ts.eventHandler != nil {
			ts.eventHandler(event.StreamEvent{
				Type: event.EventTypeToolResult,
				ToolResult: &tools.ToolResult{
					ID:      toolCall.ID,
					Content: norm.LLMContent,
					Error:   norm.Error,
				},
			})
		}

		// Update tool use message in TUI with error status (user-oriented content)
		if ts.eventHandler != nil {
			ts.eventHandler(event.StreamEvent{
				Type:   event.EventTypeToolUse,
				Source: "agent_turn",
				ToolUse: &event.ToolUse{
					ID:       toolCall.ID,
					ToolName: toolCall.Name,
					Status:   string(StatusError),
					Result:   norm.UserContent,
				},
			})
		}
		return
	}

	// Success
	norm := ts.normalizeToolResultFields(toolCall.Name, execResult.Result, execResult)
	ts.setStatus(toolCall.ID, StatusSuccess, norm, nil)

	// Emit tool result event for success (LLM-oriented content)
	if ts.eventHandler != nil {
		ts.eventHandler(event.StreamEvent{
			Type: event.EventTypeToolResult,
			ToolResult: &tools.ToolResult{
				ID:      toolCall.ID,
				Content: norm.LLMContent,
			},
		})
	}

	if ts.eventHandler != nil {
		ts.eventHandler(event.StreamEvent{
			Type:   event.EventTypeToolUse,
			Source: "agent_turn",
			ToolUse: &event.ToolUse{
				ID:       toolCall.ID,
				ToolName: toolCall.Name,
				Status:   string(StatusSuccess),
				Result:   norm.UserContent,
			},
		})
	}
}

type sandboxEventPublisher struct {
	handler func(event.StreamEvent)
}

func (p sandboxEventPublisher) PublishSandboxEvent(ev sandbox.Event) {
	if p.handler == nil {
		return
	}
	p.handler(event.StreamEvent{
		Type:      event.EventType(ev.Type),
		Content:   ev.Content,
		Source:    ev.Source,
		Timestamp: ev.Timestamp,
		Metadata:  ev.Metadata,
	})
}

// GetToolCallStatus returns the current status of a tool call
func (ts *ToolScheduler) GetToolCallStatus(callID string) (*ToolCallInfo, bool) {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()
	call, exists := ts.toolCalls[callID]
	return call, exists
}

// GetAllToolCalls returns all current tool calls
func (ts *ToolScheduler) GetAllToolCalls() []*ToolCallInfo {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()
	calls := make([]*ToolCallInfo, 0, len(ts.toolCalls))
	for _, call := range ts.toolCalls {
		calls = append(calls, call)
	}
	return calls
}

// CancelToolCall cancels a specific tool call
func (ts *ToolScheduler) CancelToolCall(callID string, reason string) error {
	ts.mutex.Lock()
	call, exists := ts.toolCalls[callID]
	if !exists {
		ts.mutex.Unlock()
		return fmt.Errorf("tool call %s not found", callID)
	}
	if call.Status == StatusSuccess || call.Status == StatusError || call.Status == StatusCancelled {
		ts.mutex.Unlock()
		return fmt.Errorf("cannot cancel tool call %s: already in terminal state %s", callID, call.Status)
	}
	cancel := call.cancel
	ts.mutex.Unlock()

	if cancel != nil {
		cancel()
	}

	// Build a structured cancellation result so upstream can construct a tool message
	tr := &interfaces.ToolResult{
		Success:     false,
		Error:       "cancelled by user",
		Metadata:    map[string]interface{}{"status": "cancelled", "reason": reason},
		LLMContent:  "Tool execution was cancelled by user.",
		UserContent: "操作已被用户取消。",
	}

	// Normalize for consistency
	tr = ts.normalizeToolResultFields(call.Name, tr, nil)

	// Emit tool result event so UI updates immediately
	if ts.eventHandler != nil {
		ts.eventHandler(event.StreamEvent{
			Type: event.EventTypeToolResult,
			ToolResult: &tools.ToolResult{
				ID:      callID,
				Content: tr.LLMContent,
				Error:   tr.Error,
			},
		})
		// Update tool use panel with cancelled status
		ts.eventHandler(event.StreamEvent{
			Type:   event.EventTypeToolUse,
			Source: "agent_turn",
			ToolUse: &event.ToolUse{
				ID:         callID,
				ToolName:   call.Name,
				Parameters: call.Parameters,
				Status:     string(StatusCancelled),
				Result:     tr.UserContent,
			},
		})
	}

	ts.setStatus(callID, StatusCancelled, tr, fmt.Errorf("cancelled: %s", reason))
	return nil
}

// UpdateLiveOutput updates the live output for an executing tool call
func (ts *ToolScheduler) UpdateLiveOutput(callID string, output string) {
	ts.mutex.Lock()

	call, exists := ts.toolCalls[callID]
	if !exists || call.Status != StatusExecuting {
		ts.mutex.Unlock()
		return
	}

	call.LiveOutput = output

	// Capture callbacks and data before releasing lock
	outputHandler := ts.outputHandler
	onUpdate := ts.onUpdate

	var updateCalls []*ToolCallInfo
	if onUpdate != nil {
		updateCalls = make([]*ToolCallInfo, 0, len(ts.toolCalls))
		for _, call := range ts.toolCalls {
			// Create a shallow copy to avoid data races
			callCopy := *call
			// Deep copy pointer fields to avoid data race
			if call.Result != nil {
				resultCopy := *call.Result
				callCopy.Result = &resultCopy
			}
			updateCalls = append(updateCalls, &callCopy)
		}
	}

	ts.mutex.Unlock()

	// Call callbacks outside of lock
	if outputHandler != nil {
		outputHandler(callID, output)
	}
	if onUpdate != nil && updateCalls != nil {
		onUpdate(updateCalls)
	}
}

// HandleConfirmationResponse handles user confirmation responses for tool calls
func (ts *ToolScheduler) HandleConfirmationResponse(callID string, approved bool) error {
	ts.mutex.RLock()
	call, exists := ts.toolCalls[callID]
	ts.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("tool call %s not found", callID)
	}

	if call.Status != StatusAwaitingApproval {
		return fmt.Errorf("tool call %s is not awaiting approval (current status: %s)", callID, call.Status)
	}

	if !approved {
		// User cancelled the tool call: attach a cancellation result and emit events
		tr := &interfaces.ToolResult{
			Success:     false,
			Error:       "cancelled by user",
			Metadata:    map[string]interface{}{"status": "cancelled", "reason": "user_declined"},
			LLMContent:  "Tool execution was cancelled by user.",
			UserContent: "操作已被用户取消。",
		}

		// Normalize for consistency
		tr = ts.normalizeToolResultFields(call.Name, tr, nil)

		if ts.eventHandler != nil {
			// Emit tool result event so downstream UI logs it
			ts.eventHandler(event.StreamEvent{
				Type: event.EventTypeToolResult,
				ToolResult: &tools.ToolResult{
					ID:      callID,
					Content: tr.LLMContent,
					Error:   tr.Error,
				},
			})
			// Reflect cancellation in ToolUse panel
			ts.eventHandler(event.StreamEvent{
				Type:   event.EventTypeToolUse,
				Source: "agent_turn",
				ToolUse: &event.ToolUse{
					ID:         callID,
					ToolName:   call.Name,
					Parameters: call.Parameters,
					Status:     string(StatusCancelled),
					Result:     tr.UserContent,
				},
			})
		}

		// Transition to terminal state with result set so ExecuteParallel can collect it
		ts.setStatus(callID, StatusCancelled, tr, fmt.Errorf("cancelled by user"))
		return nil
	}

	// Schedule the tool call for execution
	ts.setStatus(callID, StatusScheduled, nil, nil)

	// Attempt to execute scheduled calls
	go ts.executeScheduledCalls(context.Background())

	return nil
}

// IsRunning returns whether any tool calls are currently running
func (ts *ToolScheduler) IsRunning() bool {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()
	for _, call := range ts.toolCalls {
		if call.Status == StatusExecuting || call.Status == StatusAwaitingApproval || call.Status == StatusScheduled {
			return true
		}
	}
	return false
}

// WaitForCompletion waits until all tool calls are in a terminal state
func (ts *ToolScheduler) WaitForCompletion(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ts.mutex.RLock()
			allComplete := true
			for _, call := range ts.toolCalls {
				if call.Status != StatusSuccess && call.Status != StatusError && call.Status != StatusCancelled {
					allComplete = false
					break
				}
			}
			ts.mutex.RUnlock()
			if allComplete {
				return nil
			}
		}
	}
}

// GetCompletedToolCalls returns a list of completed tool calls (success, error, or cancelled)
func (ts *ToolScheduler) GetCompletedToolCalls() []*ToolCallInfo {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()
	completed := make([]*ToolCallInfo, 0)
	for _, call := range ts.toolCalls {
		if call.Status == StatusSuccess || call.Status == StatusError || call.Status == StatusCancelled {
			completed = append(completed, call)
		}
	}
	return completed
}

// ClearCompletedToolCalls removes completed tool calls from the scheduler
func (ts *ToolScheduler) ClearCompletedToolCalls() {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	for id, call := range ts.toolCalls {
		if call.Status == StatusSuccess || call.Status == StatusError || call.Status == StatusCancelled {
			delete(ts.toolCalls, id)
		}
	}
}

// ClearSpecificToolCalls removes specific tool calls from the scheduler
func (ts *ToolScheduler) ClearSpecificToolCalls(toolIDs []string) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	for _, id := range toolIDs {
		if call, exists := ts.toolCalls[id]; exists {
			if call.Status == StatusSuccess || call.Status == StatusError || call.Status == StatusCancelled {
				delete(ts.toolCalls, id)
				delete(ts.securityDecisions, id)
				logger.Infof("Cleared completed tool call %s", id)
			}
		}
	}
}

package agent

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/middleware"
)

// ToolPolicyEngine centralizes scheduler policy decisions that do not execute
// tools directly. It is intentionally a thin compatibility layer over existing
// ToolScheduler state so call sites can be migrated incrementally.
type ToolPolicyEngine struct {
	scheduler *ToolScheduler
}

type ToolSecurityAnalysis struct {
	Supported bool
	Decision  *middleware.Decision
	Err       error
}

type ToolPreflightResult struct {
	HasAllowPolicy   bool
	Allowed          bool
	RequiresApproval bool
	// ExplicitlyAllowed is true when the permission manager's auto fast-path
	// (mode=auto + shell fast-path or classifier) already allowed this tool.
	// It prevents a lower-confidence Security Analyzer ActionConfirm decision
	// from reinstating RequiresApproval and blocking the tool in headless mode.
	ExplicitlyAllowed bool
	SecurityAnalysis  ToolSecurityAnalysis
}

func (ts *ToolScheduler) policyEngine() ToolPolicyEngine {
	return ToolPolicyEngine{scheduler: ts}
}

// PreflightTool evaluates all policy checks that should run before a tool is
// scheduled for execution. It keeps the scheduler's decision order intact:
// allowlist first, approval policy second, optional security analysis last.
func (pe ToolPolicyEngine) PreflightTool(ctx context.Context, toolName string, params map[string]interface{}, tool interfaces.Tool) ToolPreflightResult {
	hasPolicy, allowed := pe.AllowedByCurrentPolicy(toolName)
	result := ToolPreflightResult{
		HasAllowPolicy: hasPolicy,
		Allowed:        allowed,
	}
	if hasPolicy && !allowed {
		return result
	}

	result.RequiresApproval = pe.RequiresApprovalForTool(toolName, params, tool)
	// Mark as explicitly allowed when auto mode fast-path already approved the
	// tool so that a lower-confidence Security Analyzer ActionConfirm decision
	// cannot override the fast-path allow in headless mode.
	if !result.RequiresApproval {
		ts := pe.scheduler
		ts.mutex.RLock()
		pm := ts.permissionManager
		ts.mutex.RUnlock()
		if pm != nil && pm.GetMode() == permission.ModeAuto {
			result.ExplicitlyAllowed = true
		}
	}
	result.SecurityAnalysis = pe.AnalyzeSecurity(ctx, toolName, params, tool)
	return result
}

// AllowedByCurrentPolicy evaluates the scheduler's per-turn tool allowlist.
// The first return value indicates whether an allowlist policy is active.
func (pe ToolPolicyEngine) AllowedByCurrentPolicy(toolName string) (bool, bool) {
	ts := pe.scheduler
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()

	hasPolicy := ts.allowedExact != nil || len(ts.allowedPatterns) > 0
	if !hasPolicy {
		return false, true
	}

	if _, allowed := ts.allowedExact[toolName]; allowed {
		return true, true
	}
	for _, pattern := range ts.allowedPatterns {
		if ok, _ := filepath.Match(pattern, toolName); ok {
			return true, true
		}
	}
	return true, false
}

// RequiresApprovalForTool centralizes the static/contextual permission check
// before security pre-analysis and daemon confirmation policy are applied.
func (pe ToolPolicyEngine) RequiresApprovalForTool(toolName string, params map[string]interface{}, tool interfaces.Tool) bool {
	ts := pe.scheduler
	ts.mutex.RLock()
	pm := ts.permissionManager
	ts.mutex.RUnlock()

	if pm != nil {
		return pm.ShouldConfirm(toolName, params, tool)
	}
	if contextualTool, ok := tool.(interfaces.ContextualConfirmationTool); ok {
		return contextualTool.RequiresConfirmationForParams(params)
	}
	return tool.RequiresConfirmation()
}

// AnalyzeSecurity runs optional tool-provided security analysis and validates
// that the returned action is one of the known middleware actions.
func (pe ToolPolicyEngine) AnalyzeSecurity(ctx context.Context, toolName string, params map[string]interface{}, tool interfaces.Tool) ToolSecurityAnalysis {
	secTool, ok := tool.(interfaces.SecurityAnalyzableTool)
	if !ok {
		return ToolSecurityAnalysis{}
	}

	decision, err := secTool.AnalyzeSecurityDecision(ctx, params)
	if err != nil {
		return ToolSecurityAnalysis{Supported: true, Err: err}
	}
	if decision == nil {
		return ToolSecurityAnalysis{Supported: true, Err: fmt.Errorf("nil security decision returned by tool %s", toolName)}
	}
	switch decision.Action {
	case middleware.ActionAllow, middleware.ActionConfirm, middleware.ActionBlock:
		return ToolSecurityAnalysis{Supported: true, Decision: decision}
	default:
		return ToolSecurityAnalysis{
			Supported: true,
			Err:       fmt.Errorf("invalid security action %d returned by tool %s", decision.Action, toolName),
		}
	}
}

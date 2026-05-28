package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/team"
	"github.com/oklog/ulid/v2"
)

// AgentTool is the unified subagent tool that replaces spawn_teammate, main_agent, etc.
type AgentTool struct {
	cfg      *config.Config
	resolver *agentprofile.Resolver
}

// NewAgentTool creates a new unified Agent tool.
func NewAgentTool(cfg *config.Config, resolver *agentprofile.Resolver) *AgentTool {
	return &AgentTool{
		cfg:      cfg,
		resolver: resolver,
	}
}

func (t *AgentTool) Name() string {
	return "Agent"
}

func (t *AgentTool) Description() string {
	return "Spawn a subagent to perform a focused task. Returns its final result synchronously, or a task handle if run_in_background=true."
}

func (t *AgentTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryAgent
}

func (t *AgentTool) RequiresConfirmation() bool {
	return false
}

func (t *AgentTool) ConcurrencySafe() bool {
	return false
}

func (t *AgentTool) Schema() *interfaces.ToolSchema {
	descProp := interfaces.NewStringProperty("Short 3-5 word task label.")
	promptProp := interfaces.NewStringProperty("Full task instructions for the subagent. Must be self-contained with all context needed.")
	typeProp := interfaces.NewStringProperty("Built-in / plugin / user-defined agent type. Available: general-purpose, explore, plan, verify, or custom.")
	modelProp := interfaces.NewStringProperty("Override model; omit to inherit parent model.")
	isolationProp := interfaces.NewStringPropertyWithEnum("Isolation mode for the subagent.", []string{"none", "worktree"})
	backgroundProp := interfaces.NewBooleanProperty("If true, returns immediately with a task handle instead of blocking.")
	forkProp := interfaces.NewStringProperty("Internal: parent agent id to fork from (prompt-cache continuation).")
	resumeProp := interfaces.NewStringProperty("Internal: agent id to resume from persisted transcript.")

	return interfaces.CreateSchema(
		t.Description(),
		map[string]*interfaces.PropertySchema{
			"description":       descProp,
			"prompt":            promptProp,
			"subagent_type":     typeProp,
			"model":             modelProp,
			"isolation":         isolationProp,
			"run_in_background": backgroundProp,
			"fork_from":         forkProp,
			"resume_from":       resumeProp,
		},
		[]string{"description", "prompt", "subagent_type"},
	)
}

func (t *AgentTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Extract parameters
	description, _ := params["description"].(string)
	prompt, _ := params["prompt"].(string)
	subagentType, _ := params["subagent_type"].(string)
	model, _ := params["model"].(string)
	isolation, _ := params["isolation"].(string)
	runInBackground, _ := params["run_in_background"].(bool)
	forkFrom, _ := params["fork_from"].(string)
	resumeFrom, _ := params["resume_from"].(string)

	if prompt == "" {
		return &interfaces.ToolResult{
			Success:    false,
			Error:      "prompt parameter is required",
			LLMContent: "Agent tool failed: prompt is required and must contain full task context.",
		}, nil
	}
	if subagentType == "" {
		subagentType = "general-purpose"
	}

	// Resolve profile
	profile, found := t.resolver.Resolve(subagentType)
	if !found {
		return &interfaces.ToolResult{
			Success:    false,
			Error:      fmt.Sprintf("unknown subagent_type %q", subagentType),
			LLMContent: fmt.Sprintf("Agent tool failed: unknown subagent_type %q. Available types: general-purpose, explore, plan, verify.", subagentType),
		}, nil
	}

	// Determine if this should run async
	isAsync := runInBackground || profile.Background

	// Generate agent ID
	agentID := fmt.Sprintf("ag_%s", ulid.Make().String())

	// Handle model override
	effectiveModel := model
	if effectiveModel == "" {
		effectiveModel = profile.Model
	}

	// Handle isolation (worktree)
	var worktreeHandle *swarm.WorktreeHandle
	if isolation == "worktree" || profile.Isolation == "worktree" {
		cwd := t.cfg.WorkingDir
		if cwd == "" {
			cwd = "."
		}
		handle, err := swarm.CreateAgentWorktree(cwd, agentID)
		if err != nil {
			logger.Warnf("Failed to create worktree for agent %s: %v", agentID, err)
		} else {
			worktreeHandle = handle
		}
	}

	// Route to appropriate execution path
	if forkFrom != "" {
		return t.executeFork(ctx, agentID, forkFrom, prompt, profile, isAsync)
	}
	if resumeFrom != "" {
		return t.executeResume(ctx, agentID, resumeFrom, prompt, profile, isAsync)
	}
	if isAsync {
		return t.executeAsync(ctx, agentID, description, prompt, profile, effectiveModel, worktreeHandle)
	}
	return t.executeSync(ctx, agentID, description, prompt, profile, effectiveModel, worktreeHandle)
}

func (t *AgentTool) executeSync(ctx context.Context, agentID, description, prompt string, profile agentprofile.AgentProfile, model string, worktree *swarm.WorktreeHandle) (*interfaces.ToolResult, error) {
	logger.Infof("Agent[sync] %s: starting type=%s desc=%q", agentID, profile.Name, description)

	// Build identity for sync execution
	identity := &swarm.TeammateIdentity{
		AgentID:   agentID,
		AgentName: description,
		TeamName:  "sync",
		Color:     DefaultColorManager.ColorFor(agentID),
		Model:     model,
	}

	// Compute timeout from profile
	timeout := 5 * time.Minute
	if profile.MaxTurns > 0 {
		// Rough estimate: 30s per turn
		timeout = time.Duration(profile.MaxTurns) * 30 * time.Second
	}

	workDir := ""
	if worktree != nil {
		workDir = worktree.Path
	}

	result, err := swarm.RunSync(ctx, swarm.SyncRunOptions{
		Identity:     identity,
		Prompt:       prompt,
		Timeout:      timeout,
		MaxTurns:     profile.MaxTurns,
		WorktreePath: workDir,
	})

	// Cleanup worktree after sync completion
	if worktree != nil {
		if cleanErr := swarm.CleanupAgentWorktree(worktree); cleanErr != nil {
			logger.Warnf("Agent %s: worktree cleanup: %v", agentID, cleanErr)
		}
	}

	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   err.Error(),
			Metadata: map[string]interface{}{
				"status":   "failed",
				"agent_id": agentID,
			},
			LLMContent: fmt.Sprintf("Agent execution failed: %v", err),
		}, nil
	}

	content := "Task completed."
	if result != nil && result.Content != "" {
		content = result.Content
	}

	return &interfaces.ToolResult{
		Success: true,
		Data:    content,
		Metadata: map[string]interface{}{
			"status":   "completed",
			"agent_id": agentID,
		},
		LLMContent: content,
	}, nil
}

func (t *AgentTool) executeAsync(ctx context.Context, agentID, description, prompt string, profile agentprofile.AgentProfile, model string, worktree *swarm.WorktreeHandle) (*interfaces.ToolResult, error) {
	logger.Infof("Agent[async] %s: launching type=%s desc=%q", agentID, profile.Name, description)

	// Determine team name
	teamName := "default"

	// Build spawn options
	opts := swarm.SpawnOptions{
		TeamName:       teamName,
		Name:           strings.ReplaceAll(description, " ", "-"),
		Color:          DefaultColorManager.ColorFor(agentID),
		InitialPrompt:  prompt,
		PermissionMode: "auto",
		Model:          model,
		Sandbox: team.SandboxPolicy{
			Lifecycle: "task",
			Scope:     "subagent",
		},
	}

	if len(profile.Tools) > 0 && !containsStar(profile.Tools) {
		opts.AllowedTools = profile.Tools
	}

	// Spawn in-process async
	handle, err := swarm.SpawnInProcess(ctx, opts)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to spawn async agent: %v", err),
			Metadata: map[string]interface{}{
				"status":   "failed",
				"agent_id": agentID,
			},
			LLMContent: fmt.Sprintf("Failed to spawn async agent: %v", err),
		}, nil
	}

	outputFile := ""
	outputDir := "/tmp/nano-agent-output"
	outputFile = swarm.TranscriptPath(outputDir, handle.AgentID)

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"agent_id":    handle.AgentID,
			"session_id":  handle.SessionID,
			"output_file": outputFile,
		},
		Metadata: map[string]interface{}{
			"status":      "async_launched",
			"agent_id":    handle.AgentID,
			"output_file": outputFile,
		},
		LLMContent: fmt.Sprintf("Async agent launched: %s (session: %s). Use TaskOutput to check results.", handle.AgentID, handle.SessionID),
	}, nil
}

func (t *AgentTool) executeFork(ctx context.Context, agentID, forkFrom, prompt string, profile agentprofile.AgentProfile, isAsync bool) (*interfaces.ToolResult, error) {
	logger.Infof("Agent[fork] %s: forking from %s", agentID, forkFrom)

	// For now, fork is implemented as a regular execution with a note about the parent
	// Full implementation would load parent's system prompt from cache
	forkPrompt := fmt.Sprintf("[Forked from %s]\n\n%s", forkFrom, prompt)

	if isAsync {
		return t.executeAsync(ctx, agentID, "fork-"+forkFrom, forkPrompt, profile, profile.Model, nil)
	}
	return t.executeSync(ctx, agentID, "fork-"+forkFrom, forkPrompt, profile, profile.Model, nil)
}

func (t *AgentTool) executeResume(ctx context.Context, agentID, resumeFrom, prompt string, profile agentprofile.AgentProfile, isAsync bool) (*interfaces.ToolResult, error) {
	logger.Infof("Agent[resume] %s: resuming from %s", agentID, resumeFrom)

	outputDir := "/tmp/nano-agent-output"

	resumeResult, err := swarm.ResumeAgent(swarm.ResumeOptions{
		AgentID:   resumeFrom,
		OutputDir: outputDir,
	})
	if err != nil {
		return &interfaces.ToolResult{
			Success:    false,
			Error:      fmt.Sprintf("failed to resume agent %s: %v", resumeFrom, err),
			LLMContent: fmt.Sprintf("Failed to resume agent: %v", err),
			Metadata: map[string]interface{}{
				"status":   "failed",
				"agent_id": agentID,
			},
		}, nil
	}

	// Build resume context into prompt
	resumeContext := swarm.BuildResumePrompt(resumeResult)
	fullPrompt := resumeContext + "\n\n" + prompt

	if isAsync {
		return t.executeAsync(ctx, agentID, "resume-"+resumeFrom, fullPrompt, profile, profile.Model, nil)
	}
	return t.executeSync(ctx, agentID, "resume-"+resumeFrom, fullPrompt, profile, profile.Model, nil)
}

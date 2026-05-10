package teammate

import (
	"context"
	"fmt"
	"os"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/team"
)

// SpawnTool implements the spawn_teammate tool
type SpawnTool struct {
	cfg *config.Config
}

// NewSpawnTool creates a new spawn_teammate tool
func NewSpawnTool(cfg *config.Config) *SpawnTool {
	return &SpawnTool{cfg: cfg}
}

// Name returns the tool name
func (t *SpawnTool) Name() string {
	return "spawn_teammate"
}

// Description returns the tool description
func (t *SpawnTool) Description() string {
	return "Spawn a new teammate agent to work on a subtask (team-lead only); initial_prompt is optional when a matching .nano/agents profile provides one"
}

// Category returns the tool category
func (t *SpawnTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryAgent
}

// RequiresConfirmation returns false
func (t *SpawnTool) RequiresConfirmation() bool {
	return false
}

// ConcurrencySafe returns false (spawning modifies team state)
func (t *SpawnTool) ConcurrencySafe() bool {
	return false
}

// Schema returns the JSON schema for tool parameters
func (t *SpawnTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Spawn a new teammate agent to work on a subtask; initial_prompt is optional when supplied by a matching .nano/agents profile",
		map[string]*interfaces.PropertySchema{
			"name": {
				Type:        "string",
				Description: "Short name for the teammate (e.g., 'researcher', 'coder')",
			},
			"kind": {
				Type:        "string",
				Description: "Execution mode: 'in_process' (goroutine) or 'subprocess' (tmux/iTerm2 pane)",
				Enum:        []string{"in_process", "subprocess"},
			},
			"initial_prompt": {
				Type:        "string",
				Description: "Initial task/prompt for the teammate; optional when a matching .nano/agents profile provides initial_prompt",
			},
			"color": {
				Type:        "string",
				Description: "Optional UI color (hex code, e.g., '#FF5733')",
			},
			"permission_mode": {
				Type:        "string",
				Description: "Permission mode: 'auto'/'ask' or nano modes 'default', 'acceptEdits', 'yolo'",
				Enum:        []string{"auto", "ask", "default", "acceptEdits", "yolo"},
			},
			"model": {
				Type:        "string",
				Description: "Optional model override for this teammate (defaults to matching .nano/agents profile or parent config)",
			},
			"fallbacks":         interfaces.NewArrayProperty("Optional provider/model fallback chain for this teammate (omitted inherits parent config or matching .nano/agents profile)", "string"),
			"context_providers": interfaces.NewArrayProperty("Optional context provider allowlist for this teammate (memory, skills, openspec; omitted inherits parent config)", "string"),
		},
		[]string{"name"},
	)
}

// Execute runs the tool with the provided parameters
func (t *SpawnTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// Check if caller is a teammate (teammates cannot spawn)
	if swarm.IsTeammate(ctx) {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "Teammates cannot spawn other teammates (team-lead only)",
			UserContent: "Teammates cannot spawn other teammates (team-lead only)",
		}, nil
	}

	// Parse parameters
	name, _ := params["name"].(string)
	kind, _ := params["kind"].(string)
	initialPrompt, _ := params["initial_prompt"].(string)
	color, _ := params["color"].(string)
	permissionMode, _ := params["permission_mode"].(string)
	model, _ := params["model"].(string)
	fallbackValue, fallbackProvided := params["fallbacks"]
	fallbacks := stringSliceParam(fallbackValue)
	contextProviders := stringSliceParam(params["context_providers"])
	var allowedTools []string

	if profile, ok := t.findProfile(name); ok {
		if kind == "" {
			kind = profile.Kind
		}
		if initialPrompt == "" {
			initialPrompt = profile.InitialPrompt
		}
		if color == "" {
			color = profile.Color
		}
		if permissionMode == "" {
			permissionMode = profile.PermissionMode
		}
		if model == "" {
			model = profile.Model
		}
		if !fallbackProvided {
			fallbacks = append([]string(nil), profile.Fallbacks...)
		}
		if len(contextProviders) == 0 {
			contextProviders = append([]string(nil), profile.ContextProviders...)
		}
		allowedTools = append([]string(nil), profile.AllowedTools...)
	}

	// Validate input
	if name == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "'name' field is required",
			UserContent: "'name' field is required",
		}, nil
	}
	if initialPrompt == "" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  "'initial_prompt' field is required",
			UserContent: "'initial_prompt' field is required",
		}, nil
	}

	// Default values
	if kind == "" {
		kind = "in_process"
	}
	if permissionMode == "" {
		permissionMode = "ask"
	}

	// Validate kind
	if kind != "in_process" && kind != "subprocess" {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Invalid kind '%s' (must be 'in_process' or 'subprocess')", kind),
			UserContent: fmt.Sprintf("Invalid kind '%s' (must be 'in_process' or 'subprocess')", kind),
		}, nil
	}

	teamName := "default"
	if leadCtx, ok := swarm.TeamLeadFromContext(ctx); ok && leadCtx.TeamName != "" {
		teamName = leadCtx.TeamName
	}
	if result := t.enforceAgentLimits(teamName); result != nil {
		return result, nil
	}

	// Prepare spawn options
	opts := swarm.SpawnOptions{
		TeamName:         teamName,
		Name:             name,
		Color:            color,
		InitialPrompt:    initialPrompt,
		PermissionMode:   permissionMode,
		AllowedTools:     allowedTools,
		Model:            model,
		Fallbacks:        fallbacks,
		ContextProviders: contextProviders,
		MaxRuntimeSec:    t.maxRuntimeSec(),
		Runner:           swarm.NewDefaultRunner(t.cfg),
	}
	if t.cfg != nil && t.cfg.Sandbox != nil {
		opts.Sandbox.Backend = t.cfg.Sandbox.Backend
		if opts.Sandbox.Backend == "" && t.cfg.Sandbox.Enabled {
			opts.Sandbox.Backend = "native"
		}
		opts.Sandbox.Lifecycle = "task"
		opts.Sandbox.Scope = "subagent"
	}

	// Spawn the teammate
	var handle *swarm.SpawnHandle
	var err error

	switch kind {
	case "in_process":
		handle, err = swarm.SpawnInProcess(ctx, opts)
	case "subprocess":
		handle, err = swarm.SpawnSubprocess(ctx, opts)
	default:
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Unknown spawn kind: %s", kind),
			UserContent: fmt.Sprintf("Unknown spawn kind: %s", kind),
		}, nil
	}

	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to spawn teammate: %v", err),
			UserContent: fmt.Sprintf("Failed to spawn teammate: %v", err),
		}, nil
	}

	// Return success message
	result := fmt.Sprintf("Spawned teammate '%s' (session: %s, kind: %s)\nMessages from this teammate will arrive in your inbox.",
		name, handle.SessionID, kind)

	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  result,
		UserContent: result,
	}, nil
}

func (t *SpawnTool) findProfile(name string) (agentprofile.AgentProfile, bool) {
	cwd := ""
	if t.cfg != nil {
		cwd = t.cfg.WorkingDir
	}
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	return agentprofile.NewManager(cwd).Find(name)
}

func stringSliceParam(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (t *SpawnTool) maxRuntimeSec() int {
	if t.cfg == nil || t.cfg.Advanced == nil || t.cfg.Advanced.Fork == nil {
		return 0
	}
	if t.cfg.Advanced.Fork.MaxRuntimeSec <= 0 {
		return 0
	}
	return t.cfg.Advanced.Fork.MaxRuntimeSec
}

func (t *SpawnTool) enforceAgentLimits(teamName string) *interfaces.ToolResult {
	maxConcurrent := 0
	if t.cfg != nil && t.cfg.Advanced != nil && t.cfg.Advanced.Fork != nil {
		maxConcurrent = t.cfg.Advanced.Fork.MaxConcurrent
	}
	if maxConcurrent <= 0 {
		return nil
	}

	existingTeam, err := team.ReadTeam(teamName)
	if err != nil {
		message := fmt.Sprintf("Failed to evaluate max concurrent agents limit for team %q: %v", teamName, err)
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  message,
			UserContent: message,
		}
	}
	active := 0
	for _, member := range existingTeam.Members {
		if member.IsActive {
			active++
		}
	}
	if active < maxConcurrent {
		return nil
	}
	message := fmt.Sprintf("Max concurrent agents limit reached for team %q: active=%d limit=%d", teamName, active, maxConcurrent)
	return &interfaces.ToolResult{
		Success:     false,
		LLMContent:  message,
		UserContent: message,
	}
}

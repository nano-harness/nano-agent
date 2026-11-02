package memory //nolint:revive

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// LocalMemoryTool exposes the local memory system as an agent tool.
// It supports three actions: add, search, get.
type LocalMemoryTool struct {
	manager *Manager
}

// NewLocalMemoryTool creates a local memory tool backed by the given Manager.
func NewLocalMemoryTool(m *Manager) interfaces.Tool {
	return &LocalMemoryTool{manager: m}
}

// Name returns the tool identifier.
func (t *LocalMemoryTool) Name() string { return "memory" }

// Description returns a human-readable description.
func (t *LocalMemoryTool) Description() string {
	return "Manage local conversation memories - add messages to session memory, search past conversations, or list recent entries."
}

// Category returns the tool category.
func (t *LocalMemoryTool) Category() interfaces.ToolCategory { return interfaces.CategoryMemory }

// RequiresConfirmation returns false; memory operations are non-destructive reads/writes.
func (t *LocalMemoryTool) RequiresConfirmation() bool { return false }

// ConcurrencySafe returns false; SQLite writes are serialised inside the store but we
// conservatively mark the tool as non-concurrent to avoid redundant parallel writes.
func (t *LocalMemoryTool) ConcurrencySafe() bool { return false }

// Schema returns the JSON schema accepted by Execute.
func (t *LocalMemoryTool) Schema() *interfaces.ToolSchema {
	actionProp := interfaces.NewStringProperty("Action to perform: 'add' stores messages, 'search' queries memory, 'get' lists recent entries")
	actionProp.Enum = []string{"add", "search", "get"}

	contentProp := interfaces.NewStringProperty("Text content to store (required for 'add' action)")
	contentProp.Examples = []string{"User prefers concise answers", "Project uses Go 1.22"}

	queryProp := interfaces.NewStringProperty("Full-text search query (required for 'search' action)")
	queryProp.Examples = []string{"user preferences", "Go version"}

	roleProp := interfaces.NewStringPropertyWithEnum("Message role for 'add' action (default: user)", []string{"user", "assistant", "system"})

	limitProp := interfaces.NewNumberProperty("Maximum number of results to return (default: 10)")
	limitProp.Examples = []string{"5", "10", "20"}

	return interfaces.CreateSchema(
		"Local memory management backed by SQLite FTS5. Stores and retrieves conversation context across sessions.",
		map[string]*interfaces.PropertySchema{
			"action":  actionProp,
			"content": contentProp,
			"query":   queryProp,
			"role":    roleProp,
			"limit":   limitProp,
		},
		[]string{"action"},
	)
}

// Execute runs the requested memory action.
func (t *LocalMemoryTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	action, _ := params["action"].(string)
	switch action {
	case "add":
		return t.add(ctx, params)
	case "search":
		return t.search(ctx, params)
	case "get":
		return t.get(ctx, params)
	default:
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("unknown action %q; valid actions: add, search, get", action),
			LLMContent:  fmt.Sprintf("unknown memory action: %q", action),
			UserContent: fmt.Sprintf("unknown memory action: %q", action),
		}, nil
	}
}

// ─── action handlers ────────────────────────────────────────────────────────

func (t *LocalMemoryTool) add(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	content, _ := params["content"].(string)
	if content == "" {
		return errResult("content is required for 'add' action"), nil
	}
	role, _ := params["role"].(string)
	if role == "" {
		role = "user"
	}

	view := t.manager.ForSession(sessionIDFromContext(ctx))
	if err := view.Add(role, content); err != nil {
		return errResult(fmt.Sprintf("failed to add memory: %v", err)), nil
	}

	msg := "Memory saved successfully"
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  msg,
		UserContent: msg,
	}, nil
}

func (t *LocalMemoryTool) search(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return errResult("query is required for 'search' action"), nil
	}
	limit := intParam(params, "limit", 10)

	view := t.manager.ForSession(sessionIDFromContext(ctx))
	entries, err := view.Search(query, limit)
	if err != nil {
		return errResult(fmt.Sprintf("search failed: %v", err)), nil
	}

	if len(entries) == 0 {
		return &interfaces.ToolResult{
			Success:     true,
			LLMContent:  "No memories found matching the query.",
			UserContent: "No memories found matching the query.",
		}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d memories:\n\n", len(entries))
	for i, e := range entries {
		fmt.Fprintf(&sb, "%d. [%s] %s: %s\n", i+1, e.CreatedAt.Format("2006-01-02 15:04"), e.Role, e.Content)
	}
	out := sb.String()
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  out,
		UserContent: out,
	}, nil
}

func (t *LocalMemoryTool) get(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	limit := intParam(params, "limit", 20)

	view := t.manager.ForSession(sessionIDFromContext(ctx))
	entries, err := view.Recent(limit)
	if err != nil {
		return errResult(fmt.Sprintf("failed to retrieve memories: %v", err)), nil
	}

	if len(entries) == 0 {
		return &interfaces.ToolResult{
			Success:     true,
			LLMContent:  "No memories found for this session.",
			UserContent: "No memories found for this session.",
		}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Recent %d memories:\n\n", len(entries))
	for i, e := range entries {
		fmt.Fprintf(&sb, "%d. [%s] %s: %s\n", i+1, e.CreatedAt.Format("2006-01-02 15:04"), e.Role, e.Content)
	}
	out := sb.String()
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  out,
		UserContent: out,
	}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func errResult(msg string) *interfaces.ToolResult {
	return &interfaces.ToolResult{
		Success:     false,
		Error:       msg,
		LLMContent:  msg,
		UserContent: msg,
	}
}

func intParam(params map[string]interface{}, key string, defaultVal int) int {
	if v, ok := params[key].(float64); ok && v > 0 {
		return int(v)
	}
	if v, ok := params[key].(int); ok && v > 0 {
		return v
	}
	return defaultVal
}

package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// ToolDetailProvider is a function that returns the full schema JSON for a tool name.
// This decouples the builtin package from the agent package.
type ToolDetailProvider func(toolName string) (string, bool)

// DiscoverToolsTool provides on-demand lookup of tool schemas.
// This implements Layer 2 of the Progressive Disclosure pattern.
type DiscoverToolsTool struct {
	// toolIndex maps tool name → abbreviated ToolSummary fields.
	// The full provider is used for detail queries.
	toolIndex map[string]toolEntry
	detailFn  ToolDetailProvider
}

type toolEntry struct {
	Name        string
	Description string
	Category    string
	Server      string
}

// NewDiscoverToolsTool creates a DiscoverToolsTool.
// detailFn may be nil, in which case full-schema queries return a notice.
func NewDiscoverToolsTool(detailFn ToolDetailProvider) *DiscoverToolsTool {
	return &DiscoverToolsTool{
		toolIndex: make(map[string]toolEntry),
		detailFn:  detailFn,
	}
}

// IndexTool registers a tool for discovery.
func (t *DiscoverToolsTool) IndexTool(name, description, category, server string) {
	t.toolIndex[name] = toolEntry{
		Name:        name,
		Description: description,
		Category:    category,
		Server:      server,
	}
}

// Name returns the tool name.
func (t *DiscoverToolsTool) Name() string { return "discover_tools" }

// Description returns the tool description.
func (t *DiscoverToolsTool) Description() string {
	return "Search or get full schema details for available tools. Use this to find the right tool for a task before using it."
}

// Category returns the tool category.
func (t *DiscoverToolsTool) Category() interfaces.ToolCategory { return interfaces.CategoryAgent }

// RequiresConfirmation returns false.
func (t *DiscoverToolsTool) RequiresConfirmation() bool { return false }

// ConcurrencySafe returns true.
func (t *DiscoverToolsTool) ConcurrencySafe() bool { return true }

// Schema returns the JSON schema.
func (t *DiscoverToolsTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Search or get full schema details for available tools",
		map[string]*interfaces.PropertySchema{
			"query": {
				Type:        "string",
				Description: "Search query (tool name, category, or keyword). Leave empty to list all tools.",
			},
			"name": {
				Type:        "string",
				Description: "Exact tool name to get full schema details for.",
			},
		},
		[]string{},
	)
}

// Execute runs the discover action.
func (t *DiscoverToolsTool) Execute(_ context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	// Get full schema for a specific tool
	if name, ok := args["name"].(string); ok && name != "" {
		return t.getToolDetail(name)
	}

	// Search / list
	query, _ := args["query"].(string)
	return t.searchTools(query)
}

func (t *DiscoverToolsTool) searchTools(query string) (*interfaces.ToolResult, error) {
	var entries []toolEntry
	q := strings.ToLower(query)

	for _, e := range t.toolIndex {
		if q == "" ||
			strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Description), q) ||
			strings.Contains(strings.ToLower(e.Category), q) {
			entries = append(entries, e)
		}
	}

	if len(entries) == 0 {
		msg := fmt.Sprintf("No tools found matching %q", query)
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	var sb strings.Builder
	if query != "" {
		fmt.Fprintf(&sb, "Tools matching %q:\n\n", query)
	} else {
		sb.WriteString("All available tools:\n\n")
	}
	sb.WriteString("| Tool | Description | Category | Server |\n")
	sb.WriteString("|------|-------------|----------|--------|\n")
	for _, e := range entries {
		srv := e.Server
		if srv == "" {
			srv = "built-in"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", e.Name, e.Description, e.Category, srv)
	}
	sb.WriteString("\nUse `discover_tools` with `name=<tool>` to get the full parameter schema.")

	content := sb.String()
	return &interfaces.ToolResult{Success: true, LLMContent: content, UserContent: content}, nil
}

func (t *DiscoverToolsTool) getToolDetail(name string) (*interfaces.ToolResult, error) {
	if t.detailFn != nil {
		if detail, ok := t.detailFn(name); ok {
			// Pretty-print if JSON
			var v interface{}
			if json.Unmarshal([]byte(detail), &v) == nil {
				if pretty, err := json.MarshalIndent(v, "", "  "); err == nil {
					detail = string(pretty)
				}
			}
			content := fmt.Sprintf("Full schema for tool %q:\n\n%s", name, detail)
			return &interfaces.ToolResult{Success: true, LLMContent: content, UserContent: content}, nil
		}
	}

	// Fall back to summary
	if e, ok := t.toolIndex[name]; ok {
		content := fmt.Sprintf("Tool: %s\nDescription: %s\nCategory: %s\n(Full schema not available)", e.Name, e.Description, e.Category)
		return &interfaces.ToolResult{Success: true, LLMContent: content, UserContent: content}, nil
	}

	return &interfaces.ToolResult{
		Success:     false,
		LLMContent:  fmt.Sprintf("Tool %q not found", name),
		UserContent: fmt.Sprintf("Tool %q not found", name),
	}, nil
}

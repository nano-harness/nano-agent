package builtin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/watcher"
)

// ManageWatcherTool lets the LLM create, list, and delete event-monitoring
// watcher rules through conversation. It is registered on the main agent so
// that natural-language requests like "监听 aone/a1 的新 MR" are translated
// automatically into structured watcher configuration.
type ManageWatcherTool struct {
	mu        sync.Mutex
	watcher   *watcher.Watcher
	confirmFn func(summary string) bool
}

// NewManageWatcherTool creates a ManageWatcherTool.
// w may be nil; in that case the tool reports that the watcher is unavailable.
// confirmFn is called before creating a rule; nil means auto-confirm.
func NewManageWatcherTool(w *watcher.Watcher, confirmFn func(string) bool) *ManageWatcherTool {
	return &ManageWatcherTool{watcher: w, confirmFn: confirmFn}
}

// SetWatcher wires in a live Watcher so operations take effect immediately.
// This is called by the Engine after the Watcher has been started.
func (t *ManageWatcherTool) SetWatcher(w *watcher.Watcher) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.watcher = w
}

// Name returns the tool name.
func (t *ManageWatcherTool) Name() string { return "manage_watcher" }

// Description returns the tool description.
func (t *ManageWatcherTool) Description() string {
	return "Manage event-monitoring watcher rules: create (from natural language), list, delete, or check status. " +
		"Supported sources: 'aone' (Aone MR/CI events), 'shell' (custom shell command). " +
		"Created rules are persisted and survive restarts."
}

// Category returns the tool category.
func (t *ManageWatcherTool) Category() interfaces.ToolCategory { return interfaces.CategoryAgent }

// RequiresConfirmation returns false – handled internally.
func (t *ManageWatcherTool) RequiresConfirmation() bool { return false }

// ConcurrencySafe returns false.
func (t *ManageWatcherTool) ConcurrencySafe() bool { return false }

// Schema returns the JSON schema for the tool parameters.
func (t *ManageWatcherTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Manage event-monitoring watcher rules",
		map[string]*interfaces.PropertySchema{
			"action": {
				Type:        "string",
				Description: "Action to perform: create, list, delete, status",
				Enum:        []string{"create", "list", "delete", "status"},
			},
			"source": {
				Type:        "string",
				Description: "Event source type: 'aone' for Aone MR/CI events, 'shell' for custom shell commands",
			},
			"event": {
				Type:        "string",
				Description: "Event type to watch: 'new_mr', 'ci_failure', 'push', 'custom'",
			},
			"filter": {
				Type:        "string",
				Description: "Optional filter expression, e.g. 'repo:aone/a1 state:opened exclude_author:me'",
			},
			"command": {
				Type:        "string",
				Description: "Agent command template executed on each matching event. Supports Go template syntax: {{.MR_URL}}, {{.MR_TITLE}}, {{.OUTPUT}}, etc.",
			},
			"shell_command": {
				Type:        "string",
				Description: "For source='shell': the shell command to run. Must emit JSON array of event payloads, one per line, or plain text lines.",
			},
			"interval": {
				Type:        "string",
				Description: "Polling interval, e.g. '5m', '1h'. Defaults to 5m.",
			},
			"timeout": {
				Type:        "string",
				Description: "Maximum time allowed per command execution, e.g. '30m'. Defaults to 30m.",
			},
			"rule_id": {
				Type:        "string",
				Description: "Rule ID for delete/status actions",
			},
		},
		[]string{"action"},
	)
}

// Execute runs the watcher management action.
func (t *ManageWatcherTool) Execute(_ context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	action, ok := args["action"].(string)
	if !ok || action == "" {
		return nil, fmt.Errorf("action is required")
	}

	switch action {
	case "list":
		return t.listRules()
	case "create":
		return t.createRule(args)
	case "delete":
		return t.deleteRule(args)
	case "status":
		return t.statusRules()
	default:
		return nil, fmt.Errorf("unknown action %q; valid: create, list, delete, status", action)
	}
}

func (t *ManageWatcherTool) getWatcher() *watcher.Watcher {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.watcher
}

func (t *ManageWatcherTool) listRules() (*interfaces.ToolResult, error) {
	w := t.getWatcher()
	if w == nil {
		msg := "Watcher is not available. Enable it in config: watcher.enabled: true"
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	rules := w.ListRules()
	if len(rules) == 0 {
		msg := "No watcher rules configured. Use action='create' to add one."
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	var lines []string
	for _, r := range rules {
		lines = append(lines, fmt.Sprintf("- [%s] source=%s event=%s interval=%s command=%s",
			watcherShortID(r.ID), r.Source, r.Event, r.Interval, r.Command))
	}
	content := "Watcher rules:\n" + strings.Join(lines, "\n")
	return &interfaces.ToolResult{Success: true, LLMContent: content, UserContent: content}, nil
}

func (t *ManageWatcherTool) createRule(args map[string]interface{}) (*interfaces.ToolResult, error) {
	source, _ := args["source"].(string)
	if source == "" {
		return nil, fmt.Errorf("source is required for create action (e.g. 'aone' or 'shell')")
	}
	event, _ := args["event"].(string)
	command, _ := args["command"].(string)
	if command == "" {
		return nil, fmt.Errorf("command is required for create action")
	}

	filter, _ := args["filter"].(string)
	shellCmd, _ := args["shell_command"].(string)

	// Validate source-specific required fields.
	if source == "shell" && shellCmd == "" {
		return nil, fmt.Errorf("shell_command is required when source='shell'")
	}

	var interval time.Duration
	if iv, _ := args["interval"].(string); iv != "" {
		d, err := time.ParseDuration(iv)
		if err == nil {
			interval = d
		}
	}

	var timeout time.Duration
	if tv, _ := args["timeout"].(string); tv != "" {
		d, err := time.ParseDuration(tv)
		if err == nil {
			timeout = d
		}
	}

	// Require explicit user confirmation before creating a watcher rule that
	// can execute arbitrary shell commands on a recurring basis.
	summary := fmt.Sprintf("Create watcher rule:\n  Source: %s\n  Event: %s\n  Command: %s",
		source, event, command)
	if shellCmd != "" {
		summary += "\n  Shell command: " + shellCmd
	}
	if filter != "" {
		summary += "\n  Filter: " + filter
	}
	t.mu.Lock()
	cfn := t.confirmFn
	t.mu.Unlock()
	if cfn != nil && !cfn(summary) {
		msg := "Watcher rule creation cancelled by user"
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}

	w := t.getWatcher()
	if w == nil {
		msg := "Watcher is not available. Enable it in config: watcher.enabled: true"
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}

	rule := watcher.Rule{
		Source:       source,
		Event:        event,
		Filter:       filter,
		Command:      command,
		Interval:     interval,
		Timeout:      timeout,
		ShellCommand: shellCmd,
	}
	rule = w.AddRule(rule)

	msg := fmt.Sprintf("Watcher rule created:\n  ID: %s\n  Source: %s\n  Event: %s\n  Interval: %s\n  Command: %s",
		rule.ID, rule.Source, rule.Event, rule.Interval, rule.Command)
	if rule.Filter != "" {
		msg += "\n  Filter: " + rule.Filter
	}
	return &interfaces.ToolResult{
		Success:     true,
		LLMContent:  msg,
		UserContent: msg,
		Data:        map[string]interface{}{"rule_id": rule.ID},
	}, nil
}

func (t *ManageWatcherTool) deleteRule(args map[string]interface{}) (*interfaces.ToolResult, error) {
	ruleID, _ := args["rule_id"].(string)
	if ruleID == "" {
		return nil, fmt.Errorf("rule_id is required for delete action")
	}

	w := t.getWatcher()
	if w == nil {
		msg := "Watcher is not available"
		return &interfaces.ToolResult{Success: false, LLMContent: msg, UserContent: msg}, nil
	}

	if err := w.RemoveRule(ruleID); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			LLMContent:  fmt.Sprintf("Failed to delete rule %q: %v", ruleID, err),
			UserContent: fmt.Sprintf("Failed to delete rule %q: %v", ruleID, err),
		}, nil
	}

	msg := fmt.Sprintf("Watcher rule %q deleted", ruleID)
	return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
}

// watcherShortID returns a short prefix of a rule ID safe for display.
// If the ID is shorter than 8 characters the full ID is returned.
func watcherShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func (t *ManageWatcherTool) statusRules() (*interfaces.ToolResult, error) {
	w := t.getWatcher()
	if w == nil {
		msg := "Watcher is not available. Enable it in config: watcher.enabled: true"
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	rules := w.ListRules()
	if len(rules) == 0 {
		msg := "No watcher rules active."
		return &interfaces.ToolResult{Success: true, LLMContent: msg, UserContent: msg}, nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Active watcher rules: %d", len(rules)))
	for _, r := range rules {
		lines = append(lines, fmt.Sprintf("  [%s] source=%s event=%s interval=%s", watcherShortID(r.ID), r.Source, r.Event, r.Interval))
	}
	content := strings.Join(lines, "\n")
	return &interfaces.ToolResult{Success: true, LLMContent: content, UserContent: content}, nil
}

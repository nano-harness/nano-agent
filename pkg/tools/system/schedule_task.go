package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// ScheduleTaskTool allows scheduling tasks to run at specific times
type ScheduleTaskTool struct{}

// NewScheduleTaskTool creates a new schedule task tool
func NewScheduleTaskTool() *ScheduleTaskTool {
	return &ScheduleTaskTool{}
}

// Name returns the tool name
func (t *ScheduleTaskTool) Name() string {
	return "schedule_task"
}

// Description returns the tool description
func (t *ScheduleTaskTool) Description() string {
	return "Creates a scheduled task using cron expression (e.g. '0 * * * * *' for every minute). The task will be executed in the background by the daemon."
}

// Category returns the tool category
func (t *ScheduleTaskTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryAgent
}

// RequiresConfirmation returns whether the tool requires user confirmation
func (t *ScheduleTaskTool) RequiresConfirmation() bool {
	return true
}

// ConcurrencySafe returns false: scheduling tasks has external side effects.
func (t *ScheduleTaskTool) ConcurrencySafe() bool { return false }

// Schema returns the tool schema
func (t *ScheduleTaskTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema(
		"Creates a scheduled task using standard cron expression format (Seconds Minutes Hours DayOfMonth Month DayOfWeek)",
		map[string]*interfaces.PropertySchema{
			"cron_expression": {
				Type:        "string",
				Description: "Cron expression (e.g., '0 * * * * *' for every minute, '0 0 * * * *' for every hour). Includes seconds as the first field.",
			},
			"command": {
				Type:        "string",
				Description: "The natural language instruction or command for the agent to execute when the schedule triggers",
			},
			"action": {
				Type:        "string",
				Description: "Action to perform: 'create', 'list', or 'delete'",
				Enum:        []string{"create", "list", "delete"},
			},
			"task_id": {
				Type:        "string",
				Description: "The ID of the task to delete (required only for 'delete' action)",
			},
		},
		[]string{"action"},
	)
}

// doRequest performs an HTTP request to the local daemon
func doRequest(method, path string, body map[string]string) (map[string]any, error) {
	cfg := config.Get()
	if cfg == nil || cfg.Daemon == nil {
		return nil, fmt.Errorf("daemon configuration not available")
	}

	host := cfg.Daemon.Host
	if host == "0.0.0.0" || host == "" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s:%d/api/v1%s", host, cfg.Daemon.Port, path)

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if cfg.Daemon.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Daemon.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w (is the daemon running?)", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// Execute runs the tool
func (t *ScheduleTaskTool) Execute(ctx context.Context, args map[string]interface{}) (*interfaces.ToolResult, error) {
	action, ok := args["action"].(string)
	if !ok {
		return nil, fmt.Errorf("action is required")
	}

	var result map[string]any
	var err error

	switch action {
	case "create":
		cronExpr, ok := args["cron_expression"].(string)
		if !ok || cronExpr == "" {
			return nil, fmt.Errorf("cron_expression is required for create action")
		}
		command, ok := args["command"].(string)
		if !ok || command == "" {
			return nil, fmt.Errorf("command is required for create action")
		}
		reqBody := map[string]string{
			"cron_expression": cronExpr,
			"command":         command,
		}
		result, err = doRequest("POST", "/scheduler/tasks", reqBody)

	case "list":
		result, err = doRequest("GET", "/scheduler/tasks", nil)

	case "delete":
		taskID, ok := args["task_id"].(string)
		if !ok || taskID == "" {
			return nil, fmt.Errorf("task_id is required for delete action")
		}
		result, err = doRequest("DELETE", "/scheduler/tasks/"+taskID, nil)

	default:
		return nil, fmt.Errorf("invalid action: %s", action)
	}

	if err != nil {
		return nil, err
	}

	// Check if success
	if success, ok := result["success"].(bool); !ok || !success {
		if errorMsg, ok := result["error"].(string); ok {
			return nil, fmt.Errorf("daemon returned error: %s", errorMsg)
		}
		return nil, fmt.Errorf("daemon operation failed")
	}

	outputJSON, _ := json.MarshalIndent(result, "", "  ")

	return &interfaces.ToolResult{
		Success:     true,
		Data:        result,
		LLMContent:  string(outputJSON),
		UserContent: string(outputJSON),
	}, nil
}

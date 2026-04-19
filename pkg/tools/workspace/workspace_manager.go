package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/tools/filesystem"
)

// ManagerTool implements workspace creation and deletion functionality
type ManagerTool struct {
	workingDir string
	config     map[string]interface{}
}

// NewWorkspaceManagerTool creates a new ManagerTool instance
func NewWorkspaceManagerTool(workingDir string, config map[string]interface{}) *ManagerTool {
	if config == nil {
		config = make(map[string]interface{})
	}
	return &ManagerTool{
		workingDir: workingDir,
		config:     config,
	}
}

// Name returns the tool name
func (t *ManagerTool) Name() string {
	return "workspace_manager"
}

// Description returns the tool description
func (t *ManagerTool) Description() string {
	return "Create, delete, and manage development workspaces for remote programming"
}

// Category returns the tool category
func (t *ManagerTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryFileSystem
}

// RequiresConfirmation checks if the tool requires confirmation
func (t *ManagerTool) RequiresConfirmation() bool {
	return true // Workspace operations can be destructive
}

// ConcurrencySafe returns false: workspace operations mutate the filesystem.
func (t *ManagerTool) ConcurrencySafe() bool { return false }

// Schema returns the tool schema
func (t *ManagerTool) Schema() *interfaces.ToolSchema {
	actionProp := interfaces.NewStringPropertyWithEnum(
		"Action to perform on workspace",
		[]string{"create", "delete", "list", "info"},
	)
	actionProp.Examples = []string{"create", "delete", "list"}
	actionProp.Usage = "create: Create new workspace, delete: Remove workspace, list: Show all workspaces, info: Get workspace details"

	workspaceNameProp := interfaces.NewStringProperty("Name of the workspace (required for create/delete/info)")
	workspaceNameProp.Examples = []string{"my-project", "web-app", "api-service"}
	workspaceNameProp.Usage = "Use alphanumeric characters and hyphens. Will be used as directory name."

	workspacePathProp := interfaces.NewStringProperty("Custom path for workspace (optional, defaults to ~/workspaces/{name})")
	workspacePathProp.Examples = []string{"/home/user/projects/my-app", "./local-workspace"}
	workspacePathProp.Usage = "Absolute or relative path. If not specified, workspace will be created in default location."

	descriptionProp := interfaces.NewStringProperty("Description of the workspace (optional)")
	descriptionProp.Examples = []string{"React web application", "Go microservice", "Python data analysis"}
	descriptionProp.Usage = "Human-readable description for workspace identification and documentation."

	return interfaces.CreateSchema(
		"Manage development workspaces for remote programming",
		map[string]*interfaces.PropertySchema{
			"action":         actionProp,
			"workspace_name": workspaceNameProp,
			"workspace_path": workspacePathProp,
			"description":    descriptionProp,
		},
		[]string{"action"},
	)
}

// Execute executes the workspace manager tool
func (t *ManagerTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	if params == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "tool parameters are missing",
			UserContent: "❌ Failed to manage workspace: tool parameters are missing",
			LLMContent:  "workspace_manager failed: tool parameters are missing",
		}, nil
	}

	// Extract action
	action, ok := params["action"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "action parameter is required and must be a string",
			UserContent: "❌ Failed to manage workspace: action parameter is required",
			LLMContent:  "workspace_manager failed: action parameter is required",
		}, nil
	}

	switch action {
	case "create":
		return t.createWorkspace(ctx, params)
	case "delete":
		return t.deleteWorkspace(ctx, params)
	case "list":
		return t.listWorkspaces(ctx)
	case "info":
		return t.getWorkspaceInfo(ctx, params)
	default:
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("unsupported action: %s", action),
			UserContent: fmt.Sprintf("❌ Unsupported workspace action: %s", action),
			LLMContent:  fmt.Sprintf("workspace_manager failed: unsupported action %s", action),
		}, nil
	}
}

func (t *ManagerTool) createWorkspace(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	workspaceName, ok := params["workspace_name"].(string)
	if !ok || workspaceName == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "workspace_name is required for create action",
			UserContent: "❌ Workspace name is required for creation",
			LLMContent:  "workspace_manager create failed: workspace_name is required",
		}, nil
	}

	// Validate workspace name
	if err := t.validateWorkspaceName(workspaceName); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Invalid workspace name: %s", err.Error()),
			LLMContent:  fmt.Sprintf("workspace_manager create failed: %s", err.Error()),
		}, nil
	}

	// Determine workspace path
	var workspacePath string
	if customPath, ok := params["workspace_path"].(string); ok && customPath != "" {
		workspacePath = customPath
	} else {
		// Default to ~/workspaces/{name}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       fmt.Sprintf("failed to get home directory: %v", err),
				UserContent: "❌ Failed to determine workspace location",
				LLMContent:  fmt.Sprintf("workspace_manager create failed: %v", err),
			}, nil
		}
		workspacePath = filepath.Join(homeDir, "workspaces", workspaceName)
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to resolve workspace path: %v", err),
			UserContent: "❌ Failed to resolve workspace path",
			LLMContent:  fmt.Sprintf("workspace_manager create failed: %v", err),
		}, nil
	}

	// Check if workspace already exists
	if _, err := os.Stat(absPath); err == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "workspace already exists",
			UserContent: fmt.Sprintf("❌ Workspace '%s' already exists at %s", workspaceName, absPath),
			LLMContent:  fmt.Sprintf("workspace_manager create failed: workspace %s already exists", workspaceName),
		}, nil
	}

	// Create workspace directory
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to create workspace directory: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to create workspace directory: %v", err),
			LLMContent:  fmt.Sprintf("workspace_manager create failed: %v", err),
		}, nil
	}

	// Create workspace metadata file
	description := ""
	if desc, ok := params["description"].(string); ok {
		description = desc
	}

	if err := t.createWorkspaceMetadata(absPath, workspaceName, description); err != nil {
		// Use logger instead of fmt.Printf to prevent TUI interference
		logger.Warnf("Failed to create workspace metadata: %v", err)
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"workspace_name": workspaceName,
			"workspace_path": absPath,
			"description":    description,
		},
		UserContent: fmt.Sprintf("✅ Workspace '%s' created successfully at %s", workspaceName, absPath),
		LLMContent:  fmt.Sprintf("workspace_manager create successful: workspace %s created at %s", workspaceName, absPath),
	}, nil
}

func (t *ManagerTool) deleteWorkspace(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	workspaceName, ok := params["workspace_name"].(string)
	if !ok || workspaceName == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "workspace_name is required for delete action",
			UserContent: "❌ Workspace name is required for deletion",
			LLMContent:  "workspace_manager delete failed: workspace_name is required",
		}, nil
	}

	// Find workspace path
	workspacePath, err := t.findWorkspacePath(workspaceName)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Workspace '%s' not found", workspaceName),
			LLMContent:  fmt.Sprintf("workspace_manager delete failed: %s", err.Error()),
		}, nil
	}

	// Remove workspace directory
	if err := os.RemoveAll(workspacePath); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to delete workspace: %v", err),
			UserContent: fmt.Sprintf("❌ Failed to delete workspace: %v", err),
			LLMContent:  fmt.Sprintf("workspace_manager delete failed: %v", err),
		}, nil
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"workspace_name": workspaceName,
			"workspace_path": workspacePath,
		},
		UserContent: fmt.Sprintf("✅ Workspace '%s' deleted successfully", workspaceName),
		LLMContent:  fmt.Sprintf("workspace_manager delete successful: workspace %s deleted", workspaceName),
	}, nil
}

func (t *ManagerTool) listWorkspaces(_ context.Context) (*interfaces.ToolResult, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to get home directory: %v", err),
			UserContent: "❌ Failed to access workspace directory",
			LLMContent:  fmt.Sprintf("workspace_manager list failed: %v", err),
		}, nil
	}

	workspacesDir := filepath.Join(homeDir, "workspaces")
	if _, err := os.Stat(workspacesDir); os.IsNotExist(err) {
		return &interfaces.ToolResult{
			Success:     true,
			Data:        []interface{}{},
			UserContent: "📁 No workspaces found",
			LLMContent:  "workspace_manager list: no workspaces found",
		}, nil
	}

	entries, err := os.ReadDir(workspacesDir)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to read workspaces directory: %v", err),
			UserContent: "❌ Failed to read workspaces directory",
			LLMContent:  fmt.Sprintf("workspace_manager list failed: %v", err),
		}, nil
	}

	var workspaces []map[string]interface{}
	for _, entry := range entries {
		if entry.IsDir() {
			workspacePath := filepath.Join(workspacesDir, entry.Name())
			workspaceInfo := map[string]interface{}{
				"name": entry.Name(),
				"path": workspacePath,
			}

			// Try to read metadata
			if metadata, err := t.readWorkspaceMetadata(workspacePath); err == nil {
				workspaceInfo["description"] = metadata["description"]
				workspaceInfo["created_at"] = metadata["created_at"]
			}

			workspaces = append(workspaces, workspaceInfo)
		}
	}

	// Format user content
	userContent := "📁 Available workspaces:\n"
	for _, ws := range workspaces {
		userContent += fmt.Sprintf("  • %s (%s)\n", ws["name"], ws["path"])
		if desc, ok := ws["description"].(string); ok && desc != "" {
			userContent += fmt.Sprintf("    %s\n", desc)
		}
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        workspaces,
		UserContent: userContent,
		LLMContent:  fmt.Sprintf("workspace_manager list successful: found %d workspaces", len(workspaces)),
	}, nil
}

func (t *ManagerTool) getWorkspaceInfo(_ context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	workspaceName, ok := params["workspace_name"].(string)
	if !ok || workspaceName == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "workspace_name is required for info action",
			UserContent: "❌ Workspace name is required",
			LLMContent:  "workspace_manager info failed: workspace_name is required",
		}, nil
	}

	workspacePath, err := t.findWorkspacePath(workspaceName)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ Workspace '%s' not found", workspaceName),
			LLMContent:  fmt.Sprintf("workspace_manager info failed: %s", err.Error()),
		}, nil
	}

	workspaceInfo := map[string]interface{}{
		"name": workspaceName,
		"path": workspacePath,
	}

	// Read metadata if available
	if metadata, err := t.readWorkspaceMetadata(workspacePath); err == nil {
		for key, value := range metadata {
			workspaceInfo[key] = value
		}
	}

	// Get directory size and file count
	if stat, err := os.Stat(workspacePath); err == nil {
		workspaceInfo["modified_at"] = stat.ModTime()
	}

	userContent := fmt.Sprintf("📁 Workspace: %s\n", workspaceName)
	userContent += fmt.Sprintf("📍 Path: %s\n", workspacePath)
	if desc, ok := workspaceInfo["description"].(string); ok && desc != "" {
		userContent += fmt.Sprintf("📝 Description: %s\n", desc)
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        workspaceInfo,
		UserContent: userContent,
		LLMContent:  fmt.Sprintf("workspace_manager info successful: workspace %s found at %s", workspaceName, workspacePath),
	}, nil
}

// Helper functions

func (t *ManagerTool) validateWorkspaceName(name string) error {
	if name == "" {
		return fmt.Errorf("workspace name cannot be empty")
	}
	if len(name) > 50 {
		return fmt.Errorf("workspace name too long (max 50 characters)")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("workspace name cannot contain path separators")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("workspace name cannot start with a dot")
	}
	return nil
}

func (t *ManagerTool) findWorkspacePath(workspaceName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %v", err)
	}

	workspacePath := filepath.Join(homeDir, "workspaces", workspaceName)
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		return "", fmt.Errorf("workspace '%s' not found", workspaceName)
	}

	return workspacePath, nil
}

func (t *ManagerTool) createWorkspaceMetadata(workspacePath, name, description string) error {
	// Create a simple metadata file
	metadataPath := filepath.Join(workspacePath, ".workspace")
	file, err := os.Create(metadataPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	_, err = fmt.Fprintf(file, "name=%s\n", name)
	if err != nil {
		return err
	}
	if description != "" {
		_, err = fmt.Fprintf(file, "description=%s\n", description)
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(file, "created_at=%d\n", time.Now().Unix())
	return err
}

func (t *ManagerTool) readWorkspaceMetadata(workspacePath string) (map[string]interface{}, error) {
	metadataPath := filepath.Join(workspacePath, ".workspace")
	content, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, err
	}

	// Detect if metadata file is binary (defensive programming)
	detection := filesystem.DetectBinaryContent(content)
	if detection.IsBinary {
		return nil, fmt.Errorf("workspace metadata file appears to be binary (encoding: %s, confidence: %.2f)",
			detection.Encoding, detection.Confidence)
	}

	metadata := make(map[string]interface{})
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				metadata[parts[0]] = parts[1]
			}
		}
	}

	return metadata, nil
}

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/mcp"
	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/nano-harness/nano-agent/pkg/openspec"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
	"github.com/nano-harness/nano-agent/pkg/toolruntime"
	"github.com/nano-harness/nano-agent/pkg/tools/filesystem"
	mcptool "github.com/nano-harness/nano-agent/pkg/tools/mcp"
	openspectool "github.com/nano-harness/nano-agent/pkg/tools/openspec"
	"github.com/nano-harness/nano-agent/pkg/tools/system"
	"github.com/nano-harness/nano-agent/pkg/tools/web"
	"github.com/nano-harness/nano-agent/pkg/tools/workspace"

	"github.com/fatih/color"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ToolboxConfig holds configuration for the toolbox
type ToolboxConfig struct {
	WorkingDirectory      string            `json:"working_directory"`
	Timeout               time.Duration     `json:"timeout"`
	MaxFileSize           int64             `json:"max_file_size"`
	MaxResponseSize       int64             `json:"max_response_size"`
	UserAgent             string            `json:"user_agent"`
	AllowedCommands       []string          `json:"allowed_commands"`
	BlockedCommands       []string          `json:"blocked_commands"`
	SensitiveReadPaths    []string          `json:"sensitive_read_paths"`
	ArbitraryExecCommands []string          `json:"arbitrary_exec_commands"`
	EnabledTools          []string          `json:"enabled_tools"`
	DisabledTools         []string          `json:"disabled_tools"`
	WebSearchAPIKeys      map[string]string `json:"web_search_api_keys"`
	EnableMCP             bool              `json:"enable_mcp"`
	MCPConfig             *mcp.MCPConfig    `json:"mcp"`

	// Tool-specific configurations
	ReadFileMaxLines    int
	SearchMaxResults    int
	WebRequestTimeout   int
	WebSearchTimeout    int
	WebMaxContentSize   int
	WebSearchMaxResults int
	FileDiffMaxLines    int
	GitMaxLogEntries    int

	// NEW: Environment filtering and strict mode for system tools like ShellTool
	AllowedEnvVars []string `json:"allowed_env_vars"`
	BlockedEnvVars []string `json:"blocked_env_vars"`
	Strict         bool     `json:"strict"`

	// NEW: Generic image generator settings
	ImageAPIKey  string `json:"image_api_key"`
	ImageBaseURL string `json:"image_base_url"`

	// OpenSpec integration settings
	EnableOpenSpec        bool   `json:"enable_openspec"`
	OpenSpecRootDir       string `json:"openspec_root_dir"`       // Default: "openspec"
	OpenSpecDefaultSchema string `json:"openspec_default_schema"` // Default: "spec-driven"
	OpenSpecMaxArtifact   int64  `json:"openspec_max_artifact"`   // Max artifact file size in bytes (0 = unlimited)

	// Sandbox configuration for process-level and path-level isolation.
	Sandbox *config.SandboxConfig `json:"sandbox"`
}

// UpdateEvent represents a tools update event
type UpdateEvent struct {
	Tools []interfaces.Tool
	Type  string // "mcp_registered", "mcp_unregistered", etc.
}

// sandboxConfig is a nil-safe accessor for the Sandbox config.
func (c *ToolboxConfig) sandboxConfig() *config.SandboxConfig {
	if c == nil {
		return nil
	}
	return c.Sandbox
}

// Toolbox manages available development tools using the new architecture
type Toolbox struct {
	registry        interfaces.ToolRegistry
	workingDir      string
	config          *ToolboxConfig
	mcpClient       *mcp.MCPClient
	mcpMonitorStop  chan struct{}
	toolsUpdateChan chan UpdateEvent          // Channel for tools update events
	readFileState   *filesystem.ReadFileState // shared by ReadFileTool and EditTool
	chain           *middleware.Chain         // Middleware chain applied to all Execute calls
	runtime         *toolruntime.Runtime
}

// MCP returns the MCP client
func (tb *Toolbox) MCP() *mcp.MCPClient {
	return tb.mcpClient
}

// DefaultToolRegistry is re-exported for backward compatibility with callers
// that constructed toolruntime.Registry through the tools package before the
// toolruntime package owned registry execution.
type DefaultToolRegistry = toolruntime.Registry

// NewDefaultToolRegistry creates a new DefaultToolRegistry.
func NewDefaultToolRegistry() *DefaultToolRegistry {
	return toolruntime.NewRegistry()
}

// NewToolbox creates a new toolbox with default tools
func NewToolbox(workingDir string, config *ToolboxConfig, _ interface{}) *Toolbox {
	if config == nil {
		config = &ToolboxConfig{
			WorkingDirectory: workingDir,
			Timeout:          30 * time.Second,
			MaxFileSize:      10 * 1024 * 1024, // 10MB
			MaxResponseSize:  10 * 1024 * 1024, // 10MB
			UserAgent:        "nano/1.0",
		}
	}

	tb := &Toolbox{
		registry:        NewDefaultToolRegistry(),
		workingDir:      workingDir,
		config:          config,
		toolsUpdateChan: make(chan UpdateEvent, 10), // Buffered channel
	}

	tb.registerDefaultTools()
	tb.chain = tb.buildDefaultChain()
	tb.runtime = toolruntime.NewRuntime(tb.registry, tb.chain, nil)

	// Initialize MCP if enabled
	if config.EnableMCP {
		tb.initializeMCP()
	}

	return tb
}

// buildDefaultChain constructs the default middleware chain: metrics → security.
func (tb *Toolbox) buildDefaultChain() *middleware.Chain {
	var ms []middleware.ToolMiddleware
	ms = append(ms, middleware.NewMetricsMiddleware())

	guard := middleware.NewCommandGuardWithConfig(
		nil,
		nil,
		nil,
		tb.workingDir,
		tb.config.SensitiveReadPaths,
		tb.config.ArbitraryExecCommands,
	)
	pathChecker := sandbox.NewPathChecker(tb.config.sandboxConfig())
	const defaultMaxFileSize = 100 * 1024 * 1024 // 100 MB
	ms = append(ms, middleware.NewSecurityMiddleware(guard, pathChecker, defaultMaxFileSize))

	return middleware.NewChain(ms...)
}

// SetMiddlewareChain replaces the toolbox middleware chain.
func (tb *Toolbox) SetMiddlewareChain(chain *middleware.Chain) {
	tb.chain = chain
	if tb.runtime != nil {
		tb.runtime.SetMiddlewareChain(chain)
	}
}

// registerDefaultTools registers the default set of tools
func (tb *Toolbox) registerDefaultTools() {
	// Get toolbox config for tools
	config := tb.config.ToolConfigMap()

	// Build enabled/disabled sets for filtering
	var enabledSet map[string]struct{}
	var disabledSet map[string]struct{}
	if tb.config != nil {
		if len(tb.config.EnabledTools) > 0 {
			enabledSet = make(map[string]struct{}, len(tb.config.EnabledTools))
			for _, n := range tb.config.EnabledTools {
				enabledSet[n] = struct{}{}
			}
		}
		if len(tb.config.DisabledTools) > 0 {
			disabledSet = make(map[string]struct{}, len(tb.config.DisabledTools))
			for _, n := range tb.config.DisabledTools {
				disabledSet[n] = struct{}{}
			}
		}
	}
	isAllowed := func(name string) bool {
		if enabledSet != nil {
			if _, ok := enabledSet[name]; !ok {
				return false
			}
		}
		if disabledSet != nil {
			if _, blocked := disabledSet[name]; blocked {
				return false
			}
		}
		return true
	}

	// Core tools that are always needed.
	// ReadFileTool and EditTool share a ReadFileState so that EditTool can
	// enforce the "read before edit" safety policy.
	// PathChecker is shared by all filesystem tools for AllowedPaths / BlockedPaths checks.
	readFileState := filesystem.NewReadFileState()
	tb.readFileState = readFileState // expose for per-turn reset
	var sandboxCfg = tb.config.sandboxConfig()
	pathChecker := sandbox.NewPathChecker(sandboxCfg)

	// Initialize singleton BackgroundTaskManager for background task support
	homeDir, _ := os.UserHomeDir()
	bgRootDir := filepath.Join(homeDir, ".nano", "bg-tasks")
	bgManager := system.NewBackgroundTaskManager(bgRootDir)

	coreTools := []interfaces.Tool{
		filesystem.NewReadFileToolWithState(tb.workingDir, config, pathChecker, readFileState),
		filesystem.NewReadPDFTool(tb.workingDir, pathChecker),
		filesystem.NewCodeSkeletonTool(tb.workingDir, config, nil),
		filesystem.NewWriteFileToolWithState(tb.workingDir, config, pathChecker, readFileState),
		filesystem.NewEditToolWithState(tb.workingDir, config, pathChecker, readFileState),
		filesystem.NewDeleteToolWithState(tb.workingDir, config, pathChecker, readFileState),
		system.NewShellToolWithBgManager(tb.workingDir, config, sandboxCfg, bgManager),
		system.NewTodoWriteTool(),
		system.NewBashOutputTool(bgManager),
		system.NewKillBashTool(bgManager),
		system.NewListBackgroundTool(bgManager),
	}

	// Register core tools first (with filtering)
	for _, tool := range coreTools {
		if !isAllowed(tool.Name()) {
			logger.Debugf("Skipping core tool %s due to Enabled/Disabled policy", tool.Name())
			continue
		}
		if err := tb.registry.Register(tool); err != nil {
			// Use Debug level for core tool registration errors to avoid TUI spam
			logger.Debugf("Failed to register core tool %s: %v", tool.Name(), err)
		}
	}

	// Register web tools (these might fail due to missing dependencies)
	webConfig := make(map[string]interface{})
	for key, value := range config {
		webConfig[key] = value
	}

	// Add API keys for web search if available
	if tb.config != nil && tb.config.WebSearchAPIKeys != nil {
		webConfig["api_keys"] = tb.config.WebSearchAPIKeys
	}

	// Register web tools with error handling (with filtering)
	webTools := []interfaces.Tool{
		web.NewWebFetchTool(webConfig),
		web.NewWebSearchTool(webConfig),
		web.NewImageGenerateTool(),
	}

	for _, tool := range webTools {
		if !isAllowed(tool.Name()) {
			logger.Debugf("Skipping web tool %s due to Enabled/Disabled policy", tool.Name())
			continue
		}
		if err := tb.registry.Register(tool); err != nil {
			logger.Warnf("Failed to register web tool %s: %v", tool.Name(), err)
		}
	}

	// Register workspace tools
	workspace.RegisterWorkspaceTools(tb.registry, tb.workingDir, config, nil)

	// Register OpenSpec tools if enabled
	if tb.config != nil && tb.config.EnableOpenSpec {
		rootDir := tb.config.OpenSpecRootDir
		if rootDir == "" {
			rootDir = "openspec"
		}
		defaultSchema := tb.config.OpenSpecDefaultSchema
		if defaultSchema == "" {
			defaultSchema = "spec-driven"
		}
		am := openspec.NewArtifactManager(rootDir, tb.workingDir)
		if tb.config.OpenSpecMaxArtifact > 0 {
			am.SetMaxArtifactSize(tb.config.OpenSpecMaxArtifact)
		}
		engine := openspec.NewWorkflowEngine(am, defaultSchema)
		openspectool.RegisterOpenSpecTools(tb.registry, engine)
	}

	// Prune any tools not allowed (covers workspace-registered tools)
	if enabledSet != nil || disabledSet != nil {
		for _, tool := range tb.registry.List() {
			if !isAllowed(tool.Name()) {
				_ = tb.registry.Unregister(tool.Name())
				logger.Debugf("Unregistered tool %s due to Enabled/Disabled policy", tool.Name())
			}
		}
	}

	logger.Infof("Registered %d default tools", len(tb.registry.List()))
}

// Register adds a new tool to the toolbox
func (tb *Toolbox) Register(tool interfaces.Tool) error {
	return tb.registry.Register(tool)
}

// Unregister removes a tool from the toolbox
func (tb *Toolbox) Unregister(name string) error {
	return tb.registry.Unregister(name)
}

// Get retrieves a tool by name, with MCP tool name resolution.
// If the exact name is not found and MCP is enabled, attempts to resolve
// the original tool name (without the mcp_<server>_ prefix).
func (tb *Toolbox) Get(name string) (interfaces.Tool, bool) {
	// First try exact match
	tool, exists := tb.registry.Get(name)
	if exists {
		return tool, true
	}

	// If not found and MCP is enabled, try MCP tool name resolution
	if tb.mcpClient != nil {
		tool, exists = tb.resolveMCPToolByOriginalName(name)
		if exists {
			return tool, true
		}
	}

	return nil, false
}

// List returns all available tools
func (tb *Toolbox) List() []interfaces.Tool {
	return tb.registry.List()
}

// Descriptors returns typed metadata for all registered tools.
func (tb *Toolbox) Descriptors() []ToolDescriptor {
	return toolruntime.NewCatalog(tb.registry).Descriptors()
}

// ListByCategory returns tools in a specific category
func (tb *Toolbox) ListByCategory(category interfaces.ToolCategory) []interfaces.Tool {
	return tb.registry.ListByCategory(category)
}

// Schemas returns all tool schemas for LLM consumption
func (tb *Toolbox) Schemas() map[string]*interfaces.ToolSchema {
	return tb.registry.Schemas()
}

// Execute runs a tool with given parameters through ToolRuntime.
func (tb *Toolbox) Execute(ctx context.Context, name string, params map[string]interface{}) (*interfaces.ToolResult, error) {
	// 1. LLM may directly use MCP tool's original name (without mcp_<server>_ prefix).
	//    registry only stores the rewritten registered name, so we need to resolve it first.
	if _, ok := tb.registry.Get(name); !ok && tb.mcpClient != nil {
		if resolved, ok := tb.resolveMCPToolByOriginalName(name); ok {
			logger.Debugf("Resolved MCP tool '%s' to registered name '%s'", name, resolved.Name())
			name = resolved.Name()
		}
	}

	logger.Infof("Running tool: %s", name)

	// Keep Execute safe for compatibility with legacy tests and callers that
	// construct Toolbox literals instead of using NewToolbox.
	if tb.runtime == nil {
		tb.runtime = toolruntime.NewRuntime(tb.registry, tb.chain, nil)
	}
	return tb.runtime.Execute(ctx, name, params)
}

// GetConfig returns the toolbox configuration
func (tb *Toolbox) GetConfig() *ToolboxConfig {
	return tb.config
}

// GetWorkingDirectory returns the current working directory
func (tb *Toolbox) GetWorkingDirectory() string {
	return tb.workingDir
}

// ResetReadFileState clears the set of files that have been read in this session.
// Call this at the start of every new Turn so that the "read before edit" safety
// check is re-enforced for each independent conversation turn.
func (tb *Toolbox) ResetReadFileState() {
	if tb.readFileState != nil {
		tb.readFileState.Reset()
	}
}

// GetToolsUpdateChannel returns the channel for receiving tools update events
func (tb *Toolbox) GetToolsUpdateChannel() <-chan UpdateEvent {
	return tb.toolsUpdateChan
}

// sendToolsUpdateEvent sends a tools update event through the channel
func (tb *Toolbox) sendToolsUpdateEvent(eventType string) {
	if tb.toolsUpdateChan != nil {
		select {
		case tb.toolsUpdateChan <- UpdateEvent{
			Tools: tb.List(),
			Type:  eventType,
		}:
			// Event sent successfully
		default:
			// Channel is full, skip this event to avoid blocking
			logger.Debugf("Tools update channel is full, skipping event: %s", eventType)
		}
	}
}

// Close closes the tools update channel and cleans up resources
func (tb *Toolbox) Close() {
	if tb.toolsUpdateChan != nil {
		close(tb.toolsUpdateChan)
		tb.toolsUpdateChan = nil
	}
}

// PrintToolList prints a formatted list of all available tools
func (tb *Toolbox) PrintToolList() {
	tools := tb.List()
	if len(tools) == 0 {
		color.Yellow("No tools available")
		return
	}

	// Group tools by category
	categories := make(map[interfaces.ToolCategory][]interfaces.Tool)
	for _, tool := range tools {
		category := tool.Category()
		categories[category] = append(categories[category], tool)
	}

	// Print tools by category
	for category, categoryTools := range categories {
		color.Cyan("\n%s Tools:", cases.Title(language.English).String(string(category)))
		for _, tool := range categoryTools {
			confirmationMark := ""
			if tool.RequiresConfirmation() {
				confirmationMark = " ⚠️"
			}
			color.White("  %-20s %s%s", tool.Name(), tool.Description(), confirmationMark)
		}
	}

	color.Yellow("\n⚠️  = Requires user confirmation")
}

// initializeMCP initializes MCP client and server functionality
func (tb *Toolbox) initializeMCP() {
	logger.Info("Initializing MCP functionality")

	var mcpConfig *mcp.MCPConfig

	// Use provided MCP config directly
	if tb.config.MCPConfig != nil {
		mcpConfig = tb.config.MCPConfig
	} else {
		mcpConfig = &mcp.MCPConfig{
			EnableClient:     true,
			MCPServers:       []mcp.MCPServerConfig{},
			DefaultTransport: "stdio",
			Timeout:          30 * time.Second,
			MaxRetries:       3,
			EnableAuth:       false,
			AuthTokens:       make(map[string]string),
			TLSConfig: &mcp.TLSConfig{
				Enabled:    false,
				SkipVerify: false,
			},
		}
	}

	// Create MCP client
	tb.mcpClient = mcp.NewMCPClient(mcpConfig)
	tb.mcpClient.SetSandboxRuntime(sandbox.NewRuntime(tb.config.sandboxConfig(), tb.workingDir), tb.workingDir)

	logger.Info("MCP client initialized successfully")
}

// StartMCP starts MCP client and registers remote tools
func (tb *Toolbox) StartMCP(ctx context.Context) error {
	if tb.mcpClient == nil {
		return fmt.Errorf("MCP not initialized")
	}

	// Start MCP client
	err := tb.mcpClient.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start MCP client: %w", err)
	}

	// Wait for connections to be established with timeout
	err = tb.waitForMCPConnections(ctx, 10*time.Second)
	if err != nil {
		logger.Warnf("Failed to establish all MCP connections: %v", err)
		// Continue anyway, some connections might be working
	}

	// Register remote tools as local tools
	tb.registerMCPTools()

	// Start background tool registration monitor
	go tb.monitorMCPToolRegistration()

	logger.Info("MCP client started and remote tools registered")
	return nil
}

// StopMCP stops MCP functionality
func (tb *Toolbox) StopMCP() error {
	if tb.mcpClient == nil {
		return nil
	}

	// Stop the monitoring goroutine
	if tb.mcpMonitorStop != nil {
		close(tb.mcpMonitorStop)
		tb.mcpMonitorStop = nil
	}

	// Unregister MCP tools
	tb.unregisterMCPTools()

	// Stop MCP client
	err := tb.mcpClient.Stop()
	if err != nil {
		return fmt.Errorf("failed to stop MCP client: %w", err)
	}

	logger.Info("MCP stopped")
	return nil
}

// waitForMCPConnections waits for MCP connections to be established
func (tb *Toolbox) waitForMCPConnections(ctx context.Context, timeout time.Duration) error {
	if tb.mcpClient == nil {
		return fmt.Errorf("MCP client not initialized")
	}

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Poll for connections with exponential backoff
	initialDelay := 100 * time.Millisecond
	maxDelay := 2 * time.Second
	currentDelay := initialDelay

	for {
		select {
		case <-timeoutCtx.Done():
			status := tb.mcpClient.Status()
			if status.ConnectedServers > 0 {
				logger.Infof("Partial MCP connections established: %d/%d servers connected",
					status.ConnectedServers, status.ConfiguredServers)
				return nil // Accept partial connections
			}
			return fmt.Errorf("timeout waiting for MCP connections: %d/%d servers connected",
				status.ConnectedServers, status.ConfiguredServers)
		default:
			status := tb.mcpClient.Status()
			if status.ConfiguredServers > 0 && status.ConnectedServers == status.ConfiguredServers {
				logger.Infof("All MCP connections established: %d/%d servers connected",
					status.ConnectedServers, status.ConfiguredServers)
				return nil
			}

			// Wait before next check
			select {
			case <-timeoutCtx.Done():
				// Re-evaluate status on timeout to allow partial connections
				status := tb.mcpClient.Status()
				if status.ConnectedServers > 0 {
					logger.Infof("Partial MCP connections established: %d/%d servers connected",
						status.ConnectedServers, status.ConfiguredServers)
					return nil
				}
				return fmt.Errorf("timeout waiting for MCP connections: %d/%d servers connected",
					status.ConnectedServers, status.ConfiguredServers)
			case <-time.After(currentDelay):
				// Exponential backoff
				currentDelay = currentDelay * 2
				if currentDelay > maxDelay {
					currentDelay = maxDelay
				}
			}
		}
	}
}

// registerMCPTools registers remote MCP tools as local tools
func (tb *Toolbox) registerMCPTools() {
	if tb.mcpClient == nil {
		return
	}

	// Get all remote tools
	allTools := tb.mcpClient.GetAllTools()

	for serverName, tools := range allTools {
		for _, toolInfo := range tools {
			// Create MCP tool with real schema
			mcpTool := mcptool.NewMCPTool(serverName, toolInfo.Name, toolInfo.Description, toolInfo.InputSchema, tb)

			// Check if tool is already registered
			if _, exists := tb.registry.Get(mcpTool.Name()); exists {
				logger.Debugf("MCP tool %s from server %s is already registered, skipping",
					toolInfo.Name, serverName)
				continue // Skip already registered tools
			}

			// Register the tool
			err := tb.registry.Register(mcpTool)
			if err != nil {
				// Use Debug level for duplicate registration errors to avoid TUI spam
				logger.Debugf("Failed to register MCP tool %s from server %s: %v",
					toolInfo.Name, serverName, err)
			} else {
				logger.Infof("Registered MCP tool: %s from server %s",
					toolInfo.Name, serverName)
			}
		}
	}

	// Get all remote resources
	allResources := tb.mcpClient.GetAllResources()

	for serverName, resources := range allResources {
		for _, resourceInfo := range resources {
			// Create MCP resource tool wrapper
			mcpResourceTool := mcptool.NewMCPResourceTool(
				serverName, resourceInfo.URI, resourceInfo.Name, tb)

			// Check if resource tool is already registered
			if _, exists := tb.registry.Get(mcpResourceTool.Name()); exists {
				logger.Debugf("MCP resource %s from server %s is already registered, skipping",
					resourceInfo.Name, serverName)
				continue // Skip already registered resource tools
			}

			// Register the resource tool
			err := tb.registry.Register(mcpResourceTool)
			if err != nil {
				// Use Debug level for duplicate registration errors to avoid TUI spam
				logger.Debugf("Failed to register MCP resource %s from server %s: %v",
					resourceInfo.Name, serverName, err)
			} else {
				logger.Infof("Registered MCP resource: %s from server %s",
					resourceInfo.Name, serverName)
			}
		}
	}

	// Get all remote prompts
	allPrompts := tb.mcpClient.GetAllPrompts()

	for serverName, prompts := range allPrompts {
		for _, promptInfo := range prompts {
			// Create MCP prompt tool wrapper
			mcpPromptTool := mcptool.NewMCPPromptTool(serverName, promptInfo.Name, tb)

			// Check if prompt tool is already registered
			if _, exists := tb.registry.Get(mcpPromptTool.Name()); exists {
				logger.Debugf("MCP prompt %s from server %s is already registered, skipping",
					promptInfo.Name, serverName)
				continue // Skip already registered prompt tools
			}

			// Register the prompt tool
			err := tb.registry.Register(mcpPromptTool)
			if err != nil {
				// Use Debug level for duplicate registration errors to avoid TUI spam
				logger.Debugf("Failed to register MCP prompt %s from server %s: %v",
					promptInfo.Name, serverName, err)
			} else {
				logger.Infof("Registered MCP prompt: %s from server %s",
					promptInfo.Name, serverName)
			}
		}
	}

	// Notify that tools have been updated
	tb.sendToolsUpdateEvent("mcp_registered")
}

// unregisterMCPTools removes all MCP tools from the registry
func (tb *Toolbox) unregisterMCPTools() {
	tools := tb.registry.List()

	for _, tool := range tools {
		if tool.Category() == interfaces.CategoryMCP {
			err := tb.registry.Unregister(tool.Name())
			if err != nil {
				// Use Debug level for unregistration errors to avoid TUI spam
				logger.Debugf("Failed to unregister MCP tool %s: %v", tool.Name(), err)
			} else {
				logger.Infof("Unregistered MCP tool: %s", tool.Name())
			}
		}
	}

	// Notify that tools have been updated
	tb.sendToolsUpdateEvent("mcp_unregistered")
}

// monitorMCPToolRegistration monitors for new MCP connections and registers tools dynamically
func (tb *Toolbox) monitorMCPToolRegistration() {
	if tb.mcpClient == nil {
		return
	}

	// Initialize stop channel if not already done
	if tb.mcpMonitorStop == nil {
		tb.mcpMonitorStop = make(chan struct{})
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	registeredTools := make(map[string]bool)

	// Initialize with currently registered MCP tools
	tools := tb.registry.List()
	for _, tool := range tools {
		if tool.Category() == interfaces.CategoryMCP {
			registeredTools[tool.Name()] = true
		}
	}

	for {
		select {
		case <-tb.mcpMonitorStop:
			logger.Info("MCP tool registration monitor stopped")
			return
		case <-ticker.C:
			// Check for new tools, resources, and prompts
			newToolsFound := false

			// Check tools
			allTools := tb.mcpClient.GetAllTools()
			for serverName, tools := range allTools {
				for _, toolInfo := range tools {
					// Create MCP tool with real schema information
					mcpTool := mcptool.NewMCPTool(serverName, toolInfo.Name, toolInfo.Description, toolInfo.InputSchema, tb)
					if !registeredTools[mcpTool.Name()] {
						newToolsFound = true
						break
					}
				}
				if newToolsFound {
					break
				}
			}

			// Check resources if no new tools found
			if !newToolsFound {
				allResources := tb.mcpClient.GetAllResources()
				for serverName, resources := range allResources {
					for _, resourceInfo := range resources {
						mcpResourceTool := mcptool.NewMCPResourceTool(serverName, resourceInfo.URI, resourceInfo.Name, tb)
						if !registeredTools[mcpResourceTool.Name()] {
							newToolsFound = true
							break
						}
					}
					if newToolsFound {
						break
					}
				}
			}

			// Check prompts if no new tools/resources found
			if !newToolsFound {
				allPrompts := tb.mcpClient.GetAllPrompts()
				for serverName, prompts := range allPrompts {
					for _, promptInfo := range prompts {
						mcpPromptTool := mcptool.NewMCPPromptTool(serverName, promptInfo.Name, tb)
						if !registeredTools[mcpPromptTool.Name()] {
							newToolsFound = true
							break
						}
					}
					if newToolsFound {
						break
					}
				}
			}

			// If new tools are detected, register them and update our tracking
			if newToolsFound {
				logger.Info("Detected new MCP tools/resources/prompts, registering...")
				tb.registerMCPTools()

				// Update registered tools tracking
				tools := tb.registry.List()
				for _, tool := range tools {
					if tool.Category() == interfaces.CategoryMCP {
						registeredTools[tool.Name()] = true
					}
				}
			}
		}
	}
}

// GetMCPClient returns the MCP client instance
func (tb *Toolbox) GetMCPClient() *mcp.MCPClient {
	return tb.mcpClient
}

// IsMCPEnabled returns whether MCP is enabled
func (tb *Toolbox) IsMCPEnabled() bool {
	return tb.config.EnableMCP && tb.mcpClient != nil
}

// GetMCPStatus returns the status of MCP functionality
func (tb *Toolbox) GetMCPStatus() map[string]interface{} {
	status := map[string]interface{}{
		"enabled": tb.IsMCPEnabled(),
		"client":  tb.mcpClient != nil,
	}

	if tb.mcpClient != nil {
		clientStatus := tb.mcpClient.Status()
		status["configured_servers"] = clientStatus.ConfiguredServers
		status["connected_servers"] = clientStatus.ConnectedServers
		status["available_tools"] = clientStatus.AvailableTools
		status["available_resources"] = clientStatus.AvailableResources
		status["available_prompts"] = clientStatus.AvailablePrompts
		status["last_error"] = clientStatus.LastError
		status["uptime"] = clientStatus.Uptime
	}

	return status
}

// resolveMCPToolByOriginalName resolves an MCP tool by its original name
// (without the mcp_<server>_ prefix). Returns the tool if exactly one match
// is found across all MCP servers. Returns nil, false if no match or multiple
// matches are found (to avoid ambiguity).
func (tb *Toolbox) resolveMCPToolByOriginalName(originalName string) (interfaces.Tool, bool) {
	if tb.mcpClient == nil {
		return nil, false
	}

	allTools := tb.mcpClient.GetAllTools()
	var matches []interfaces.Tool

	for serverName, tools := range allTools {
		for _, toolInfo := range tools {
			if toolInfo.Name == originalName {
				// Construct the registered name
				registeredName := fmt.Sprintf("mcp_%s_%s", serverName, toolInfo.Name)
				tool, exists := tb.registry.Get(registeredName)
				if exists {
					matches = append(matches, tool)
				}
			}
		}
	}

	// Only return if exactly one match found (avoid ambiguity)
	if len(matches) == 1 {
		logger.Debugf("Resolved MCP tool '%s' to registered name '%s'", originalName, matches[0].Name())
		return matches[0], true
	} else if len(matches) > 1 {
		logger.Warnf("Multiple MCP tools found with original name '%s', cannot resolve ambiguously", originalName)
	}

	return nil, false
}

// GetMCPToolByOriginalName returns information about an MCP tool by its original name.
// This is useful for the ProgressiveDisclosure system and tool_scheduler to check if
// a tool exists in the MCP registry even if it hasn't been expanded yet.
func (tb *Toolbox) GetMCPToolByOriginalName(originalName string) (serverName string, toolInfo *interfaces.Tool, exists bool) {
	if tb.mcpClient == nil {
		return "", nil, false
	}

	allTools := tb.mcpClient.GetAllTools()
	var foundServerName string
	var foundTool interfaces.Tool
	matchCount := 0

	for srvName, tools := range allTools {
		for _, tInfo := range tools {
			if tInfo.Name == originalName {
				registeredName := fmt.Sprintf("mcp_%s_%s", srvName, tInfo.Name)
				tool, regExists := tb.registry.Get(registeredName)
				if regExists {
					foundServerName = srvName
					foundTool = tool
					matchCount++
				}
			}
		}
	}

	if matchCount == 1 {
		return foundServerName, &foundTool, true
	}
	return "", nil, false
}

// ListMCPConnections returns information about MCP connections
func (tb *Toolbox) ListMCPConnections() []mcp.MCPConnectionInfo {
	if tb.mcpClient == nil {
		return nil
	}
	return tb.mcpClient.ListRemoteConnections()
}

// CallMCPTool calls a tool on a remote MCP server
func (tb *Toolbox) CallMCPTool(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (interface{}, error) {
	if tb.mcpClient == nil {
		return nil, fmt.Errorf("MCP not initialized")
	}
	return tb.mcpClient.CallRemoteTool(ctx, serverName, toolName, arguments)
}

// ReadMCPResource reads a resource from a remote MCP server
func (tb *Toolbox) ReadMCPResource(ctx context.Context, serverName, uri string) (interface{}, error) {
	if tb.mcpClient == nil {
		return nil, fmt.Errorf("MCP not initialized")
	}
	return tb.mcpClient.ReadRemoteResource(ctx, serverName, uri)
}

// GetMCPPrompt gets a prompt from a remote MCP server
func (tb *Toolbox) GetMCPPrompt(ctx context.Context, serverName, promptName string, arguments map[string]string) (interface{}, error) {
	if tb.mcpClient == nil {
		return nil, fmt.Errorf("MCP not initialized")
	}
	return tb.mcpClient.GetRemotePrompt(ctx, serverName, promptName, arguments)
}

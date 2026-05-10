// Package mcp implements the Model Context Protocol client
// Package mcp implements the Model Context Protocol client
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPClient manages connections to multiple MCP servers
type MCPClient struct { //nolint:revive
	config      *MCPConfig
	connections map[string]*MCPConnection
	mutex       sync.RWMutex
	started     bool

	// Enhanced features
	healthChecker *HealthChecker
	diagnostics   *Diagnostics

	// OAuth token store
	tokenStore *TokenStore

	// Reconnection channel
	reconnectChan chan string

	// Sandbox runtime for stdio MCP servers.
	sandboxRuntime    sandbox.Runtime
	sandboxWorkingDir string

	// Background context for long-lived goroutines (health checks, reconnect handler)
	bgCtx    context.Context
	bgCancel context.CancelFunc
}

// MCPConnection represents a connection to an MCP server
type MCPConnection struct { //nolint:revive
	name         string
	config       MCPServerConfig
	client       *mcp.Client
	session      *mcp.ClientSession
	transport    mcp.Transport
	tools        map[string]*mcp.Tool
	resources    map[string]*mcp.Resource
	prompts      map[string]*mcp.Prompt
	connected    bool
	lastActivity time.Time
	mutex        sync.RWMutex

	// Process monitoring for stdio transport
	process       *exec.Cmd
	processDone   chan error
	processCtx    context.Context
	processCancel context.CancelFunc
}

// NewMCPClient creates a new MCP client manager
func NewMCPClient(config *MCPConfig) *MCPClient {
	if config == nil {
		config = &MCPConfig{
			EnableClient:        true,
			DefaultTransport:    "stdio",
			Timeout:             30 * time.Second,
			MaxRetries:          3,
			EnableHealthCheck:   true,
			HealthCheckInterval: 30 * time.Second,
			HealthCheckTimeout:  10 * time.Second,
		}
	}

	client := &MCPClient{
		config:        config,
		connections:   make(map[string]*MCPConnection),
		reconnectChan: make(chan string, 100), // 缓冲通道避免阻塞
	}

	// Initialize token store (best effort - log error but don't fail)
	tokenStore, err := NewTokenStore("")
	if err != nil {
		logger.Debugf("Failed to initialize token store: %v", err)
	} else {
		client.tokenStore = tokenStore
	}

	// Initialize enhanced features
	client.healthChecker = NewHealthChecker(client)
	client.diagnostics = NewDiagnostics(client, client.healthChecker)

	return client
}

// SetSandboxRuntime configures process isolation for stdio MCP servers.
func (c *MCPClient) SetSandboxRuntime(runtime sandbox.Runtime, workingDir ...string) {
	if c == nil {
		return
	}
	c.sandboxRuntime = runtime
	if len(workingDir) > 0 {
		c.sandboxWorkingDir = workingDir[0]
	}
}

// Start initializes and connects to all configured MCP servers
func (c *MCPClient) Start(ctx context.Context) error { //nolint:revive
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.started {
		return fmt.Errorf("client already started")
	}

	logger.Info("Starting MCP client")

	// Check if config is available and has servers
	if c.config == nil {
		logger.Warn("MCP config is nil, creating default config")
		c.config = &MCPConfig{
			EnableClient:     true,
			DefaultTransport: "stdio",
			Timeout:          30 * time.Second,
			MaxRetries:       3,
		}
	}

	if c.config.MCPServers == nil {
		logger.Info("No MCP servers configured")
		c.started = true
		return nil
	}

	// 设置启动状态
	c.started = true
	logger.Info("MCP client marked as started, beginning async connections")

	// Create a background context for long-lived routines
	c.bgCtx, c.bgCancel = context.WithCancel(context.Background())

	// 异步连接所有服务器，避免阻塞主程序
	go func() {
		var wg sync.WaitGroup
		for _, serverConfig := range c.config.MCPServers {
			if !serverConfig.Enabled {
				logger.Infof("Skipping disabled MCP server: %s", serverConfig.Name)
				continue
			}

			wg.Add(1)
			go func(sc MCPServerConfig) {
				defer wg.Done()
				// Use bgCtx to avoid being bounded by short-lived startup context deadlines
				if err := c.connectToServer(c.bgCtx, sc); err != nil {
					logger.Infof("Failed to connect to MCP server %s: %v", sc.Name, err)
				} else {
					logger.Infof("Connected to MCP server: %s", sc.Name)
				}
			}(serverConfig)
		}
		wg.Wait()

		c.mutex.RLock()
		logger.Infof("MCP client async connection phase completed with %d active connections", len(c.connections))
		c.mutex.RUnlock()

		// Start health checker asynchronously (if enabled)
		if c.config.EnableHealthCheck {
			if err := c.healthChecker.Start(c.bgCtx); err != nil {
				logger.Debugf("Failed to start health checker: %v", err)
			}
			logger.Info("MCP health checking is enabled")
		}

		// 启动重连处理器（使用后台上下文，避免受启动ctx超时影响）
		go c.handleReconnectRequests(c.bgCtx)
	}()

	logger.Info("MCP client starting asynchronously")
	return nil
}

// connectToServer establishes a connection to a single MCP server
func (c *MCPClient) connectToServer(ctx context.Context, serverConfig MCPServerConfig) error {
	logger.Infof("Attempting to connect to MCP server: %s", serverConfig.Name)
	logger.Debugf("Server config: %+v", serverConfig)

	// Determine effective timeout (server > client > sensible default)
	effectiveTimeout := serverConfig.Timeout
	if effectiveTimeout <= 0 {
		effectiveTimeout = c.config.Timeout
	}
	if effectiveTimeout <= 0 {
		// Allow ample time for first-time installs (e.g., npx download)
		effectiveTimeout = 3 * time.Minute
	}
	ctxWithTimeout, cancel := context.WithTimeout(ctx, effectiveTimeout)
	defer cancel()
	logger.Debugf("Using effective timeout %s for server %s", effectiveTimeout, serverConfig.Name)

	// Create transport based on configuration
	transport, cmd, err := c.createTransport(ctxWithTimeout, serverConfig)
	if err != nil {
		logger.Debugf("Failed to create transport for server %s: %v", serverConfig.Name, err)
		return fmt.Errorf("failed to create transport: %w", err)
	}
	logger.Debugf("Transport created successfully for server %s", serverConfig.Name)

	// Create implementation
	impl := &mcp.Implementation{
		Name:    "nano-agent",
		Version: "1.0.0",
	}

	// Create client options
	opts := &mcp.ClientOptions{}

	// Create MCP client
	client := mcp.NewClient(impl, opts)
	logger.Debugf("MCP client created for server %s", serverConfig.Name)

	// Connect to server
	logger.Infof("Connecting to MCP server %s...", serverConfig.Name)
	session, err := client.Connect(ctxWithTimeout, transport, nil)
	if err != nil {
		logger.Infof("Failed to connect to MCP server %s: %v", serverConfig.Name, err)
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	logger.Infof("Successfully connected to MCP server %s", serverConfig.Name)

	// Create connection object
	connection := &MCPConnection{
		name:         serverConfig.Name,
		config:       serverConfig,
		client:       client,
		session:      session,
		transport:    transport,
		tools:        make(map[string]*mcp.Tool),
		resources:    make(map[string]*mcp.Resource),
		prompts:      make(map[string]*mcp.Prompt),
		connected:    true,
		lastActivity: time.Now(),
		process:      cmd,
	}

	// Setup process monitoring for stdio transport
	if TransportType(serverConfig.Transport) == TransportSTDIO {
		c.setupProcessMonitoring(connection, serverConfig)
	}

	// Discover server capabilities
	err = c.discoverCapabilities(ctxWithTimeout, connection)
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("failed to discover capabilities: %w", err)
	}

	// Store connection
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.connections[serverConfig.Name] = connection

	return nil
}

// createTransport creates the appropriate transport based on configuration
func (c *MCPClient) createTransport(ctx context.Context, config MCPServerConfig) (mcp.Transport, *exec.Cmd, error) {
	switch TransportType(config.Transport) {
	case TransportSTDIO:
		if len(config.Command) == 0 {
			return nil, nil, fmt.Errorf("command is required for stdio transport")
		}

		command := config.Command[0]
		args := append([]string(nil), config.Command[1:]...)
		if c.sandboxRuntime != nil {
			env, err := c.sandboxRuntime.PrepareCommand(ctx, sandbox.SandboxRequest{
				Command:    command,
				Args:       args,
				WorkingDir: c.sandboxWorkingDir,
				Metadata: map[string]interface{}{
					"tool":       "mcp_stdio_server",
					"mcp_server": config.Name,
					"transport":  string(TransportSTDIO),
				},
			})
			if err != nil {
				return nil, nil, err
			}
			command = env.Command
			args = env.Args
		}

		cmd := exec.Command(command, args...)
		// place child in its own process group so we can terminate group
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return &mcp.CommandTransport{Command: cmd}, cmd, nil

	case TransportType("sse"):
		return nil, nil, fmt.Errorf("SSE transport is no longer supported; please use 'streamable' (HTTP) or 'stdio'")

	case TransportStreamable:
		if config.URL == "" {
			return nil, nil, fmt.Errorf("URL is required for streamable transport")
		}

		// Build headers map
		headers := make(map[string]string, len(config.Headers)+1)
		for k, v := range config.Headers {
			headers[k] = v
		}

		// Inject OAuth token if configured
		if config.OAuth != nil && c.tokenStore != nil {
			oauthClient := NewOAuthClient(config.OAuth, c.tokenStore)
			entry, err := oauthClient.GetValidToken(ctx, config.Name)
			if err != nil {
				return nil, nil, fmt.Errorf("oauth required for server %q: run 'nano mcp auth %s' (%w)", config.Name, config.Name, err)
			}
			// Only set Authorization header if user hasn't already provided one
			if _, exists := headers["Authorization"]; !exists {
				tokenType := entry.TokenType
				if tokenType == "" {
					tokenType = "Bearer"
				}
				headers["Authorization"] = tokenType + " " + entry.AccessToken
			}
		}

		// Create HTTP client with custom headers
		httpClient := &http.Client{}
		if len(headers) > 0 {
			httpClient.Transport = &headerRoundTripper{
				headers: headers,
				base:    http.DefaultTransport,
			}
		}

		return &mcp.StreamableClientTransport{
			Endpoint:   config.URL,
			HTTPClient: httpClient,
			MaxRetries: 5,
		}, nil, nil

	case TransportInMemory:
		// This would typically be used for testing
		transport1, _ := mcp.NewInMemoryTransports()
		return transport1, nil, nil

	case TransportType("http"), TransportType("websocket"):
		return nil, nil, fmt.Errorf(
			"transport %q is no longer supported; use 'streamable' (HTTP) or 'stdio' instead. "+
				"Migrate your config: change `transport: %s` to `transport: streamable` and keep the `url` field.",
			config.Transport, config.Transport,
		)

	default:
		return nil, nil, fmt.Errorf("unsupported transport type: %s", config.Transport)
	}
}

// discoverCapabilities discovers tools, resources, and prompts from the server
func (c *MCPClient) discoverCapabilities(ctx context.Context, conn *MCPConnection) error {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()

	// List tools
	toolsResult, err := conn.session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		logger.Debugf("Failed to list tools from server %s: %v", conn.name, err)
	} else {
		for _, tool := range toolsResult.Tools {
			conn.tools[tool.Name] = tool
		}
		logger.Infof("Discovered %d tools from server %s", len(toolsResult.Tools), conn.name)
	}

	// List resources
	resourcesResult, err := conn.session.ListResources(ctx, &mcp.ListResourcesParams{})
	if err != nil {
		logger.Debugf("Failed to list resources from server %s: %v", conn.name, err)
	} else {
		for _, resource := range resourcesResult.Resources {
			conn.resources[resource.URI] = resource
		}
		logger.Infof("Discovered %d resources from server %s", len(resourcesResult.Resources), conn.name)
	}

	// List prompts
	promptsResult, err := conn.session.ListPrompts(ctx, &mcp.ListPromptsParams{})
	if err != nil {
		logger.Debugf("Failed to list prompts from server %s: %v", conn.name, err)
	} else {
		for _, prompt := range promptsResult.Prompts {
			conn.prompts[prompt.Name] = prompt
		}
		logger.Infof("Discovered %d prompts from server %s", len(promptsResult.Prompts), conn.name)
	}

	conn.lastActivity = time.Now()
	return nil
}

// Stop disconnects from all MCP servers
func (c *MCPClient) Stop() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.started {
		return fmt.Errorf("client not started")
	}

	logger.Info("Stopping MCP client")

	// Cancel background goroutines first
	if c.bgCancel != nil {
		c.bgCancel()
		c.bgCancel = nil
		c.bgCtx = nil
	}

	// Close all connections
	for name, conn := range c.connections {
		conn.mutex.Lock()
		if conn.connected {
			conn.connected = false
			if conn.session != nil {
				_ = conn.session.Close()
			}
		}

		// Stop process monitoring for stdio transport
		if conn.processCancel != nil {
			conn.processCancel()
		}

		// Terminate process if it's still running
		if conn.process != nil && conn.process.Process != nil {
			if conn.process.ProcessState == nil || !conn.process.ProcessState.Exited() {
				logger.Infof("Terminating process for server %s", name)
				pid := conn.process.Process.Pid
				// Try graceful TERM to the whole process group first
				_ = syscall.Kill(-pid, syscall.SIGTERM)
				// Wait briefly for graceful shutdown
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					if err := conn.process.Process.Signal(syscall.Signal(0)); err != nil {
						break // process is gone
					}
					time.Sleep(200 * time.Millisecond)
				}
				// Force kill if still alive
				if err := conn.process.Process.Signal(syscall.Signal(0)); err == nil {
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			}
		}

		conn.mutex.Unlock()
		logger.Infof("Closed connection to %s", name)
	}

	c.connections = make(map[string]*MCPConnection)
	c.started = false

	// Stop health checker
	if c.healthChecker != nil {
		if err := c.healthChecker.Stop(); err != nil {
			logger.Debugf("Failed to stop health checker: %v", err)
		}
	}

	logger.Info("MCP client stopped")
	return nil
}

// CallTool calls a tool on a specific MCP server
func (c *MCPClient) CallTool(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (*mcp.CallToolResult, error) {
	c.mutex.RLock()
	conn, exists := c.connections[serverName]
	c.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("server %s not connected", serverName)
	}

	conn.mutex.Lock()
	defer conn.mutex.Unlock()

	if !conn.connected {
		return nil, fmt.Errorf("server %s not connected", serverName)
	}

	// Check if tool exists
	if _, exists := conn.tools[toolName]; !exists {
		return nil, fmt.Errorf("tool %s not found on server %s", toolName, serverName)
	}

	logger.Infof("Calling tool %s on server %s", toolName, serverName)

	// Call the tool
	result, err := conn.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to call tool %s on server %s: %w", toolName, serverName, err)
	}

	conn.lastActivity = time.Now()
	return result, nil
}

// ReadResource reads a resource from a specific MCP server
func (c *MCPClient) ReadResource(ctx context.Context, serverName, uri string) (*mcp.ReadResourceResult, error) {
	c.mutex.RLock()
	conn, exists := c.connections[serverName]
	c.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("server %s not connected", serverName)
	}

	conn.mutex.Lock()
	defer conn.mutex.Unlock()

	if !conn.connected {
		return nil, fmt.Errorf("server %s not connected", serverName)
	}

	// Check if resource exists
	if _, exists := conn.resources[uri]; !exists {
		return nil, fmt.Errorf("resource %s not found on server %s", uri, serverName)
	}

	logger.Infof("Reading resource %s from server %s", uri, serverName)

	// Read the resource
	result, err := conn.session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: uri,
	})

	conn.lastActivity = time.Now()

	if err != nil {
		logger.Errorf("Resource read failed: %v", err)
		return nil, err
	}

	logger.Infof("Resource read successful: %s", uri)
	return result, nil
}

// GetPrompt gets a prompt from a specific MCP server
func (c *MCPClient) GetPrompt(ctx context.Context, serverName, promptName string, arguments map[string]string) (*mcp.GetPromptResult, error) {
	c.mutex.RLock()
	conn, exists := c.connections[serverName]
	c.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("server %s not connected", serverName)
	}

	conn.mutex.Lock()
	defer conn.mutex.Unlock()

	if !conn.connected {
		return nil, fmt.Errorf("server %s not connected", serverName)
	}

	// Check if prompt exists
	if _, exists := conn.prompts[promptName]; !exists {
		return nil, fmt.Errorf("prompt %s not found on server %s", promptName, serverName)
	}

	logger.Infof("Getting prompt %s from server %s", promptName, serverName)

	// Get the prompt
	result, err := conn.session.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      promptName,
		Arguments: arguments,
	})

	conn.lastActivity = time.Now()

	if err != nil {
		logger.Errorf("Prompt get failed: %v", err)
		return nil, err
	}

	logger.Infof("Prompt get successful: %s", promptName)
	return result, nil
}

// ListConnections returns information about all active connections
func (c *MCPClient) ListConnections() []MCPConnectionInfo {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var connections []MCPConnectionInfo
	for _, conn := range c.connections {
		conn.mutex.RLock()
		info := MCPConnectionInfo{
			Name:          conn.name,
			Connected:     conn.connected,
			ToolCount:     len(conn.tools),
			ResourceCount: len(conn.resources),
			PromptCount:   len(conn.prompts),
			LastActivity:  conn.lastActivity,
			Transport:     TransportType(conn.config.Transport),
		}
		conn.mutex.RUnlock()
		connections = append(connections, info)
	}

	return connections
}

// GetAllTools returns all available tools from all connected servers
func (c *MCPClient) GetAllTools() map[string][]MCPToolInfo {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	allTools := make(map[string][]MCPToolInfo)

	for serverName, conn := range c.connections {
		conn.mutex.RLock()
		var tools []MCPToolInfo
		for _, tool := range conn.tools {
			// Convert InputSchema to map[string]interface{}
			var inputSchema map[string]interface{}
			if tool.InputSchema != nil {
				// Handle the case where InputSchema is of type any
				// Try to marshal and unmarshal to convert to map
				if schemaBytes, err := json.Marshal(tool.InputSchema); err == nil {
					json.Unmarshal(schemaBytes, &inputSchema) //nolint:errcheck
				}
			}

			tools = append(tools, MCPToolInfo{
				Name:        tool.Name,
				Description: tool.Description,
				Server:      serverName,
				InputSchema: inputSchema,
			})
		}
		conn.mutex.RUnlock()
		allTools[serverName] = tools
	}

	return allTools
}

// GetAllResources returns all available resources from all connected servers
func (c *MCPClient) GetAllResources() map[string][]MCPResourceInfo {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	allResources := make(map[string][]MCPResourceInfo)

	for serverName, conn := range c.connections {
		conn.mutex.RLock()
		var resources []MCPResourceInfo
		for _, resource := range conn.resources {
			resources = append(resources, MCPResourceInfo{
				URI:         resource.URI,
				Name:        resource.Name,
				Description: resource.Description,
				Server:      serverName,
			})
		}
		conn.mutex.RUnlock()
		allResources[serverName] = resources
	}

	return allResources
}

// GetAllPrompts returns all available prompts from all connected servers
func (c *MCPClient) GetAllPrompts() map[string][]MCPPromptInfo {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	allPrompts := make(map[string][]MCPPromptInfo)

	for serverName, conn := range c.connections {
		conn.mutex.RLock()
		var prompts []MCPPromptInfo
		for _, prompt := range conn.prompts {
			prompts = append(prompts, MCPPromptInfo{
				Name:        prompt.Name,
				Description: prompt.Description,
				Server:      serverName,
			})
		}
		conn.mutex.RUnlock()
		allPrompts[serverName] = prompts
	}

	return allPrompts
}

// CallRemoteTool calls a tool on a specific MCP server
func (c *MCPClient) CallRemoteTool(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (interface{}, error) {
	c.mutex.RLock()
	conn, exists := c.connections[serverName]
	c.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("server %s not found or not connected", serverName)
	}

	return conn.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	})
}

// ReadRemoteResource reads a resource from a specific MCP server
func (c *MCPClient) ReadRemoteResource(ctx context.Context, serverName, uri string) (interface{}, error) {
	c.mutex.RLock()
	conn, exists := c.connections[serverName]
	c.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("server %s not found or not connected", serverName)
	}

	return conn.session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: uri,
	})
}

// GetRemotePrompt gets a prompt from a specific MCP server
func (c *MCPClient) GetRemotePrompt(ctx context.Context, serverName, promptName string, arguments map[string]string) (interface{}, error) {
	c.mutex.RLock()
	conn, exists := c.connections[serverName]
	c.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("server %s not found or not connected", serverName)
	}

	return conn.session.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      promptName,
		Arguments: arguments,
	})
}

// ListRemoteConnections returns information about all active connections
func (c *MCPClient) ListRemoteConnections() []MCPConnectionInfo {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var connections []MCPConnectionInfo
	for name, conn := range c.connections {
		conn.mutex.RLock()
		info := MCPConnectionInfo{
			Name:          name,
			Connected:     conn.connected,
			ToolCount:     len(conn.tools),
			ResourceCount: len(conn.resources),
			PromptCount:   len(conn.prompts),
			LastActivity:  conn.lastActivity,
		}
		conn.mutex.RUnlock()
		connections = append(connections, info)
	}

	return connections
}

// Status returns the current status of the MCP client
func (c *MCPClient) Status() struct {
	ConfiguredServers  int
	ConnectedServers   int
	AvailableTools     int
	AvailableResources int
	AvailablePrompts   int
	LastError          string
	Uptime             time.Duration
} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var totalTools, totalResources, totalPrompts int
	connected := 0

	for _, conn := range c.connections {
		conn.mutex.RLock()
		if conn.connected {
			connected++
			totalTools += len(conn.tools)
			totalResources += len(conn.resources)
			totalPrompts += len(conn.prompts)
		}
		conn.mutex.RUnlock()
	}

	configuredServers := 0
	if c.config != nil && c.config.MCPServers != nil {
		configuredServers = len(c.config.MCPServers)
	}

	return struct {
		ConfiguredServers  int
		ConnectedServers   int
		AvailableTools     int
		AvailableResources int
		AvailablePrompts   int
		LastError          string
		Uptime             time.Duration
	}{
		ConfiguredServers:  configuredServers,
		ConnectedServers:   connected,
		AvailableTools:     totalTools,
		AvailableResources: totalResources,
		AvailablePrompts:   totalPrompts,
	}
}

// GetHealthChecker returns the health checker instance
func (c *MCPClient) GetHealthChecker() *HealthChecker {
	return c.healthChecker
}

// GetDiagnostics returns the diagnostics instance
func (c *MCPClient) GetDiagnostics() *Diagnostics {
	return c.diagnostics
}

// GetHealthStatus returns current health status of all servers
func (c *MCPClient) GetHealthStatus() map[string]*HealthCheck {
	if c.healthChecker == nil {
		return make(map[string]*HealthCheck)
	}
	return c.healthChecker.GetHealthStatus()
}

// GenerateDiagnosticReport generates a comprehensive diagnostic report
func (c *MCPClient) GenerateDiagnosticReport(ctx context.Context) (*DiagnosticReport, error) {
	if c.diagnostics == nil {
		return nil, fmt.Errorf("diagnostics not initialized")
	}
	return c.diagnostics.GenerateReport(ctx)
}

// IsHealthy returns true if all connected servers are healthy
func (c *MCPClient) IsHealthy() bool {
	if c.healthChecker == nil {
		return true // No health checker means we can't detect issues
	}
	return c.healthChecker.IsHealthy()
}

// ReconnectServer attempts to reconnect to a specific MCP server
// ReconnectServer reconnects to an MCP server
func (c *MCPClient) ReconnectServer(_ context.Context, serverName string) error {
	c.mutex.Lock() // 忽略传入的 ctx，使用内部带超时的上下文，避免被外部已取消的 ctx 影响
	connectTimeout := c.config.Timeout
	if connectTimeout <= 0 {
		connectTimeout = 30 * time.Second
	}
	connectCtx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	// 查找服务器配置
	c.mutex.RLock()
	var serverConfig *MCPServerConfig
	for _, sc := range c.config.MCPServers {
		if sc.Name == serverName {
			configCopy := sc
			serverConfig = &configCopy
			break
		}
	}
	c.mutex.RUnlock()

	if serverConfig == nil {
		return fmt.Errorf("server config not found for %s", serverName)
	}

	if !serverConfig.Enabled {
		return fmt.Errorf("server %s is disabled", serverName)
	}

	logger.Infof("Attempting safe reconnection to MCP server: %s", serverName)

	// 先尝试建立新连接（不影响旧连接）
	transport, cmd, err := c.createTransport(connectCtx, *serverConfig)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

	impl := &mcp.Implementation{Name: "nano-agent", Version: "1.0.0"}
	opts := &mcp.ClientOptions{}
	client := mcp.NewClient(impl, opts)

	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}

	newConn := &MCPConnection{
		name:         serverConfig.Name,
		config:       *serverConfig,
		client:       client,
		session:      session,
		transport:    transport,
		tools:        make(map[string]*mcp.Tool),
		resources:    make(map[string]*mcp.Resource),
		prompts:      make(map[string]*mcp.Prompt),
		connected:    true,
		lastActivity: time.Now(),
		process:      cmd,
	}

	// 发现能力
	if err := c.discoverCapabilities(connectCtx, newConn); err != nil {
		// 失败则关闭新会话并返回错误，不影响旧连接
		_ = session.Close()
		return fmt.Errorf("failed to discover capabilities: %w", err)
	}

	// 为stdio重连后恢复进程监控
	if TransportType(serverConfig.Transport) == TransportSTDIO {
		c.setupProcessMonitoring(newConn, *serverConfig)
	}

	// 成功后原子替换连接
	c.mutex.Lock()
	if existingConn, exists := c.connections[serverName]; exists {
		existingConn.mutex.Lock()
		if existingConn.session != nil {
			_ = existingConn.session.Close()
		}
		existingConn.connected = false
		existingConn.mutex.Unlock()
	}
	c.connections[serverName] = newConn
	c.mutex.Unlock()

	// Extra diagnostics: log new stdio process PID and PGID for troubleshooting
	if TransportType(serverConfig.Transport) == TransportSTDIO && cmd != nil && cmd.Process != nil {
		pid := cmd.Process.Pid
		if pgid, err := syscall.Getpgid(pid); err == nil {
			logger.Infof("Reconnected server %s: stdio process pid=%d, pgid=%d", serverName, pid, pgid)
		} else {
			logger.Infof("Reconnected server %s: stdio process pid=%d (pgid unknown: %v)", serverName, pid, err)
		}
	}

	logger.Infof("Successfully reconnected to MCP server: %s", serverName)
	return nil
}

// handleReconnectRequests handles reconnection requests from the channel
func (c *MCPClient) handleReconnectRequests(ctx context.Context) {
	for {
		select {
		case serverName := <-c.reconnectChan:
			logger.Infof("Received reconnection request for server: %s", serverName)
			go func(name string) {
				if err := c.ReconnectServer(ctx, name); err != nil {
					logger.Debugf("Failed to reconnect to server %s: %v", name, err)
				}
			}(serverName)
		case <-ctx.Done():
			return
		}
	}
}

// RequestReconnect sends a reconnection request via channel
func (c *MCPClient) RequestReconnect(serverName string) {
	select {
	case c.reconnectChan <- serverName:
		logger.Debugf("Queued reconnection request for server: %s", serverName)
	default:
		logger.Debugf("Reconnect channel full, dropping request for server: %s", serverName)
	}
}

// MCPConnectionInfo contains information about an MCP connection
type MCPConnectionInfo struct { //nolint:revive
	Name          string        `json:"name"`
	Connected     bool          `json:"connected"`
	ToolCount     int           `json:"tool_count"`
	ResourceCount int           `json:"resource_count"`
	PromptCount   int           `json:"prompt_count"`
	LastActivity  time.Time     `json:"last_activity"`
	Transport     TransportType `json:"transport"`
}

type MCPToolInfo struct { //nolint:revive
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Server      string                 `json:"server"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

type MCPResourceInfo struct { //nolint:revive
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Server      string `json:"server"`
}

type MCPPromptInfo struct { //nolint:revive
	Name        string `json:"name"`
	Description string `json:"description"`
	Server      string `json:"server"`
}

// headerRoundTripper is a custom RoundTripper that adds headers to HTTP requests
type headerRoundTripper struct {
	headers map[string]string
	base    http.RoundTripper
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	req = req.Clone(req.Context())

	// Add custom headers
	for key, value := range h.headers {
		req.Header.Set(key, value)
	}

	return h.base.RoundTrip(req)
}

// setupProcessMonitoring sets up process monitoring for stdio transport
func (c *MCPClient) setupProcessMonitoring(conn *MCPConnection, config MCPServerConfig) {
	// Create process command for monitoring
	cmd := exec.Command(config.Command[0], config.Command[1:]...)
	processCtx, processCancel := context.WithCancel(c.bgCtx)

	conn.process = cmd
	conn.processCtx = processCtx
	conn.processCancel = processCancel
	conn.processDone = make(chan error, 1)

	// Start process monitoring goroutine
	go c.monitorProcess(conn)

	logger.Infof("Process monitoring setup for server %s", config.Name)
}

// monitorProcess monitors the stdio process and handles automatic restart
func (c *MCPClient) monitorProcess(conn *MCPConnection) {
	logger.Infof("Starting process monitoring for server %s", conn.name)

	for {
		select {
		case <-conn.processCtx.Done():
			logger.Infof("Process monitoring stopped for server %s", conn.name)
			return
		case err := <-conn.processDone:
			logger.Warnf("Process for server %s exited with error: %v", conn.name, err)

			// Mark connection as disconnected
			conn.mutex.Lock()
			conn.connected = false
			conn.mutex.Unlock()

			// Attempt automatic restart
			c.restartStdioProcess(conn)
			return
		default:
			// Check if process is still running
			if conn.process != nil && conn.process.Process != nil {
				// Check process state
				if conn.process.ProcessState != nil && conn.process.ProcessState.Exited() {
					logger.Warnf("Process for server %s has exited", conn.name)

					// Mark connection as disconnected
					conn.mutex.Lock()
					conn.connected = false
					conn.mutex.Unlock()

					// Attempt automatic restart
					c.restartStdioProcess(conn)
					return
				}
			}

			// Sleep before next check
			time.Sleep(5 * time.Second)
		}
	}
}

// restartStdioProcess attempts to restart a stdio process
func (c *MCPClient) restartStdioProcess(conn *MCPConnection) {
	logger.Infof("Attempting to restart stdio process for server %s", conn.name)

	// Wait a bit before restarting to avoid rapid restart loops
	time.Sleep(2 * time.Second)

	// Check if client is still running
	c.mutex.RLock()
	if !c.started {
		c.mutex.RUnlock()
		logger.Infof("Client is stopping, not restarting process for server %s", conn.name)
		return
	}
	c.mutex.RUnlock()

	// Attempt to reconnect using the existing reconnection mechanism
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.ReconnectServer(ctx, conn.name)
	if err != nil {
		logger.Errorf("Failed to restart stdio process for server %s: %v", conn.name, err)
		// Schedule another restart attempt after a delay
		go func() {
			time.Sleep(10 * time.Second)
			c.restartStdioProcess(conn)
		}()
	} else {
		logger.Infof("Successfully restarted stdio process for server %s", conn.name)
	}
}

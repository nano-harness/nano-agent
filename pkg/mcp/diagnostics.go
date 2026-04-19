package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// DiagnosticReport contains comprehensive MCP diagnostic information
type DiagnosticReport struct {
	Timestamp       time.Time               `json:"timestamp"`
	SystemInfo      SystemInfo              `json:"system_info"`
	MCPStatus       MCPStatus               `json:"mcp_status"`
	Connections     []ConnectionDiagnostic  `json:"connections"`
	Configuration   ConfigurationDiagnostic `json:"configuration"`
	Performance     PerformanceDiagnostic   `json:"performance"`
	Errors          []ErrorDiagnostic       `json:"errors"`
	Recommendations []string                `json:"recommendations"`
	HealthChecks    map[string]*HealthCheck `json:"health_checks,omitempty"`
}

// SystemInfo contains system information
type SystemInfo struct {
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	GoVersion     string `json:"go_version"`
	NumCPU        int    `json:"num_cpu"`
	NumGoroutines int    `json:"num_goroutines"`
	MemoryUsage   uint64 `json:"memory_usage_mb"`
}

// MCPStatus contains overall MCP status information
type MCPStatus struct { //nolint:revive
	Enabled           bool      `json:"enabled"`
	ClientInitialized bool      `json:"client_initialized"`
	TotalServers      int       `json:"total_servers"`
	ConnectedServers  int       `json:"connected_servers"`
	TotalTools        int       `json:"total_tools"`
	TotalResources    int       `json:"total_resources"`
	TotalPrompts      int       `json:"total_prompts"`
	LastActivity      time.Time `json:"last_activity"`
}

// ConnectionDiagnostic contains diagnostic information for a single connection
type ConnectionDiagnostic struct {
	Name           string                 `json:"name"`
	Transport      string                 `json:"transport"`
	Connected      bool                   `json:"connected"`
	LastActivity   time.Time              `json:"last_activity"`
	Tools          []ToolDiagnostic       `json:"tools"`
	Resources      []ResourceDiagnostic   `json:"resources"`
	Prompts        []PromptDiagnostic     `json:"prompts"`
	ConnectionTime time.Duration          `json:"connection_time"`
	Errors         []string               `json:"errors"`
	Configuration  map[string]interface{} `json:"configuration"`
}

// ToolDiagnostic contains diagnostic information for a tool
type ToolDiagnostic struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Available   bool       `json:"available"`
	LastUsed    *time.Time `json:"last_used,omitempty"`
	CallCount   int        `json:"call_count"`
}

// ResourceDiagnostic contains diagnostic information for a resource
type ResourceDiagnostic struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Available   bool   `json:"available"`
}

// PromptDiagnostic contains diagnostic information for a prompt
type PromptDiagnostic struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Available   bool   `json:"available"`
}

// ConfigurationDiagnostic contains configuration diagnostic information
type ConfigurationDiagnostic struct {
	ConfigSources    []string               `json:"config_sources"`
	EnabledFeatures  []string               `json:"enabled_features"`
	DefaultTransport string                 `json:"default_transport"`
	Timeout          time.Duration          `json:"timeout"`
	MaxRetries       int                    `json:"max_retries"`
	CustomSettings   map[string]interface{} `json:"custom_settings"`
}

// PerformanceDiagnostic contains performance metrics
type PerformanceDiagnostic struct {
	AverageResponseTime time.Duration `json:"average_response_time"`
	TotalToolCalls      int64         `json:"total_tool_calls"`
	FailedCalls         int64         `json:"failed_calls"`
	SuccessRate         float64       `json:"success_rate"`
	ConnectionCount     int           `json:"active_connections"`
}

// ErrorDiagnostic contains error information
type ErrorDiagnostic struct {
	Timestamp   time.Time `json:"timestamp"`
	ServerName  string    `json:"server_name"`
	ErrorType   string    `json:"error_type"`
	Message     string    `json:"message"`
	Context     string    `json:"context"`
	Recoverable bool      `json:"recoverable"`
}

// Diagnostics provides MCP diagnostic capabilities
type Diagnostics struct {
	client      *MCPClient
	healthCheck *HealthChecker
	errors      []ErrorDiagnostic
	metrics     *Metrics
}

// Metrics tracks MCP performance metrics
type Metrics struct {
	ToolCallCount      int64
	SuccessfulCalls    int64
	FailedCalls        int64
	TotalResponseTime  time.Duration
	ConnectionAttempts int64
	ReconnectionCount  int64
}

// NewDiagnostics creates a new diagnostics instance
func NewDiagnostics(client *MCPClient, healthCheck *HealthChecker) *Diagnostics {
	return &Diagnostics{
		client:      client,
		healthCheck: healthCheck,
		errors:      make([]ErrorDiagnostic, 0),
		metrics:     &Metrics{},
	}
}

// GenerateReport generates a comprehensive diagnostic report
func (d *Diagnostics) GenerateReport(ctx context.Context) (*DiagnosticReport, error) { //nolint:revive
	report := &DiagnosticReport{
		Timestamp: time.Now(),
	}

	// Gather system information
	report.SystemInfo = d.getSystemInfo()

	// Gather MCP status
	report.MCPStatus = d.getMCPStatus()

	// Gather connection diagnostics
	report.Connections = d.getConnectionDiagnostics()

	// Gather configuration diagnostics
	report.Configuration = d.getConfigurationDiagnostics()

	// Gather performance diagnostics
	report.Performance = d.getPerformanceDiagnostics()

	// Include recent errors
	report.Errors = d.getRecentErrors(10)

	// Add health checks if available
	if d.healthCheck != nil {
		report.HealthChecks = d.healthCheck.GetHealthStatus()
	}

	// Generate recommendations
	report.Recommendations = d.generateRecommendations(report)

	return report, nil
}

// getSystemInfo gathers system information
func (d *Diagnostics) getSystemInfo() SystemInfo {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)

	return SystemInfo{
		OS:            runtime.GOOS,
		Architecture:  runtime.GOARCH,
		GoVersion:     runtime.Version(),
		NumCPU:        runtime.NumCPU(),
		NumGoroutines: runtime.NumGoroutine(),
		MemoryUsage:   m.Alloc / 1024 / 1024, // Convert to MB
	}
}

// getMCPStatus gathers MCP status information
func (d *Diagnostics) getMCPStatus() MCPStatus {
	status := MCPStatus{
		Enabled:           d.client != nil,
		ClientInitialized: d.client != nil,
	}

	if d.client != nil {
		connections := d.client.ListConnections()
		clientStatus := d.client.Status()

		status.TotalServers = clientStatus.ConfiguredServers
		status.ConnectedServers = clientStatus.ConnectedServers
		status.TotalTools = clientStatus.AvailableTools
		status.TotalResources = clientStatus.AvailableResources
		status.TotalPrompts = clientStatus.AvailablePrompts

		// Find most recent activity
		for _, conn := range connections {
			if conn.LastActivity.After(status.LastActivity) {
				status.LastActivity = conn.LastActivity
			}
		}
	}

	return status
}

// getConnectionDiagnostics gathers connection diagnostic information
func (d *Diagnostics) getConnectionDiagnostics() []ConnectionDiagnostic {
	var diagnostics []ConnectionDiagnostic

	if d.client == nil {
		return diagnostics
	}

	connections := d.client.ListConnections()

	for _, conn := range connections {
		diagnostic := ConnectionDiagnostic{
			Name:         conn.Name,
			Transport:    string(conn.Transport),
			Connected:    conn.Connected,
			LastActivity: conn.LastActivity,
			Tools:        make([]ToolDiagnostic, 0),
			Resources:    make([]ResourceDiagnostic, 0),
			Prompts:      make([]PromptDiagnostic, 0),
		}

		// Get tools for this server
		allTools := d.client.GetAllTools()
		if serverTools, exists := allTools[conn.Name]; exists {
			for _, tool := range serverTools {
				diagnostic.Tools = append(diagnostic.Tools, ToolDiagnostic{
					Name:        tool.Name,
					Description: tool.Description,
					Available:   true,
				})
			}
		}

		// Get resources for this server
		allResources := d.client.GetAllResources()
		if serverResources, exists := allResources[conn.Name]; exists {
			for _, resource := range serverResources {
				diagnostic.Resources = append(diagnostic.Resources, ResourceDiagnostic{
					URI:         resource.URI,
					Name:        resource.Name,
					Description: resource.Description,
					Available:   true,
				})
			}
		}

		// Get prompts for this server
		allPrompts := d.client.GetAllPrompts()
		if serverPrompts, exists := allPrompts[conn.Name]; exists {
			for _, prompt := range serverPrompts {
				diagnostic.Prompts = append(diagnostic.Prompts, PromptDiagnostic{
					Name:        prompt.Name,
					Description: prompt.Description,
					Available:   true,
				})
			}
		}

		diagnostics = append(diagnostics, diagnostic)
	}

	return diagnostics
}

// getConfigurationDiagnostics gathers configuration diagnostic information
func (d *Diagnostics) getConfigurationDiagnostics() ConfigurationDiagnostic {
	diagnostic := ConfigurationDiagnostic{
		ConfigSources:   []string{"environment variables", "config files"},
		EnabledFeatures: []string{},
	}

	// This would be populated based on actual configuration
	diagnostic.EnabledFeatures = append(diagnostic.EnabledFeatures, "client_mode")

	if d.client != nil {
		diagnostic.DefaultTransport = "stdio"
		diagnostic.Timeout = 30 * time.Second
		diagnostic.MaxRetries = 3
	}

	return diagnostic
}

// getPerformanceDiagnostics gathers performance metrics
func (d *Diagnostics) getPerformanceDiagnostics() PerformanceDiagnostic {
	diagnostic := PerformanceDiagnostic{}

	if d.metrics != nil {
		diagnostic.TotalToolCalls = d.metrics.ToolCallCount
		diagnostic.FailedCalls = d.metrics.FailedCalls

		if d.metrics.ToolCallCount > 0 {
			diagnostic.SuccessRate = float64(d.metrics.SuccessfulCalls) / float64(d.metrics.ToolCallCount)
			diagnostic.AverageResponseTime = d.metrics.TotalResponseTime / time.Duration(d.metrics.ToolCallCount)
		}
	}

	if d.client != nil {
		diagnostic.ConnectionCount = len(d.client.ListConnections())
	}

	return diagnostic
}

// getRecentErrors returns recent errors
func (d *Diagnostics) getRecentErrors(limit int) []ErrorDiagnostic {
	if len(d.errors) <= limit {
		return d.errors
	}
	return d.errors[len(d.errors)-limit:]
}

// generateRecommendations generates recommendations based on the diagnostic report
func (d *Diagnostics) generateRecommendations(report *DiagnosticReport) []string {
	var recommendations []string

	// Check if MCP is enabled but no servers are connected
	if report.MCPStatus.Enabled && report.MCPStatus.ConnectedServers == 0 {
		recommendations = append(recommendations, "No MCP servers are connected. Check your MCP configuration and server availability.")
	}

	// Check for high memory usage
	if report.SystemInfo.MemoryUsage > 500 { // More than 500MB
		recommendations = append(recommendations, "High memory usage detected. Consider restarting the application if memory usage continues to grow.")
	}

	// Check for high error rate
	if report.Performance.SuccessRate < 0.9 && report.Performance.TotalToolCalls > 10 {
		recommendations = append(recommendations, "High error rate detected in tool calls. Check server connectivity and tool availability.")
	}

	// Check for stale connections
	for _, conn := range report.Connections {
		if conn.Connected && time.Since(conn.LastActivity) > time.Hour {
			recommendations = append(recommendations, fmt.Sprintf("Connection to '%s' has been inactive for over an hour. Consider checking server health.", conn.Name))
		}
	}

	// Check health status
	if report.HealthChecks != nil {
		unhealthyCount := 0
		for _, health := range report.HealthChecks {
			if health.Status == HealthStatusUnhealthy {
				unhealthyCount++
			}
		}

		if unhealthyCount > 0 {
			recommendations = append(recommendations, fmt.Sprintf("%d MCP servers are unhealthy. Check server logs and configuration.", unhealthyCount))
		}
	}

	// Performance recommendations
	if report.Performance.AverageResponseTime > 5*time.Second {
		recommendations = append(recommendations, "Average response time is high. Consider checking network connectivity and server performance.")
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "All systems appear to be functioning normally.")
	}

	return recommendations
}

// RecordError records an error for diagnostics
func (d *Diagnostics) RecordError(serverName, errorType, message, context string) {
	error := ErrorDiagnostic{ //nolint:revive
		Timestamp:   time.Now(),
		ServerName:  serverName,
		ErrorType:   errorType,
		Message:     message,
		Context:     context,
		Recoverable: d.isRecoverableError(errorType),
	}

	d.errors = append(d.errors, error)

	// Keep only last 100 errors
	if len(d.errors) > 100 {
		d.errors = d.errors[1:]
	}

	// Update metrics
	if d.metrics != nil {
		d.metrics.FailedCalls++
	}

	logger.Errorf("MCP Error [%s:%s]: %s", serverName, errorType, message)
}

// RecordSuccess records a successful operation
func (d *Diagnostics) RecordSuccess(responseTime time.Duration) {
	if d.metrics != nil {
		d.metrics.ToolCallCount++
		d.metrics.SuccessfulCalls++
		d.metrics.TotalResponseTime += responseTime
	}
}

// isRecoverableError determines if an error type is recoverable
func (d *Diagnostics) isRecoverableError(errorType string) bool {
	recoverableErrors := []string{
		"connection_timeout",
		"temporary_failure",
		"rate_limit",
		"server_busy",
	}

	for _, recoverable := range recoverableErrors {
		if strings.Contains(strings.ToLower(errorType), recoverable) {
			return true
		}
	}

	return false
}

// ExportReport exports the diagnostic report to JSON
func (d *Diagnostics) ExportReport(report *DiagnosticReport) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal diagnostic report: %w", err)
	}
	return string(data), nil
}

// PrintSummary prints a human-readable summary of the diagnostic report
func (d *Diagnostics) PrintSummary(report *DiagnosticReport) string {
	var summary strings.Builder

	summary.WriteString("=== MCP Diagnostic Summary ===\n")
	fmt.Fprintf(&summary, "Timestamp: %s\n", report.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&summary, "System: %s/%s (Go %s)\n", report.SystemInfo.OS, report.SystemInfo.Architecture, report.SystemInfo.GoVersion)
	fmt.Fprintf(&summary, "Memory Usage: %d MB\n", report.SystemInfo.MemoryUsage)
	summary.WriteString("\n")

	summary.WriteString("MCP Status:\n")
	fmt.Fprintf(&summary, "  Enabled: %v\n", report.MCPStatus.Enabled)
	fmt.Fprintf(&summary, "  Connected Servers: %d/%d\n", report.MCPStatus.ConnectedServers, report.MCPStatus.TotalServers)
	fmt.Fprintf(&summary, "  Available Tools: %d\n", report.MCPStatus.TotalTools)
	fmt.Fprintf(&summary, "  Available Resources: %d\n", report.MCPStatus.TotalResources)
	fmt.Fprintf(&summary, "  Available Prompts: %d\n", report.MCPStatus.TotalPrompts)
	summary.WriteString("\n")

	if len(report.Connections) > 0 {
		summary.WriteString("Connections:\n")
		for _, conn := range report.Connections {
			status := "✓"
			if !conn.Connected {
				status = "✗"
			}
			fmt.Fprintf(&summary, "  %s %s (%s): %d tools, %d resources, %d prompts\n",
				status, conn.Name, conn.Transport, len(conn.Tools), len(conn.Resources), len(conn.Prompts))
		}
		summary.WriteString("\n")
	}

	if report.Performance.TotalToolCalls > 0 {
		summary.WriteString("Performance:\n")
		fmt.Fprintf(&summary, "  Tool Calls: %d (%.1f%% success rate)\n",
			report.Performance.TotalToolCalls, report.Performance.SuccessRate*100)
		fmt.Fprintf(&summary, "  Average Response Time: %v\n", report.Performance.AverageResponseTime)
		summary.WriteString("\n")
	}

	if len(report.Errors) > 0 {
		summary.WriteString("Recent Errors:\n")
		for i, err := range report.Errors {
			if i >= 5 { // Show only last 5 errors in summary
				break
			}
			fmt.Fprintf(&summary, "  [%s] %s: %s\n", err.Timestamp.Format("15:04:05"), err.ServerName, err.Message)
		}
		summary.WriteString("\n")
	}

	if len(report.Recommendations) > 0 {
		summary.WriteString("Recommendations:\n")
		for _, rec := range report.Recommendations {
			fmt.Fprintf(&summary, "  • %s\n", rec)
		}
	}

	return summary.String()
}

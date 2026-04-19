package mcp

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HealthStatus represents the health status of an MCP connection
type HealthStatus string

const (
	HealthStatusHealthy      HealthStatus = "healthy" //nolint:revive
	HealthStatusDegraded     HealthStatus = "degraded"
	HealthStatusUnhealthy    HealthStatus = "unhealthy"
	HealthStatusDisconnected HealthStatus = "disconnected"
)

// HealthCheck represents a health check result
type HealthCheck struct {
	ServerName   string        `json:"server_name"`
	Status       HealthStatus  `json:"status"`
	LastCheck    time.Time     `json:"last_check"`
	ResponseTime time.Duration `json:"response_time"`
	Error        string        `json:"error,omitempty"`
	Consecutive  int           `json:"consecutive_failures"`
	Uptime       time.Duration `json:"uptime"`
	TotalChecks  int64         `json:"total_checks"`
	FailureCount int64         `json:"failure_count"`
}

// HealthChecker monitors MCP server health
type HealthChecker struct {
	client       *MCPClient
	interval     time.Duration
	timeout      time.Duration
	maxFailures  int
	healthChecks map[string]*HealthCheck
	mutex        sync.RWMutex
	stopCh       chan struct{}
	running      bool
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(client *MCPClient) *HealthChecker {
	interval := 30 * time.Second
	timeout := 10 * time.Second

	if client.config != nil {
		if client.config.HealthCheckInterval > 0 {
			interval = client.config.HealthCheckInterval
		}
		if client.config.HealthCheckTimeout > 0 {
			timeout = client.config.HealthCheckTimeout
		}
	}

	return &HealthChecker{
		client:       client,
		interval:     interval,
		timeout:      timeout,
		maxFailures:  3, // Max 3 consecutive failures before marking unhealthy
		healthChecks: make(map[string]*HealthCheck),
		stopCh:       make(chan struct{}),
	}
}

// Start begins health checking
func (hc *HealthChecker) Start(ctx context.Context) error {
	hc.mutex.Lock()
	defer hc.mutex.Unlock()

	if hc.running {
		return fmt.Errorf("health checker already running")
	}

	hc.running = true

	// Initialize health checks for all connections
	connections := hc.client.ListConnections()
	for _, conn := range connections {
		hc.healthChecks[conn.Name] = &HealthCheck{
			ServerName:   conn.Name,
			Status:       HealthStatusHealthy,
			LastCheck:    time.Now(),
			TotalChecks:  0,
			FailureCount: 0,
		}
	}

	// Start monitoring goroutine
	go hc.monitor(ctx)

	logger.Info("MCP health checker started")
	return nil
}

// Stop stops health checking
func (hc *HealthChecker) Stop() error {
	hc.mutex.Lock()
	defer hc.mutex.Unlock()

	if !hc.running {
		return fmt.Errorf("health checker not running")
	}

	close(hc.stopCh)
	hc.running = false

	logger.Info("MCP health checker stopped")
	return nil
}

// monitor runs the health check loop
func (hc *HealthChecker) monitor(ctx context.Context) {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hc.performHealthChecks(ctx)
		case <-hc.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// performHealthChecks performs health checks on all connections
func (hc *HealthChecker) performHealthChecks(ctx context.Context) {
	connections := hc.client.ListConnections()
	for _, conn := range connections {
		go hc.checkConnection(ctx, conn.Name)
	}
}

// checkConnection performs a health check on a specific connection
func (hc *HealthChecker) checkConnection(ctx context.Context, serverName string) {
	startTime := time.Now()

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, hc.timeout)
	defer cancel()

	hc.mutex.Lock()
	healthCheck, exists := hc.healthChecks[serverName]
	if !exists {
		healthCheck = &HealthCheck{
			ServerName:   serverName,
			Status:       HealthStatusHealthy,
			TotalChecks:  0,
			FailureCount: 0,
		}
		hc.healthChecks[serverName] = healthCheck
	}
	hc.mutex.Unlock()

	// Perform the actual health check
	err := hc.pingServer(checkCtx, serverName)

	// Additional check for stdio transport process status
	if err == nil {
		err = hc.checkProcessStatus(serverName)
	}

	responseTime := time.Since(startTime)

	// Update health check result
	hc.mutex.Lock()
	defer hc.mutex.Unlock()

	healthCheck.LastCheck = time.Now()
	healthCheck.ResponseTime = responseTime
	healthCheck.TotalChecks++

	if err != nil {
		healthCheck.Error = err.Error()
		healthCheck.Consecutive++
		healthCheck.FailureCount++

		// Update status based on consecutive failures
		if healthCheck.Consecutive >= hc.maxFailures {
			healthCheck.Status = HealthStatusUnhealthy
		} else {
			healthCheck.Status = HealthStatusDegraded
		}

		logger.Debugf("MCP server %s health check failed (%d/%d): %v",
			serverName, healthCheck.Consecutive, hc.maxFailures, err)

		// Try to reconnect if server is unhealthy
		if healthCheck.Status == HealthStatusUnhealthy {
			go hc.attemptReconnect(context.Background(), serverName)
		}
	} else {
		healthCheck.Error = ""
		healthCheck.Consecutive = 0

		// Determine status based on response time
		if responseTime > hc.timeout/2 {
			healthCheck.Status = HealthStatusDegraded
		} else {
			healthCheck.Status = HealthStatusHealthy
		}

		if healthCheck.Status == HealthStatusHealthy {
			logger.Debugf("MCP server %s is healthy (response time: %v)", serverName, responseTime)
		}
	}
}

// pingServer performs a simple ping to check server responsiveness
func (hc *HealthChecker) pingServer(ctx context.Context, serverName string) error {
	// 通过客户端的安全方法获取连接状态
	connections := hc.client.ListConnections()

	var session *mcp.ClientSession
	var connected bool

	for _, conn := range connections {
		if conn.Name == serverName {
			connected = conn.Connected
			break
		}
	}

	if !connected {
		return fmt.Errorf("server not connected")
	}

	// 通过客户端的安全方法获取会话
	c := hc.client
	c.mutex.RLock()
	mcpConn, exists := c.connections[serverName]
	if exists {
		mcpConn.mutex.RLock()
		session = mcpConn.session
		mcpConn.mutex.RUnlock()
	}
	c.mutex.RUnlock()

	if session == nil {
		return fmt.Errorf("no active session")
	}

	// 使用带超时的上下文来避免阻塞
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// 执行轻量级操作：列出工具来测试响应性
	_, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// 成功获取响应
	return nil
}

// attemptReconnect attempts to reconnect to an unhealthy server
func (hc *HealthChecker) attemptReconnect(ctx context.Context, serverName string) { //nolint:revive
	logger.Infof("Attempting to reconnect to unhealthy MCP server: %s", serverName)

	// 使用channel方式发送重连请求，避免直接调用
	hc.client.RequestReconnect(serverName)
	logger.Infof("Reconnection request queued for server: %s", serverName)
}

// GetHealthStatus returns the current health status of all servers
func (hc *HealthChecker) GetHealthStatus() map[string]*HealthCheck {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()

	result := make(map[string]*HealthCheck)
	for name, check := range hc.healthChecks {
		// Create a copy to avoid data races
		result[name] = &HealthCheck{
			ServerName:   check.ServerName,
			Status:       check.Status,
			LastCheck:    check.LastCheck,
			ResponseTime: check.ResponseTime,
			Error:        check.Error,
			Consecutive:  check.Consecutive,
			Uptime:       check.Uptime,
			TotalChecks:  check.TotalChecks,
			FailureCount: check.FailureCount,
		}
	}

	return result
}

// GetServerHealth returns health status for a specific server
func (hc *HealthChecker) GetServerHealth(serverName string) (*HealthCheck, error) {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()

	check, exists := hc.healthChecks[serverName]
	if !exists {
		return nil, fmt.Errorf("server %s not found", serverName)
	}

	// Return a copy
	return &HealthCheck{
		ServerName:   check.ServerName,
		Status:       check.Status,
		LastCheck:    check.LastCheck,
		ResponseTime: check.ResponseTime,
		Error:        check.Error,
		Consecutive:  check.Consecutive,
		Uptime:       check.Uptime,
		TotalChecks:  check.TotalChecks,
		FailureCount: check.FailureCount,
	}, nil
}

// IsHealthy returns true if all servers are healthy
func (hc *HealthChecker) IsHealthy() bool {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()

	for _, check := range hc.healthChecks {
		if check.Status == HealthStatusUnhealthy {
			return false
		}
	}

	return true
}

// checkProcessStatus checks if the stdio process is still running
func (hc *HealthChecker) checkProcessStatus(serverName string) error {
	hc.client.mutex.RLock()
	conn, exists := hc.client.connections[serverName]
	hc.client.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("connection not found")
	}

	conn.mutex.RLock()
	defer conn.mutex.RUnlock()

	// Only check process status for stdio transport
	if conn.config.Transport != "stdio" {
		return nil
	}

	// Check if process is still running
	if conn.process != nil && conn.process.Process != nil {
		if conn.process.ProcessState != nil && conn.process.ProcessState.Exited() {
			return fmt.Errorf("stdio process has exited")
		}
		// Probe with signal 0 to detect zombie or defunct states quickly
		if err := conn.process.Process.Signal(syscall.Signal(0)); err != nil {
			return fmt.Errorf("stdio process not alive: %w", err)
		}
	}

	return nil
}

// GetOverallStatus returns an overall health summary
func (hc *HealthChecker) GetOverallStatus() map[string]interface{} {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()

	totalServers := len(hc.healthChecks)
	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0
	disconnectedCount := 0

	for _, check := range hc.healthChecks {
		switch check.Status {
		case HealthStatusHealthy:
			healthyCount++
		case HealthStatusDegraded:
			degradedCount++
		case HealthStatusUnhealthy:
			unhealthyCount++
		case HealthStatusDisconnected:
			disconnectedCount++
		}
	}

	overallStatus := "healthy"
	if unhealthyCount > 0 {
		overallStatus = "unhealthy"
	} else if degradedCount > 0 {
		overallStatus = "degraded"
	}

	return map[string]interface{}{
		"overall_status":     overallStatus,
		"total_servers":      totalServers,
		"healthy_count":      healthyCount,
		"degraded_count":     degradedCount,
		"unhealthy_count":    unhealthyCount,
		"disconnected_count": disconnectedCount,
		"last_check":         time.Now(),
		"monitoring_enabled": hc.running,
	}
}

package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/mcp"
)

// probeMCPServer establishes a one-off connection to a configured MCP server
// and returns its connection info on success. It is the shared backend for
// "nano mcp servers start" and "nano mcp servers restart"; both commands
// surface a successful probe as evidence that the configured command,
// environment, and transport are valid.
//
// The caller's context controls cancellation; if it has no deadline a
// default per-server timeout is applied.
func probeMCPServer(ctx context.Context, serverName string) (*mcp.MCPConnectionInfo, error) {
	cfg := config.Get()
	if cfg == nil || cfg.MCP == nil || !cfg.MCP.EnableClient {
		return nil, fmt.Errorf("MCP is not enabled in config")
	}

	var target *config.MCPServerConfig
	for i := range cfg.MCP.Servers {
		if cfg.MCP.Servers[i].Name == serverName {
			target = &cfg.MCP.Servers[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("MCP server not found in config: %s", serverName)
	}

	// Force-enable the entry for the probe so users can validate a server
	// they have just configured but not yet enabled. The original Enabled
	// flag in cfg.MCP.Servers is unchanged.
	probeCfg := *target
	probeCfg.Enabled = true

	timeout := cfg.MCP.Timeout
	if probeCfg.Timeout > 0 {
		timeout = probeCfg.Timeout
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	// Apply the timeout if the caller did not provide one.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	clientCfg := &mcp.MCPConfig{
		EnableClient:        true,
		MCPServers:          convertMCPServers([]config.MCPServerConfig{probeCfg}),
		DefaultTransport:    cfg.MCP.DefaultTransport,
		Timeout:             cfg.MCP.Timeout,
		MaxRetries:          cfg.MCP.MaxRetries,
		EnableHealthCheck:   false,
		HealthCheckInterval: 0,
		HealthCheckTimeout:  0,
	}
	client := mcp.NewMCPClient(clientCfg)

	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("connection probe failed: %w", err)
	}
	defer func() { _ = client.Stop() }()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		for _, conn := range client.ListConnections() {
			if conn.Name != serverName {
				continue
			}
			if conn.Connected {
				return &conn, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("connection probe timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

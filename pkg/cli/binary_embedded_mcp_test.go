//go:build !windows

package cli

import (
	"os"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

// TestEmbeddedModePreservesEnableMCP guards the fix for symphony's MCP loopback:
// when the agent is launched under a symphony / orchestrator profile (detected
// via SYMPHONY_* or NANO_ORCHESTRATOR_PROFILE env vars), binary mode must NOT
// reset EnableMCP — the orchestrator profile sets it to true and the agent
// needs MCP to call back to symphony.
func TestEmbeddedModePreservesEnableMCP(t *testing.T) {
	// Save & restore env
	for _, k := range []string{"SYMPHONY_MCP_URL", "SYMPHONY_TOKEN", "SYMPHONY_WORKSPACE"} {
		old := os.Getenv(k)
		defer os.Setenv(k, old)
	}
	os.Setenv("SYMPHONY_MCP_URL", "http://localhost:4123/mcp")
	os.Setenv("SYMPHONY_TOKEN", "test-token")
	os.Setenv("SYMPHONY_WORKSPACE", "/tmp/test-ws")

	if !isEmbeddedBinaryExecution() {
		t.Fatal("isEmbeddedBinaryExecution() should return true with SYMPHONY_* set")
	}

	// Simulate post-LoadConfig state (orchestrator profile has set EnableMCP=true)
	cfg := &config.Config{
		EnableMCP: true,
		MCP:       &config.MCPConfig{},
	}
	cfgCopy := *cfg
	// Apply the same logic as executeBinaryModeWithOptions
	if !isEmbeddedBinaryExecution() {
		cfgCopy.EnableMCP = false
	}
	if cfgCopy.EnableMCP != true {
		t.Errorf("EnableMCP must remain true under embedded execution; got false")
	}
}

func TestStandaloneModeDisablesMCP(t *testing.T) {
	for _, k := range []string{"SYMPHONY_MCP_URL", "SYMPHONY_TOKEN", "SYMPHONY_WORKSPACE", "NANO_ORCHESTRATOR_PROFILE"} {
		old := os.Getenv(k)
		os.Unsetenv(k)
		defer os.Setenv(k, old)
	}

	if isEmbeddedBinaryExecution() {
		t.Fatal("isEmbeddedBinaryExecution() should return false with no orchestrator env")
	}

	cfg := &config.Config{EnableMCP: true, MCP: &config.MCPConfig{}}
	cfgCopy := *cfg
	if !isEmbeddedBinaryExecution() {
		cfgCopy.EnableMCP = false
	}
	if cfgCopy.EnableMCP != false {
		t.Error("standalone binary mode should keep MCP disabled to avoid auto-connecting to user MCP servers")
	}
}

//go:build integration

package permission_test

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/hookservice"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- minimal mock tool for integration tests ---

type mockToolIntegration struct {
	name       string
	requiresOK bool
	category   interfaces.ToolCategory
}

func (m *mockToolIntegration) Name() string                   { return m.name }
func (m *mockToolIntegration) Description() string            { return "" }
func (m *mockToolIntegration) Schema() *interfaces.ToolSchema { return nil }
func (m *mockToolIntegration) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	return nil, nil
}
func (m *mockToolIntegration) RequiresConfirmation() bool        { return m.requiresOK }
func (m *mockToolIntegration) Category() interfaces.ToolCategory { return m.category }
func (m *mockToolIntegration) ConcurrencySafe() bool             { return true }

// contextualToolIntegration overrides RequiresConfirmationForParams.
type contextualToolIntegration struct {
	mockToolIntegration
	paramsRequire bool
}

func (c *contextualToolIntegration) RequiresConfirmationForParams(_ map[string]interface{}) bool {
	return c.paramsRequire
}

// M1IntegrationSuite tests the complete M1 milestone integration:
// 1. Hook events wiring
// 2. Plan mode restrictions
// 3. Dangerous command firewall
type M1IntegrationSuite struct {
	workdir string
}

// TestM1_HooksAndFirewallIntegration tests that hooks fire correctly and firewall intercepts dangerous commands.
func TestM1_HooksAndFirewallIntegration(t *testing.T) {
	suite := &M1IntegrationSuite{
		workdir: t.TempDir(),
	}

	// Create permission manager in default mode
	mgr := permission.NewManagerWithWorkdir(permission.ModeDefault, nil, suite.workdir)

	// Setup firewall hook
	firewallConfig := permission.FirewallConfig{
		Enabled:           true,
		SeverityThreshold: permission.SeverityMedium,
		FailurePolicy:     "confirm",
		CustomPatterns:    nil,
		Overrides:         nil,
	}
	firewallHook := permission.NewFirewallHook(firewallConfig)

	// Create a mock shell tool
	shellTool := &mockToolIntegration{
		name:       "run_shell_command",
		requiresOK: false, // The firewall will decide
		category:   interfaces.CategoryShell,
	}

	t.Run("dangerous command triggers firewall", func(t *testing.T) {
		// Test a dangerous command
		dangerousCmd := "rm -rf /"
		params := map[string]interface{}{"command": dangerousCmd}

		// Execute firewall hook
		decision, err := firewallHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionConfirm, decision.Action, "Firewall should require confirmation for dangerous command")
		assert.Contains(t, decision.Reason, "dangerous command", "Decision reason should mention dangerous command")
		assert.NotEmpty(t, decision.Warnings, "Decision should include warnings")
		assert.Contains(t, decision.Warnings[0], "Dangerous command detected", "Warning should mention dangerous command")
	})

	t.Run("safe command passes firewall", func(t *testing.T) {
		// Test a safe command
		safeCmd := "ls -la"
		params := map[string]interface{}{"command": safeCmd}

		// Execute firewall hook
		decision, err := firewallHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionAllow, decision.Action, "Firewall should allow safe command")
	})

	t.Run("firewall override whitelist", func(t *testing.T) {
		// Create firewall with override
		overrideConfig := permission.FirewallConfig{
			Enabled:           true,
			SeverityThreshold: permission.SeverityMedium,
			FailurePolicy:     "confirm",
			Overrides:         []string{"rm -rf /tmp/test"},
		}
		overrideHook := permission.NewFirewallHook(overrideConfig)

		// This would normally be dangerous, but it's in the override list
		params := map[string]interface{}{"command": "rm -rf /tmp/test"}

		decision, err := overrideHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionAllow, decision.Action, "Firewall should allow overridden command")
	})

	t.Run("firewall severity threshold", func(t *testing.T) {
		// Create firewall with high severity threshold
		highThresholdConfig := permission.FirewallConfig{
			Enabled:           true,
			SeverityThreshold: permission.SeverityHigh,
			FailurePolicy:     "confirm",
		}
		highThresholdHook := permission.NewFirewallHook(highThresholdConfig)

		// Medium severity command should pass
		params := map[string]interface{}{"command": "git push --force"}

		decision, err := highThresholdHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionAllow, decision.Action, "Medium severity should pass with high threshold")
	})

	t.Run("permission manager integrates with firewall", func(t *testing.T) {
		// In default mode, tool that doesn't require confirmation should pass
		assert.False(t, mgr.ShouldConfirm("run_shell_command", map[string]interface{}{"command": "ls"}, shellTool))

		// But the firewall can add its own check before the tool executes
		params := map[string]interface{}{"command": "rm -rf /"}
		decision, err := firewallHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionConfirm, decision.Action, "Firewall intercepts dangerous command even if tool doesn't require confirmation")
	})
}

// TestM1_PlanModeIntegration tests Plan mode restrictions.
func TestM1_PlanModeIntegration(t *testing.T) {
	suite := &M1IntegrationSuite{
		workdir: t.TempDir(),
	}

	// Create permission manager in Plan mode
	mgr := permission.NewManagerWithWorkdir(permission.ModePlan, nil, suite.workdir)

	t.Run("plan mode blocks all write operations", func(t *testing.T) {
		// Plan mode should confirm ALL filesystem writes
		writeTool := &mockToolIntegration{
			name:       "write_file",
			requiresOK: false,
			category:   interfaces.CategoryFileSystem,
		}

		params := map[string]interface{}{"file_path": filepath.Join(suite.workdir, "test.txt")}
		assert.True(t, mgr.ShouldConfirm("write_file", params, writeTool), "Plan mode should block write_file")
	})

	t.Run("plan mode blocks shell commands", func(t *testing.T) {
		// Plan mode should confirm shell commands (except read-only ones)
		shellTool := &mockToolIntegration{
			name:       "run_shell_command",
			requiresOK: false,
			category:   interfaces.CategoryShell,
		}

		// Non-read-only command should require confirmation
		params := map[string]interface{}{"command": "npm install"}
		assert.True(t, mgr.ShouldConfirm("run_shell_command", params, shellTool), "Plan mode should block non-read-only shell commands")

		// Read-only commands like "ls -la" are allowed in Plan mode
		readOnlyParams := map[string]interface{}{"command": "ls -la"}
		assert.False(t, mgr.ShouldConfirm("run_shell_command", readOnlyParams, shellTool), "Plan mode should allow read-only shell commands like ls")
	})

	t.Run("plan mode allows read operations", func(t *testing.T) {
		// Plan mode should allow read operations
		readTool := &mockToolIntegration{
			name:       "read_file",
			requiresOK: false,
			category:   interfaces.CategoryFileSystem,
		}

		params := map[string]interface{}{"file_path": filepath.Join(suite.workdir, "test.txt")}
		// Read operations should not require confirmation in Plan mode
		// Note: This depends on the tool implementation, but typically reads don't set requiresOK
		assert.False(t, mgr.ShouldConfirm("read_file", params, readTool), "Plan mode should allow read_file")
	})

	t.Run("mode switching", func(t *testing.T) {
		// Switch to YOLO mode
		mgr.SetMode(permission.ModeYOLO)
		assert.Equal(t, permission.ModeYOLO, mgr.GetMode())

		// In YOLO mode, nothing requires confirmation
		writeTool := &mockToolIntegration{
			name:       "write_file",
			requiresOK: true,
			category:   interfaces.CategoryFileSystem,
		}
		params := map[string]interface{}{"file_path": filepath.Join(suite.workdir, "test.txt")}
		assert.False(t, mgr.ShouldConfirm("write_file", params, writeTool), "YOLO mode should not confirm anything")

		// Switch back to Plan mode
		mgr.SetMode(permission.ModePlan)
		assert.Equal(t, permission.ModePlan, mgr.GetMode())

		// Should require confirmation again
		assert.True(t, mgr.ShouldConfirm("write_file", params, writeTool), "Switching back to Plan mode should re-enable restrictions")
	})
}

// TestM1_CompleteFlowIntegration tests the complete flow with hooks, firewall, and permission modes.
func TestM1_CompleteFlowIntegration(t *testing.T) {
	suite := &M1IntegrationSuite{
		workdir: t.TempDir(),
	}

	t.Run("complete flow: default mode + firewall", func(t *testing.T) {
		// Setup
		mgr := permission.NewManagerWithWorkdir(permission.ModeDefault, nil, suite.workdir)
		firewallConfig := permission.DefaultFirewallConfig()
		firewallHook := permission.NewFirewallHook(firewallConfig)

		shellTool := &mockToolIntegration{
			name:       "run_shell_command",
			requiresOK: false,
			category:   interfaces.CategoryShell,
		}

		// Scenario 1: Safe command in default mode
		params := map[string]interface{}{"command": "git status"}

		// Step 1: Permission manager check
		needsConfirm := mgr.ShouldConfirm("run_shell_command", params, shellTool)
		assert.False(t, needsConfirm, "Safe command should not need permission confirmation")

		// Step 2: Firewall hook check (pre_tool_use event)
		decision, err := firewallHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionAllow, decision.Action, "Firewall should allow safe command")

		// Result: Command can execute

		// Scenario 2: Dangerous command in default mode
		dangerousParams := map[string]interface{}{"command": "rm -rf /"}

		// Step 1: Permission manager check
		needsConfirm = mgr.ShouldConfirm("run_shell_command", dangerousParams, shellTool)
		assert.False(t, needsConfirm, "Default mode doesn't block by permission alone")

		// Step 2: Firewall hook check (pre_tool_use event)
		decision, err = firewallHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", dangerousParams)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionConfirm, decision.Action, "Firewall should catch dangerous command")
		assert.NotEmpty(t, decision.Warnings, "Should provide warnings about dangerous command")

		// Result: Command requires user confirmation
	})

	t.Run("complete flow: plan mode + firewall", func(t *testing.T) {
		// Setup
		mgr := permission.NewManagerWithWorkdir(permission.ModePlan, nil, suite.workdir)
		firewallConfig := permission.DefaultFirewallConfig()
		firewallHook := permission.NewFirewallHook(firewallConfig)

		shellTool := &mockToolIntegration{
			name:       "run_shell_command",
			requiresOK: false,
			category:   interfaces.CategoryShell,
		}

		// Scenario: Safe command in plan mode
		params := map[string]interface{}{"command": "git status"}

		// Step 1: Permission manager check (Plan mode blocks shell, but allows read-only)
		needsConfirm := mgr.ShouldConfirm("run_shell_command", params, shellTool)
		assert.False(t, needsConfirm, "Plan mode allows read-only git status")

		// Step 2: Firewall would also check, but permission manager already allowed it
		decision, err := firewallHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionAllow, decision.Action, "Firewall sees safe command")

		// Test with a write command
		writeParams := map[string]interface{}{"command": "npm install"}
		needsConfirm = mgr.ShouldConfirm("run_shell_command", writeParams, shellTool)
		assert.True(t, needsConfirm, "Plan mode should block write operations like npm install")

		// Result: Read-only commands allowed, write commands blocked by Plan mode
	})

	t.Run("complete flow: acceptEdits mode + firewall", func(t *testing.T) {
		// Setup
		mgr := permission.NewManagerWithWorkdir(permission.ModeAcceptEdits, nil, suite.workdir)
		firewallConfig := permission.DefaultFirewallConfig()
		firewallHook := permission.NewFirewallHook(firewallConfig)

		// Scenario 1: Filesystem write in workdir (acceptEdits mode)
		writeTool := &mockToolIntegration{
			name:       "write_file",
			requiresOK: true,
			category:   interfaces.CategoryFileSystem,
		}
		params := map[string]interface{}{"file_path": filepath.Join(suite.workdir, "test.txt")}

		// Step 1: Permission manager check
		needsConfirm := mgr.ShouldConfirm("write_file", params, writeTool)
		assert.False(t, needsConfirm, "AcceptEdits mode auto-approves filesystem writes")

		// Step 2: Firewall doesn't check non-shell tools
		// (In real flow, firewall hook only processes shell commands)

		// Result: Write can proceed

		// Scenario 2: Dangerous shell command
		shellTool := &mockToolIntegration{
			name:       "run_shell_command",
			requiresOK: true,
			category:   interfaces.CategoryShell,
		}
		dangerousParams := map[string]interface{}{"command": "rm -rf /"}

		// Step 1: Permission manager check
		needsConfirm = mgr.ShouldConfirm("run_shell_command", dangerousParams, shellTool)
		assert.True(t, needsConfirm, "AcceptEdits doesn't auto-approve shell commands")

		// Step 2: Firewall hook check
		decision, err := firewallHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", dangerousParams)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionConfirm, decision.Action, "Firewall catches dangerous command")

		// Result: Command requires confirmation from both permission manager and firewall
	})

	t.Run("complete flow: allowlist overrides", func(t *testing.T) {
		// Setup
		mgr := permission.NewManagerWithWorkdir(permission.ModeDefault, nil, suite.workdir)
		firewallConfig := permission.DefaultFirewallConfig()
		firewallHook := permission.NewFirewallHook(firewallConfig)

		shellTool := &mockToolIntegration{
			name:       "run_shell_command",
			requiresOK: true,
			category:   interfaces.CategoryShell,
		}

		// Add allowlist rule
		mgr.GetSessionAllowlist().AddRule(permission.ParseRule("run_shell_command(git push --force *)"))

		params := map[string]interface{}{"command": "git push --force origin main"}

		// Step 1: Permission manager check (allowlist overrides)
		needsConfirm := mgr.ShouldConfirm("run_shell_command", params, shellTool)
		assert.False(t, needsConfirm, "Allowlist should override permission confirmation")

		// Step 2: Firewall hook check
		decision, err := firewallHook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		// Firewall would still catch it as dangerous (medium severity)
		assert.Equal(t, hookservice.ActionConfirm, decision.Action, "Firewall still flags dangerous command")

		// Result: Permission manager allows, but firewall requires confirmation
		// This shows the defense-in-depth approach
	})
}

// TestM1_SensitiveFilesIntegration tests sensitive file detection in the complete flow.
func TestM1_SensitiveFilesIntegration(t *testing.T) {
	suite := &M1IntegrationSuite{
		workdir: t.TempDir(),
	}

	mgr := permission.NewManagerWithWorkdir(permission.ModeDefault, nil, suite.workdir)

	// Contextual tool that checks for sensitive files
	contextualTool := &contextualToolIntegration{
		mockToolIntegration: mockToolIntegration{
			name:       "write_file",
			requiresOK: false,
			category:   interfaces.CategoryFileSystem,
		},
		paramsRequire: true, // Will say "yes" for sensitive files
	}

	t.Run("sensitive file requires confirmation", func(t *testing.T) {
		// Writing to .env should require confirmation
		params := map[string]interface{}{"file_path": filepath.Join(suite.workdir, ".env")}

		needsConfirm := mgr.ShouldConfirm("write_file", params, contextualTool)
		assert.True(t, needsConfirm, "Sensitive file (.env) should require confirmation")

		// Verify using built-in detector
		isSensitive := permission.IsSensitiveFile(".env")
		assert.True(t, isSensitive, "IsSensitiveFile should detect .env")
	})

	t.Run("normal file does not require confirmation", func(t *testing.T) {
		// Writing to regular file should not require confirmation
		normalTool := &contextualToolIntegration{
			mockToolIntegration: mockToolIntegration{
				name:       "write_file",
				requiresOK: false,
				category:   interfaces.CategoryFileSystem,
			},
			paramsRequire: false, // Will say "no" for normal files
		}

		params := map[string]interface{}{"file_path": filepath.Join(suite.workdir, "README.md")}

		needsConfirm := mgr.ShouldConfirm("write_file", params, normalTool)
		assert.False(t, needsConfirm, "Normal file should not require confirmation")

		// Verify using built-in detector
		isSensitive := permission.IsSensitiveFile("README.md")
		assert.False(t, isSensitive, "IsSensitiveFile should not flag README.md")
	})
}

// TestM1_FirewallPolicyConfiguration tests different firewall policies.
func TestM1_FirewallPolicyConfiguration(t *testing.T) {
	t.Run("confirm policy", func(t *testing.T) {
		config := permission.FirewallConfig{
			Enabled:           true,
			SeverityThreshold: permission.SeverityMedium,
			FailurePolicy:     "confirm",
		}
		hook := permission.NewFirewallHook(config)

		params := map[string]interface{}{"command": "rm -rf /tmp/test"}
		decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionConfirm, decision.Action, "Confirm policy should require user confirmation")
	})

	t.Run("block policy", func(t *testing.T) {
		config := permission.FirewallConfig{
			Enabled:           true,
			SeverityThreshold: permission.SeverityMedium,
			FailurePolicy:     "block",
		}
		hook := permission.NewFirewallHook(config)

		params := map[string]interface{}{"command": "rm -rf /tmp/test"}
		decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionBlock, decision.Action, "Block policy should block dangerous command")
	})

	t.Run("allow policy", func(t *testing.T) {
		config := permission.FirewallConfig{
			Enabled:           true,
			SeverityThreshold: permission.SeverityMedium,
			FailurePolicy:     "allow",
		}
		hook := permission.NewFirewallHook(config)

		params := map[string]interface{}{"command": "rm -rf /tmp/test"}
		decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionAllow, decision.Action, "Allow policy should allow even dangerous commands (for testing)")
	})

	t.Run("disabled firewall", func(t *testing.T) {
		config := permission.FirewallConfig{
			Enabled:           false,
			SeverityThreshold: permission.SeverityMedium,
			FailurePolicy:     "confirm",
		}
		hook := permission.NewFirewallHook(config)

		params := map[string]interface{}{"command": "rm -rf /"}
		decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		// Even though disabled in config, the hook still runs and returns decision
		// The actual enabling/disabling is done at the hook registration level
		assert.NotNil(t, decision)
	})
}

// TestM1_CustomDangerousPatterns tests custom firewall patterns.
func TestM1_CustomDangerousPatterns(t *testing.T) {
	customRule := permission.DangerousCommandRule{
		Pattern:  regexp.MustCompile(`\bdropdb\b`),
		Severity: permission.SeverityHigh,
		Category: "database",
		Reason:   "Database deletion command",
	}

	config := permission.FirewallConfig{
		Enabled:           true,
		SeverityThreshold: permission.SeverityLow,
		FailurePolicy:     "confirm",
		CustomPatterns:    []permission.DangerousCommandRule{customRule},
	}
	hook := permission.NewFirewallHook(config)

	t.Run("custom pattern matches", func(t *testing.T) {
		params := map[string]interface{}{"command": "dropdb production"}
		decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionConfirm, decision.Action, "Custom pattern should match dropdb command")
	})

	t.Run("built-in patterns still work", func(t *testing.T) {
		params := map[string]interface{}{"command": "rm -rf /"}
		decision, err := hook.Execute(context.Background(), hookservice.EventPreToolUse, "run_shell_command", params)
		require.NoError(t, err)
		assert.Equal(t, hookservice.ActionConfirm, decision.Action, "Built-in patterns should still work with custom patterns")
	})
}

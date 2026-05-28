package cli

import (
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// LogPermissionResolution logs the resolved permission configuration for audit purposes.
// This provides a single, consistent audit trail across all entry points showing:
// - Which entry point initiated the agent
// - What permission mode was resolved and from what source
// - What confirm policy is in effect (for daemon-aware modes)
// - What sandbox backend is configured (if any)
// - Any warnings that were generated during resolution
//
// Example output:
//
//	[INFO] Permission resolved [entry=tui.tview mode=yolo source=flag:dangerously-skip-permissions sandbox=docker]
//	[INFO] Permission resolved [entry=binary mode=auto source=env:NANO_PERMISSION_MODE confirm_policy=block]
func LogPermissionResolution(entry string, res PermissionResolution, warnings []string) {
	msg := fmt.Sprintf("Permission resolved [entry=%s mode=%s source=%s", entry, res.Mode, res.Source)

	if res.ConfirmPolicy != "" {
		msg += fmt.Sprintf(" confirm_policy=%s", res.ConfirmPolicy)
	}

	if res.SandboxBackend != "" {
		msg += fmt.Sprintf(" sandbox=%s", res.SandboxBackend)
	}

	msg += "]"

	logger.Info(msg)

	// Log any warnings that were generated
	for _, warn := range warnings {
		logger.Warnf("Permission resolution: %s", warn)
	}
}

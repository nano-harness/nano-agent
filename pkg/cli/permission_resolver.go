package cli

import (
	"os"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// PermissionResolveOpts holds inputs for permission resolution.
type PermissionResolveOpts struct {
	// SkipPerms is true when --dangerously-skip-permissions flag is set (highest priority).
	SkipPerms bool
	// FlagMode is the --permission-mode flag value (second highest priority).
	FlagMode string
	// EnvHintEnabled controls whether environment variable resolution is enabled.
	// Set to false for daemon client mode where server-side policy applies.
	EnvHintEnabled bool
}

// PermissionResolution holds the resolved permission configuration and metadata.
type PermissionResolution struct {
	// Mode is the resolved permission mode.
	Mode permission.PermissionMode
	// ConfirmPolicy is the resolved daemon confirm policy (may be modified by auto mode).
	ConfirmPolicy config.ConfirmPolicy
	// SandboxBackend is the resolved sandbox backend (may be set by yolo mode).
	SandboxBackend string
	// Source indicates which source was used for resolution (flag/env/cfg/default).
	Source string
}

// ResolvePermission resolves permission mode from all sources (flags, env, config)
// following the priority chain: SkipPerms flag → FlagMode flag → NANO_PERMISSION_MODE env → cfg → default.
// It applies side effects to cfg in-place and returns the resolution result plus any warnings.
//
// Side effects:
//   - yolo mode with no sandbox backend → sets cfg.Sandbox.Backend = "docker"
//   - auto mode without PermissionAuto/AllowedRules config → emits diagnostic log
func ResolvePermission(cfg *config.Config, opts PermissionResolveOpts) (PermissionResolution, []string) {
	var warnings []string
	res := PermissionResolution{}

	// Priority 1: --dangerously-skip-permissions flag (always wins)
	if opts.SkipPerms {
		cfg.PermissionMode = string(permission.ModeYOLO)
		res.Mode = permission.ModeYOLO
		res.Source = "flag:dangerously-skip-permissions"
	} else if opts.FlagMode != "" {
		// Priority 2: --permission-mode flag
		cfg.PermissionMode = opts.FlagMode
		res.Mode = permission.PermissionMode(opts.FlagMode)
		res.Source = "flag:permission-mode"
	} else if opts.EnvHintEnabled {
		// Priority 3: environment variable NANO_PERMISSION_MODE
		if nanoMode := os.Getenv("NANO_PERMISSION_MODE"); nanoMode != "" {
			cfg.PermissionMode = nanoMode
			res.Mode = permission.PermissionMode(nanoMode)
			res.Source = "env:NANO_PERMISSION_MODE"
		}
	}

	// Priority 4: config file value (if not already set by higher priority)
	if res.Source == "" {
		if cfg.PermissionMode != "" {
			res.Mode = permission.PermissionMode(cfg.PermissionMode)
			res.Source = "config"
		} else {
			// Priority 5: default value
			cfg.PermissionMode = string(permission.ModeDefault)
			res.Mode = permission.ModeDefault
			res.Source = "default"
		}
	}

	// Side effect 1: yolo mode auto-enables docker sandbox if not configured
	if res.Mode == permission.ModeYOLO {
		if cfg.Sandbox == nil {
			cfg.Sandbox = &config.SandboxConfig{}
		}
		if cfg.Sandbox.Backend == "" {
			cfg.Sandbox.Enabled = true
			cfg.Sandbox.Backend = "docker"
			logger.Infof("YOLO permission mode selected; defaulting sandbox backend to docker")
		}
		res.SandboxBackend = cfg.Sandbox.Backend
	} else if cfg.Sandbox != nil {
		res.SandboxBackend = cfg.Sandbox.Backend
	}

	// Diagnostic: warn if auto mode has no permission/allow configuration.
	if res.Mode == permission.ModeAuto && cfg.PermissionAuto == nil && len(cfg.AllowedRules) == 0 {
		var confirmPolicy config.ConfirmPolicy
		if cfg.Daemon != nil {
			confirmPolicy = cfg.Daemon.ConfirmPolicy
		}
		logger.Infof("permission_mode=auto selected but no permissions/permission_auto block configured; "+
			"auto mode behaves like default. Daemon.ConfirmPolicy=%q controls headless fallback.",
			confirmPolicy)
	}

	// Capture the resolved confirm policy
	if cfg.Daemon != nil {
		res.ConfirmPolicy = cfg.Daemon.ConfirmPolicy
	}

	return res, warnings
}

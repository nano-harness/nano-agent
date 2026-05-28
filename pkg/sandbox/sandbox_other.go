//go:build !linux && !darwin

package sandbox

import (
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// SandboxExecSandbox is a stub for non-macOS platforms.
type SandboxExecSandbox struct {
	cfg        *config.SandboxConfig
	workingDir string
}

// NewSandboxExecSandbox is a stub for non-macOS platforms.
func NewSandboxExecSandbox(cfg *config.SandboxConfig, workingDir string) *SandboxExecSandbox {
	return &SandboxExecSandbox{cfg: cfg, workingDir: workingDir}
}

// BuildProfileForInspection is a stub for non-macOS platforms.
func (s *SandboxExecSandbox) BuildProfileForInspection() string {
	return "# sandbox-exec profiles are only available on macOS\n"
}

// newBwrapSandbox is a stub for non-Linux platforms.
func newBwrapSandbox(cfg *config.SandboxConfig, workingDir string) Sandbox { //nolint:unused
	logger.Warnf("sandbox: bwrap is only supported on Linux – falling back to no isolation")
	return &NoopSandbox{}
}

// newSandboxExecSandbox is a stub for non-macOS platforms.
func newSandboxExecSandbox(cfg *config.SandboxConfig, workingDir string) Sandbox { //nolint:unused
	logger.Warnf("sandbox: sandbox-exec is only supported on macOS – falling back to no isolation")
	return &NoopSandbox{}
}

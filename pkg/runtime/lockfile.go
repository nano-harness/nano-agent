// Package runtime provides runtime state management for nano-agent.
// This includes preventing simultaneous TUI/Daemon execution via lockfile mechanism.
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

const (
	// ModeTUI represents TUI mode
	ModeTUI = "tui"
	// ModeDaemon represents Daemon mode
	ModeDaemon = "daemon"
)

// LockFile manages runtime mode locking to prevent simultaneous TUI/Daemon execution.
// Uses a lockfile approach similar to daemon.pid but for TUI mode.
type LockFile struct {
	path string
	mode string
}

// NewLockFile creates a new LockFile for the specified mode.
// The lockfile path defaults to ~/.nano/tui.lock for TUI mode.
func NewLockFile(mode string) (*LockFile, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".nano")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	lockPath := filepath.Join(configDir, fmt.Sprintf("%s.lock", mode))
	return &LockFile{
		path: lockPath,
		mode: mode,
	}, nil
}

// Acquire attempts to acquire the lock for this mode.
// Returns an error if another mode is already running.
func (lf *LockFile) Acquire() error {
	// Check if the other mode is running
	otherMode := ModeDaemon
	if lf.mode == ModeDaemon {
		otherMode = ModeTUI
	}

	otherLock, err := NewLockFile(otherMode)
	if err != nil {
		return fmt.Errorf("failed to create lock for other mode: %w", err)
	}

	if otherLock.IsLocked() {
		return fmt.Errorf("%s mode is already running (lock file: %s). Cannot start %s mode simultaneously.\n"+
			"Please stop %s mode first or use the already-running mode",
			otherMode, otherLock.path, lf.mode, otherMode)
	}

	// Write our PID to the lockfile
	pid := os.Getpid()
	if err := os.WriteFile(lf.path, []byte(fmt.Sprintf("%d\n", pid)), 0644); err != nil {
		return fmt.Errorf("failed to write lock file: %w", err)
	}

	logger.Debugf("Acquired %s mode lock (pid: %d, path: %s)", lf.mode, pid, lf.path)
	return nil
}

// Release removes the lock file for this mode.
func (lf *LockFile) Release() error {
	if err := os.Remove(lf.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}
	logger.Debugf("Released %s mode lock (path: %s)", lf.mode, lf.path)
	return nil
}

// IsLocked checks if this mode's lockfile exists and the process is still running.
// Returns true if locked, false otherwise.
func (lf *LockFile) IsLocked() bool {
	data, err := os.ReadFile(lf.path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		logger.Warnf("Failed to read lock file %s: %v", lf.path, err)
		return false
	}

	// Parse PID from lockfile
	pidStr := string(data)
	pid, err := strconv.Atoi(pidStr[:len(pidStr)-1]) // Strip trailing newline
	if err != nil {
		logger.Warnf("Invalid PID in lock file %s: %v", lf.path, err)
		// Remove invalid lockfile
		_ = os.Remove(lf.path)
		return false
	}

	// Check if process is still running
	if err := syscall.Kill(pid, 0); err == nil {
		return true
	}

	// Process is dead, remove stale lockfile
	logger.Debugf("Removing stale lock file %s (pid %d not running)", lf.path, pid)
	_ = os.Remove(lf.path)
	return false
}

// GetPID returns the PID stored in the lockfile, or 0 if not locked.
func (lf *LockFile) GetPID() int {
	data, err := os.ReadFile(lf.path)
	if err != nil {
		return 0
	}

	pidStr := string(data)
	pid, err := strconv.Atoi(pidStr[:len(pidStr)-1])
	if err != nil {
		return 0
	}

	return pid
}

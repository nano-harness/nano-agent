package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	pkgruntime "github.com/nano-harness/nano-agent/pkg/runtime"
)

// Manager handles daemon process management
type Manager struct {
	config     *config.DaemonConfig
	pidFile    string
	configFile string
}

// NewManager creates a new daemon manager
func NewManager() *Manager {
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".nano")

	return &Manager{
		config: &config.DaemonConfig{
			Port:       8080,
			Host:       "127.0.0.1",
			PidFile:    filepath.Join(configDir, "daemon.pid"),
			LogFile:    filepath.Join(configDir, "daemon.log"),
			EnableCORS: true,
			APIKey:     "",
		},
		pidFile:    filepath.Join(configDir, "daemon.pid"),
		configFile: filepath.Join(configDir, "daemon.json"),
	}
}

func (m *Manager) normalizePaths() {
	if m == nil || m.config == nil {
		return
	}

	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".nano")

	if strings.TrimSpace(m.config.PidFile) == "" {
		m.config.PidFile = filepath.Join(configDir, "daemon.pid")
	}
	if strings.TrimSpace(m.config.LogFile) == "" {
		m.config.LogFile = filepath.Join(configDir, "daemon.log")
	}

	if runtime.GOOS == "darwin" {
		if strings.Contains(m.config.PidFile, "/home/ubuntu") {
			m.config.PidFile = filepath.Join(configDir, filepath.Base(m.config.PidFile))
		}
		if strings.Contains(m.config.LogFile, "/home/ubuntu") {
			m.config.LogFile = filepath.Join(configDir, filepath.Base(m.config.LogFile))
		}
	}

	m.pidFile = m.config.PidFile
}

// LoadConfig loads daemon configuration from main config file
func (m *Manager) LoadConfig() error {
	// First try to load from main config file
	mainConfig := config.Get()
	if mainConfig != nil && mainConfig.Daemon != nil {
		m.config = mainConfig.Daemon
		m.normalizePaths()
		return nil
	}

	// Fallback: try to load from separate daemon.json file for backward compatibility
	if _, err := os.Stat(m.configFile); os.IsNotExist(err) {
		// Create default config
		m.normalizePaths()
		return m.SaveConfig()
	}

	data, err := os.ReadFile(m.configFile)
	if err != nil {
		return fmt.Errorf("failed to read daemon config: %w", err)
	}

	if err := json.Unmarshal(data, m.config); err != nil {
		return err
	}

	m.normalizePaths()

	return nil
}

// SaveConfig saves daemon configuration
func (m *Manager) SaveConfig() error {
	configDir := filepath.Dir(m.configFile)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal daemon config: %w", err)
	}

	return os.WriteFile(m.configFile, data, 0644)
}

// Start starts the daemon process
func (m *Manager) Start(background bool) error {
	// Check if TUI mode is running
	lock, err := pkgruntime.NewLockFile(pkgruntime.ModeDaemon)
	if err != nil {
		return fmt.Errorf("failed to create daemon lock: %w", err)
	}
	if err := lock.Acquire(); err != nil {
		return fmt.Errorf("cannot start daemon: %w", err)
	}
	// Note: Lock is NOT released here because daemon continues running as background process.
	// The lock will be released when daemon stops via the Stop() method or process termination.

	// Check if already running
	if m.IsRunning() {
		return fmt.Errorf("daemon is already running")
	}

	if background {
		return m.startBackground()
	}

	return m.startForeground()
}

// startBackground starts daemon as background process
func (m *Manager) startBackground() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	programArgs := []string{"daemon", "foreground"}
	if v := strings.TrimSpace(os.Getenv("NANO_CONFIG_FILE")); v != "" {
		programArgs = append(programArgs, "--config", v)
	} else if _, err := os.Stat(filepath.Join(".nano", "nano.yaml")); err == nil {
		programArgs = append(programArgs, "--config", filepath.Join(".nano", "nano.yaml"))
	}

	useGoRun := strings.Contains(executable, "/go-build") || strings.Contains(executable, "\\go-build")
	var cmd *exec.Cmd
	if useGoRun {
		cmd = exec.Command("go", append([]string{"run", "./cmd/nano"}, programArgs...)...)
	} else {
		cmd = exec.Command(executable, programArgs...)
	}
	cmd.Dir, _ = os.Getwd()
	cmd.Env = append(os.Environ(),
		"NANO_DAEMON_CONSOLE_LOG=false",
		fmt.Sprintf("NANO_DAEMON_PORT=%d", m.config.Port),
		fmt.Sprintf("NANO_DAEMON_HOST=%s", m.config.Host),
		fmt.Sprintf("NANO_DAEMON_PID_FILE=%s", m.config.PidFile),
		fmt.Sprintf("NANO_DAEMON_LOG_FILE=%s", m.config.LogFile),
	)
	if strings.TrimSpace(m.config.APIKey) != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("NANO_DAEMON_API_KEY=%s", m.config.APIKey))
	}

	// Setup logging
	if m.config.LogFile != "" {
		logDir := filepath.Dir(m.config.LogFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			logger.Warnf("Failed to create log directory: %v", err)
		}

		logFile, err := os.OpenFile(m.config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			logger.Warnf("Failed to open log file: %v", err)
		} else {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
	} else {
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0644)
		if err == nil {
			cmd.Stdout = devNull
			cmd.Stderr = devNull
		}
	}

	// Start process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	startDeadline := time.Now().Add(20 * time.Second)
	for !m.IsRunning() && time.Now().Before(startDeadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if !m.IsRunning() {
		return fmt.Errorf("daemon process failed to start")
	}

	logger.Infof("Daemon started with PID %d", cmd.Process.Pid)
	return nil
}

// startForeground starts daemon in foreground (internal use)
func (m *Manager) startForeground() error {
	return fmt.Errorf("foreground mode should be handled by daemon server directly")
}

// Stop stops the daemon process
func (m *Manager) Stop() error {
	pid, err := m.getPID()
	if err != nil {
		return fmt.Errorf("daemon not running or PID file not found: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	// Send SIGTERM for graceful shutdown
	logger.Infof("Sending SIGTERM to daemon process %d", pid)
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	waitDeadline := time.Now().Add(daemonDrainTimeout() + 30*time.Second)
	for time.Now().Before(waitDeadline) {
		if !m.IsRunning() {
			logger.Info("Daemon stopped gracefully")
			// Release daemon lock
			lock, err := pkgruntime.NewLockFile(pkgruntime.ModeDaemon)
			if err == nil {
				_ = lock.Release()
			}
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Force kill if not stopped
	logger.Warn("Daemon did not stop gracefully, sending SIGKILL")
	if err := process.Signal(syscall.SIGKILL); err != nil {
		return fmt.Errorf("failed to send SIGKILL to process %d: %w", pid, err)
	}

	// Release daemon lock even if force killed
	lock, err := pkgruntime.NewLockFile(pkgruntime.ModeDaemon)
	if err == nil {
		_ = lock.Release()
	}

	// Wait briefly after SIGKILL
	time.Sleep(300 * time.Millisecond)

	if !m.IsRunning() {
		logger.Info("Daemon killed")
		return nil
	}

	return fmt.Errorf("failed to stop daemon process %d", pid)
}

// Restart restarts the daemon process
func (m *Manager) Restart() error {
	if err := m.Stop(); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}
	return m.Start(true)
}

// Status returns daemon status information
func (m *Manager) Status() (*Status, error) {
	status := &Status{
		Running: m.IsRunning(),
		PidFile: m.pidFile,
		Config:  m.config,
		// Timestamp filled below
	}

	pid, err := m.getPID()
	if err == nil {
		status.PID = pid
		if p, e := os.FindProcess(pid); e == nil {
			status.Process = p
		}
	}

	status.Timestamp = time.Now()
	return status, nil
}

// IsRunning checks if daemon is currently running
func (m *Manager) IsRunning() bool {
	switch pid, err := m.getPID(); {
	case err != nil:
		return false
	default:
		// Check if process exists
		if err := syscall.Kill(pid, 0); err == nil {
			return true
		}
	}
	return false
}

func (m *Manager) getPID() (int, error) {
	data, err := os.ReadFile(m.pidFile)
	if err != nil {
		return 0, err
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, err
	}
	return pid, nil
}

// GetConfig returns current daemon configuration
func (m *Manager) GetConfig() *config.DaemonConfig {
	return m.config
}

// UpdateConfig updates daemon configuration
func (m *Manager) UpdateConfig(daemonConfig *config.DaemonConfig) error {
	m.config = daemonConfig
	m.normalizePaths()
	return m.SaveConfig()
}

// Logs returns recent daemon logs
func (m *Manager) Logs(lines int) ([]string, error) {
	if m.config.LogFile == "" {
		return nil, fmt.Errorf("log file not configured")
	}
	cmd := exec.Command("tail", "-n", strconv.Itoa(lines), m.config.LogFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	// Split by lines
	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

// Status represents daemon status information
type Status struct {
	Running   bool                 `json:"running"`
	PID       int                  `json:"pid,omitempty"`
	Config    *config.DaemonConfig `json:"config"`
	PidFile   string               `json:"pid_file"`
	Timestamp time.Time            `json:"timestamp"`
	Process   *os.Process          `json:"-"` // Not serialized
}

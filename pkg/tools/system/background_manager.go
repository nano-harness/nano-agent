package system

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/google/uuid"
)

// BackgroundTaskStatus represents the status of a background task
type BackgroundTaskStatus string

const (
	BgStatusRunning   BackgroundTaskStatus = "running"
	BgStatusCompleted BackgroundTaskStatus = "completed"
	BgStatusFailed    BackgroundTaskStatus = "failed"
	BgStatusKilled    BackgroundTaskStatus = "killed"

	// MaxBgLogFileBytes is the maximum size for a single log file (100MB)
	MaxBgLogFileBytes = 100 * 1024 * 1024

	// MaxTasksPerSession limits the number of background tasks per session
	MaxTasksPerSession = 100

	// BgKillGracePeriod is the time to wait between SIGTERM and SIGKILL
	BgKillGracePeriod = 5 * time.Second
)

// BackgroundTask represents a long-running background task
type BackgroundTask struct {
	ID         string
	Command    string
	SessionID  string
	Workdir    string
	LogPath    string
	Pid        int
	mu         sync.RWMutex
	Status     BackgroundTaskStatus
	ExitCode   int
	StartedAt  time.Time
	FinishedAt *time.Time
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	logFile    *os.File
	logMu      sync.Mutex
	waitDone   chan struct{} // closed when the wait goroutine has finished cleanup
}

// GetStatus returns the task status using the task lock.
func (t *BackgroundTask) GetStatus() BackgroundTaskStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status
}

// snapshot returns a thread-safe copy of status, exit code, and finished time.
func (t *BackgroundTask) snapshot() (BackgroundTaskStatus, int, *time.Time) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var finishedAt *time.Time
	if t.FinishedAt != nil {
		finished := *t.FinishedAt
		finishedAt = &finished
	}
	return t.Status, t.ExitCode, finishedAt
}

// BackgroundTaskManager manages background shell tasks
type BackgroundTaskManager struct {
	mu      sync.RWMutex
	tasks   map[string]*BackgroundTask
	rootDir string
}

// Get returns a task by ID.
func (m *BackgroundTaskManager) Get(taskID string) (*BackgroundTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[taskID]
	return task, ok
}

// GetForSession returns a task by ID and enforces that it belongs to sessionID.
func (m *BackgroundTaskManager) GetForSession(taskID, sessionID string) (*BackgroundTask, bool) {
	task, ok := m.Get(taskID)
	if !ok {
		return nil, false
	}
	if sessionID != "" && task.SessionID != sessionID {
		return nil, false
	}
	return task, true
}

// NewBackgroundTaskManager creates a new background task manager
func NewBackgroundTaskManager(rootDir string) *BackgroundTaskManager {
	return &BackgroundTaskManager{
		tasks:   make(map[string]*BackgroundTask),
		rootDir: rootDir,
	}
}

// Spawn creates and starts a new background task
func (m *BackgroundTaskManager) Spawn(ctx context.Context, sessionID, command, workdir string) (*BackgroundTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check session task limit
	sessionTaskCount := 0
	for _, t := range m.tasks {
		if t.SessionID == sessionID {
			sessionTaskCount++
		}
	}
	if sessionTaskCount >= MaxTasksPerSession {
		// GC oldest completed/failed tasks for this session
		m.gcSessionLocked(sessionID)
	}

	// Generate task ID with session-derived prefix to avoid cross-session collisions.
	// Keep it short enough for UI tables and file names.
	sessionPrefix := sessionIDPrefix(sessionID)
	taskID := sessionPrefix + "-" + uuid.New().String()[:8]

	// Create session directory if needed
	sessionDir := filepath.Join(m.rootDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	// Create log file
	logPath := filepath.Join(sessionDir, taskID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	// Create independent context (not tied to parent ctx)
	taskCtx, cancel := context.WithCancel(context.Background())

	// Parse shell command
	var shell string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		shellArgs = []string{"/C", command}
	} else {
		shell = "sh"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.CommandContext(taskCtx, shell, shellArgs...)
	cmd.Dir = workdir

	// Set process group for proper cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Redirect output to limited writer
	limitedWriter := &limitedFileWriter{
		file:     logFile,
		maxBytes: MaxBgLogFileBytes,
	}
	cmd.Stdout = limitedWriter
	cmd.Stderr = limitedWriter

	// Start the command
	if err := cmd.Start(); err != nil {
		logFile.Close()
		cancel()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	task := &BackgroundTask{
		ID:        taskID,
		Command:   command,
		SessionID: sessionID,
		Workdir:   workdir,
		LogPath:   logPath,
		Pid:       cmd.Process.Pid,
		Status:    BgStatusRunning,
		StartedAt: time.Now(),
		cmd:       cmd,
		cancel:    cancel,
		logFile:   logFile,
		waitDone:  make(chan struct{}),
	}

	m.tasks[taskID] = task

	// Monitor task completion asynchronously
	go m.waitTask(task)

	logger.Infof("Spawned background task %s (PID %d): %s", taskID, task.Pid, command)
	return task, nil
}

func sessionIDPrefix(sessionID string) string {
	if sessionID == "" {
		return "default"
	}
	// Produce a stable, filesystem-safe short prefix.
	// Use base36 hash of session string to keep it compact.
	var h uint32 = 2166136261
	for i := 0; i < len(sessionID); i++ {
		h ^= uint32(sessionID[i])
		h *= 16777619
	}
	return "s" + strconv.FormatUint(uint64(h), 36)
}

// waitTask waits for a task to complete and updates its status
func (m *BackgroundTaskManager) waitTask(task *BackgroundTask) {
	defer close(task.waitDone)

	err := task.cmd.Wait()
	now := time.Now()

	// Only update if not already killed
	task.mu.Lock()
	if task.Status != BgStatusKilled {
		task.FinishedAt = &now
		if err != nil {
			task.Status = BgStatusFailed
			if exitErr, ok := err.(*exec.ExitError); ok {
				task.ExitCode = exitErr.ExitCode()
			} else {
				task.ExitCode = -1
			}
		} else {
			task.Status = BgStatusCompleted
			task.ExitCode = 0
		}
	}
	status, exitCode := task.Status, task.ExitCode
	task.mu.Unlock()

	// Close log file
	task.logMu.Lock()
	if task.logFile != nil {
		_ = task.logFile.Close()
		task.logFile = nil
	}
	task.logMu.Unlock()

	logger.Infof("Background task %s finished with status %s (exit code %d)", task.ID, status, exitCode)
}

// ReadOutput reads output from a background task's log file
func (m *BackgroundTaskManager) ReadOutput(taskID string, fromOffset int64, blockTimeout time.Duration, maxLines int) (content string, newOffset int64, status BackgroundTaskStatus, err error) {
	m.mu.RLock()
	task, exists := m.tasks[taskID]
	m.mu.RUnlock()

	if !exists {
		return "", 0, "", fmt.Errorf("task %s not found", taskID)
	}

	// If blocking is requested, wait for new output or completion
	if blockTimeout > 0 {
		deadline := time.Now().Add(blockTimeout)
		for time.Now().Before(deadline) {
			stat, err := os.Stat(task.LogPath)
			if err == nil && stat.Size() > fromOffset {
				break
			}
			if task.GetStatus() != BgStatusRunning {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Read log file from offset
	file, err := os.Open(task.LogPath)
	if err != nil {
		return "", fromOffset, task.GetStatus(), fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	// Seek to offset
	if _, err := file.Seek(fromOffset, io.SeekStart); err != nil {
		return "", fromOffset, task.GetStatus(), fmt.Errorf("failed to seek log file: %w", err)
	}

	// Read remaining content
	data, err := io.ReadAll(file)
	if err != nil {
		return "", fromOffset, task.GetStatus(), fmt.Errorf("failed to read log file: %w", err)
	}

	newOffset = fromOffset + int64(len(data))

	// Optionally limit lines
	content = string(data)
	if maxLines > 0 && content != "" {
		lines := splitLines(content)
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
		}
		content = joinLines(lines)
	}

	return content, newOffset, task.GetStatus(), nil
}

// Kill terminates a background task
func (m *BackgroundTaskManager) Kill(taskID string) error {
	m.mu.RLock()
	task, exists := m.tasks[taskID]
	if !exists {
		m.mu.RUnlock()
		return fmt.Errorf("task %s not found", taskID)
	}
	m.mu.RUnlock()

	// Mark as killed to prevent status override
	task.mu.Lock()
	task.Status = BgStatusKilled
	now := time.Now()
	task.FinishedAt = &now
	task.mu.Unlock()

	// Try graceful SIGTERM first
	if task.cmd.Process != nil && runtime.GOOS != "windows" {
		pgid := -task.cmd.Process.Pid
		_ = syscall.Kill(pgid, syscall.SIGTERM)

		// Wait for grace period
		timer := time.NewTimer(BgKillGracePeriod)

		select {
		case <-timer.C:
			// Grace period expired, force kill
			_ = syscall.Kill(pgid, syscall.SIGKILL)
			<-task.waitDone
		case <-task.waitDone:
			// Process exited gracefully
		}
		timer.Stop()
		if task.cancel != nil {
			task.cancel()
		}
	} else {
		// Cancel context
		if task.cancel != nil {
			task.cancel()
		}
		<-task.waitDone
	}

	logger.Infof("Killed background task %s (PID %d)", taskID, task.Pid)
	return nil
}

// List returns all background tasks for a session
func (m *BackgroundTaskManager) List(sessionID string) []*BackgroundTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tasks []*BackgroundTask
	for _, t := range m.tasks {
		if sessionID == "" || t.SessionID == sessionID {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// gcSessionLocked removes oldest completed/failed tasks for a session (must be called with lock held)
func (m *BackgroundTaskManager) gcSessionLocked(sessionID string) {
	var toRemove []string
	for id, t := range m.tasks {
		status := t.GetStatus()
		if t.SessionID == sessionID && (status == BgStatusCompleted || status == BgStatusFailed) {
			toRemove = append(toRemove, id)
		}
	}

	// Remove half of completed tasks
	limit := len(toRemove) / 2
	for i := 0; i < limit && i < len(toRemove); i++ {
		delete(m.tasks, toRemove[i])
		logger.Debugf("GC removed background task %s from session %s", toRemove[i], sessionID)
	}
}

// limitedFileWriter wraps a file to enforce size limits
type limitedFileWriter struct {
	file     *os.File
	written  int64
	maxBytes int64
	mu       sync.Mutex
}

func (w *limitedFileWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.written+int64(len(p)) > w.maxBytes {
		// Truncate write to stay within limit
		available := w.maxBytes - w.written
		if available <= 0 {
			return len(p), nil // Pretend we wrote it
		}
		p = p[:available]
	}

	n, err = w.file.Write(p)
	w.written += int64(n)
	return n, err
}

// splitLines splits content into lines
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := []string{}
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines = append(lines, content[start:i+1])
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

// joinLines joins lines back into content
func joinLines(lines []string) string {
	result := ""
	for _, line := range lines {
		result += line
	}
	return result
}

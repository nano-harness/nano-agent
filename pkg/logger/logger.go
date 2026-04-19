package logger //nolint:revive

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	globalLogger *zap.SugaredLogger
	initOnce     sync.Once
	atomicLevel  zap.AtomicLevel
	sessionID    string
	sessionOnce  sync.Once
	tuiMode      bool
	tuiMutex     sync.RWMutex
	// daemon logging configuration
	configMu      sync.RWMutex
	customLogPath string
	consoleAlso   = true
)

const (
	// TimeFormat is the standard time format used for logging and display
	TimeFormat = "2006-01-02 15:04:05"
)

func init() {
	atomicLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
}

// ConfigureForDaemon sets a unified logging strategy for daemon mode.
// It directs logs to the provided logFile and optionally also to stderr (for journald/systemd).
// Calling this will reinitialize the logger on next GetLogger().
func ConfigureForDaemon(logFile string, alsoLogToStderr bool) {
	configMu.Lock()
	defer configMu.Unlock()

	// Ensure directory exists when a custom path is provided
	if logFile != "" {
		dir := filepath.Dir(logFile)
		_ = os.MkdirAll(dir, 0o755)
		customLogPath = logFile
	} else {
		customLogPath = ""
	}
	consoleAlso = alsoLogToStderr

	// Reset logger so new settings take effect
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
	globalLogger = nil
	initOnce = sync.Once{}
}

func GetLogger() *zap.SugaredLogger { //nolint:revive
	initOnce.Do(func() {
		// Resolve target log file path
		configMu.RLock()
		path := customLogPath
		alsoConsole := consoleAlso
		configMu.RUnlock()

		if path == "" {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil || home == "" {
				// Fall back to plain temp directory when home is unavailable
				path = filepath.Join(os.TempDir(), "nano-agent-debug.log")
			} else {
				nanoDir := filepath.Join(home, ".nano")
				if mkdirErr := os.MkdirAll(nanoDir, 0o700); mkdirErr != nil {
					fmt.Fprintf(os.Stderr, "failed to create log directory %q: %v; falling back to temp dir\n", nanoDir, mkdirErr)
					path = filepath.Join(os.TempDir(), "nano-agent-debug.log")
				} else {
					path = filepath.Join(nanoDir, "nano-agent-debug.log")
				}
			}
		}

		// Configure encoder
		encoderConfig := zapcore.EncoderConfig{
			TimeKey:        "time",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.CapitalLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		}

		// Create file output
		fileOutput := zapcore.AddSync(&lumberjack.Logger{
			Filename:   path,
			MaxSize:    100, // megabytes
			MaxBackups: 3,
			MaxAge:     28, // days
			Compress:   true,
		})

		// Configure core based on TUI mode
		tuiMutex.RLock()
		isInTUIMode := tuiMode
		tuiMutex.RUnlock()

		var core zapcore.Core
		if isInTUIMode {
			// In TUI mode, only log to file to avoid stderr interference
			core = zapcore.NewCore(
				zapcore.NewJSONEncoder(encoderConfig),
				fileOutput,
				atomicLevel,
			)
		} else {
			// In non-TUI mode, optionally log to both file and stderr
			if alsoConsole {
				consoleOutput := zapcore.Lock(os.Stderr)
				core = zapcore.NewTee(
					zapcore.NewCore(
						zapcore.NewJSONEncoder(encoderConfig),
						fileOutput,
						atomicLevel,
					),
					zapcore.NewCore(
						zapcore.NewConsoleEncoder(encoderConfig),
						consoleOutput,
						atomicLevel,
					),
				)
			} else {
				core = zapcore.NewCore(
					zapcore.NewJSONEncoder(encoderConfig),
					fileOutput,
					atomicLevel,
				)
			}
		}

		// Create logger with session ID field
		logger := zap.New(core,
			zap.AddCaller(),
			zap.AddCallerSkip(1),
			zap.Fields(zap.String("session_id", getSessionID())),
		)

		globalLogger = logger.Sugar()
	})
	return globalLogger
}

// SetVerbose sets the logging level based on verbose flag
func SetVerbose(verbose bool) {
	if verbose {
		atomicLevel.SetLevel(zap.DebugLevel)
	} else {
		atomicLevel.SetLevel(zap.InfoLevel)
	}
}

// SetTUIMode configures logger for TUI mode to avoid stderr interference
func SetTUIMode(enabled bool) {
	tuiMutex.Lock()
	defer tuiMutex.Unlock()

	// If mode is changing, reset the logger to apply new configuration
	if tuiMode != enabled {
		tuiMode = enabled
		// Reset the logger to apply new configuration
		initOnce = sync.Once{}
		globalLogger = nil
	}
}

// IsTUIMode returns whether TUI mode is enabled
func IsTUIMode() bool {
	tuiMutex.RLock()
	defer tuiMutex.RUnlock()
	return tuiMode
}

// Debug logs a debug message
func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
}

func Debugf(template string, args ...interface{}) { //nolint:revive
	GetLogger().Debugf(template, args...)
}

func Info(args ...interface{}) { //nolint:revive
	GetLogger().Info(args...)
}

func Infof(template string, args ...interface{}) { //nolint:revive
	GetLogger().Infof(template, args...)
}

func Warn(args ...interface{}) { //nolint:revive
	GetLogger().Warn(args...)
}

func Warnf(template string, args ...interface{}) { //nolint:revive
	GetLogger().Warnf(template, args...)
}

func Error(args ...interface{}) { //nolint:revive
	GetLogger().Error(args...)
}

func Errorf(template string, args ...interface{}) { //nolint:revive
	GetLogger().Errorf(template, args...)
}

func Fatal(args ...interface{}) { //nolint:revive
	GetLogger().Fatal(args...)
}

func Fatalf(template string, args ...interface{}) { //nolint:revive
	GetLogger().Fatalf(template, args...)
}

// generateSessionID creates a unique session identifier
func generateSessionID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to process ID if random generation fails
		return fmt.Sprintf("pid-%d", os.Getpid())
	}
	return hex.EncodeToString(bytes)
}

// getSessionID returns the current session ID, generating it if needed
func getSessionID() string {
	sessionOnce.Do(func() {
		sessionID = generateSessionID()
	})
	return sessionID
}

// GetSessionID returns the current session ID for external use
func GetSessionID() string {
	return getSessionID()
}

func Close() error { //nolint:revive
	if globalLogger != nil {
		return globalLogger.Sync()
	}
	return nil
}

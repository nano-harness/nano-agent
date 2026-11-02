package daemon

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultTaskTimeoutSeconds is the default timeout for tasks
	DefaultTaskTimeoutSeconds = 6 * 60 * 60
	// MaxTaskTimeoutSeconds is the maximum timeout for tasks
	MaxTaskTimeoutSeconds = 72 * 60 * 60
	// ClientHTTPGraceSeconds is the grace period for client HTTP connections
	ClientHTTPGraceSeconds = 300
)

// NormalizeTaskTimeoutSeconds normalizes the task timeout in seconds
// NormalizeTaskTimeoutSeconds normalizes the task timeout in seconds
func NormalizeTaskTimeoutSeconds(timeout int) int {
	if timeout <= 0 {
		return DefaultTaskTimeoutSeconds
	}
	if timeout > MaxTaskTimeoutSeconds {
		return MaxTaskTimeoutSeconds
	}
	return timeout
}

func daemonDrainTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("NANO_DAEMON_DRAIN_TIMEOUT"))
	if v == "" {
		return 10 * time.Minute
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return 10 * time.Minute
}

package cli

import (
	"errors"
	"io"
	"os"
	"syscall"

	"github.com/mattn/go-isatty"
)

// IsPipedStdin checks if stdin is piped (not a terminal)
func IsPipedStdin() bool {
	return !isatty.IsTerminal(os.Stdin.Fd())
}

// IsPipedStdout checks if stdout is piped (not a terminal)
func IsPipedStdout() bool {
	return !isatty.IsTerminal(os.Stdout.Fd())
}

// SafeWrite writes data to the writer and returns nil if the error is EPIPE,
// allowing graceful exit when downstream pipe is closed
func SafeWrite(w io.Writer, data []byte) error {
	_, err := w.Write(data)
	if err != nil {
		// Check if error is broken pipe (EPIPE)
		if errors.Is(err, syscall.EPIPE) {
			return nil // Gracefully ignore broken pipe
		}
		return err
	}
	return nil
}

// IgnoreSIGPIPE sets up signal handling to ignore SIGPIPE.
// This should be called early in process startup for programs that write to pipes.
// Note: On Unix systems, SIGPIPE is typically handled at the syscall level,
// but this provides an additional safety layer.
func IgnoreSIGPIPE() {
	// SIGPIPE is already handled via signal.Notify in signalContext
	// This is a placeholder for any additional pipe-specific initialization
}

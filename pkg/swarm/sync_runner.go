package swarm

import (
	"context"
	"fmt"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
)

// SyncResult holds the result of a synchronous subagent execution.
type SyncResult struct {
	AgentID string
	Content string
	Error   error
	Usage   map[string]interface{}
}

// SyncRunOptions configures synchronous subagent execution.
type SyncRunOptions struct {
	Identity     *TeammateIdentity
	Prompt       string
	Runner       Runner
	Timeout      time.Duration
	MaxTurns     int
	WorktreePath string
}

// RunSync executes a subagent synchronously within the parent's turn.
// It blocks until the subagent completes, times out, or the parent ctx is cancelled.
func RunSync(ctx context.Context, opts SyncRunOptions) (*SyncResult, error) {
	if opts.Identity == nil {
		return nil, fmt.Errorf("identity is required for sync execution")
	}
	if opts.Prompt == "" {
		return nil, fmt.Errorf("prompt is required for sync execution")
	}

	// Default timeout: 5 minutes
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	// Create a child context bound to parent + timeout
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Set up the identity's abort context
	opts.Identity.AbortCtx = childCtx
	opts.Identity.AbortCancel = cancel

	// Get runner
	runner := opts.Runner
	if runner == nil {
		runner = NewDefaultRunner(config.Get())
	}

	// Result channel
	resultCh := make(chan *SyncResult, 1)

	go func() {
		err := runner.Run(childCtx, opts.Identity, opts.Prompt)
		result := &SyncResult{
			AgentID: opts.Identity.AgentID,
			Error:   err,
		}
		// Content is captured via the runner's event handler
		// For now we report completion status
		if err == nil {
			result.Content = "Task completed successfully."
		}
		resultCh <- result
	}()

	// Wait for completion or context cancellation
	select {
	case result := <-resultCh:
		return result, nil
	case <-childCtx.Done():
		cancel()
		return &SyncResult{
			AgentID: opts.Identity.AgentID,
			Error:   childCtx.Err(),
		}, childCtx.Err()
	}
}

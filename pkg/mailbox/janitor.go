package mailbox

import (
	"context"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// runJanitor is the background cleanup goroutine that periodically cleans up
// expired messages and checks for ack timeouts across all mailboxes
func runJanitor(ctx context.Context, mgr *Manager, interval time.Duration, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Infof("mailbox janitor started (interval: %v)", interval)

	for {
		select {
		case <-ctx.Done():
			logger.Info("mailbox janitor stopping")
			return
		case <-ticker.C:
			runJanitorCycle(ctx, mgr)
		}
	}
}

// runJanitorCycle performs one cleanup cycle
// TODO swarm: Phase 1 - Simplified to just trigger Peek which filters expired messages
func runJanitorCycle(ctx context.Context, mgr *Manager) {
	mgr.mu.RLock()
	if mgr.closed {
		mgr.mu.RUnlock()
		return
	}

	// Get snapshot of all cached mailboxes
	mailboxes := make([]Mailbox, 0, len(mgr.cache))
	for _, mb := range mgr.cache {
		mailboxes = append(mailboxes, mb)
	}
	mgr.mu.RUnlock()

	// Process each mailbox - just peek to trigger TTL cleanup
	cleaned := 0
	for _, mb := range mailboxes {
		// Peek with limit 0 to just trigger cleanup without returning messages
		_, err := mb.Peek(ctx, 0)
		if err == nil {
			cleaned++
		}
	}

	if cleaned > 0 {
		logger.Debugf("mailbox janitor cycle complete: processed %d mailboxes", cleaned)
	}
}

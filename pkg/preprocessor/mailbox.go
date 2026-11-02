package preprocessor

import (
	"context"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/mailbox"
)

// DrainMailboxAttachment drains a mailbox and returns a formatted attachment
// suitable for appending to turn input.
func DrainMailboxAttachment(ctx context.Context, mb mailbox.Mailbox) (string, bool, error) {
	if mb == nil {
		return "", false, nil
	}
	attachment, err := mailbox.DrainAndFormat(ctx, mb)
	if err != nil {
		return "", false, fmt.Errorf("drain mailbox attachment: %w", err)
	}
	return attachment, attachment != "", nil
}

// HasUnreadMailboxMessages reports whether the mailbox currently has messages
// that should keep a turn active.
func HasUnreadMailboxMessages(ctx context.Context, mb mailbox.Mailbox) (bool, int, error) {
	if mb == nil {
		return false, 0, nil
	}
	count, err := mb.Count(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("count mailbox messages: %w", err)
	}
	return count > 0, count, nil
}

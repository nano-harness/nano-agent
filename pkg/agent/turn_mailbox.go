package agent

import (
	"context"

	"github.com/nano-harness/nano-agent/pkg/preprocessor"
)

func (t *Turn) hasUnreadMailboxMessages(ctx context.Context) (bool, int, error) {
	if t.agent == nil {
		return false, 0, nil
	}
	return preprocessor.HasUnreadMailboxMessages(ctx, t.agent.Mailbox())
}

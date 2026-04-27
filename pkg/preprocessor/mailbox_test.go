package preprocessor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/mailbox"
)

func TestDrainMailboxAttachmentNilMailbox(t *testing.T) {
	attachment, ok, err := DrainMailboxAttachment(context.Background(), nil)
	if err != nil {
		t.Fatalf("DrainMailboxAttachment returned error: %v", err)
	}
	if ok || attachment != "" {
		t.Fatalf("expected no attachment, got ok=%v attachment=%q", ok, attachment)
	}
}

func TestDrainMailboxAttachmentFormatsAndDrainsMessages(t *testing.T) {
	ctx := context.Background()
	mb := newTestMailbox(t)

	if err := mb.Send(ctx, mailbox.Message{
		ID:        "msg-1",
		From:      "researcher",
		To:        "lead",
		Topic:     mailbox.TopicProgress,
		Body:      map[string]interface{}{"content": "done"},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	attachment, ok, err := DrainMailboxAttachment(ctx, mb)
	if err != nil {
		t.Fatalf("DrainMailboxAttachment returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected mailbox attachment")
	}
	if !strings.Contains(attachment, "Mailbox Messages") || !strings.Contains(attachment, "done") {
		t.Fatalf("unexpected attachment: %q", attachment)
	}

	hasUnread, count, err := HasUnreadMailboxMessages(ctx, mb)
	if err != nil {
		t.Fatalf("HasUnreadMailboxMessages returned error: %v", err)
	}
	if hasUnread || count != 0 {
		t.Fatalf("expected drained mailbox, got hasUnread=%v count=%d", hasUnread, count)
	}
}

func TestHasUnreadMailboxMessages(t *testing.T) {
	ctx := context.Background()
	mb := newTestMailbox(t)

	hasUnread, count, err := HasUnreadMailboxMessages(ctx, mb)
	if err != nil {
		t.Fatalf("HasUnreadMailboxMessages returned error: %v", err)
	}
	if hasUnread || count != 0 {
		t.Fatalf("expected empty mailbox, got hasUnread=%v count=%d", hasUnread, count)
	}

	if err := mb.Send(ctx, mailbox.Message{
		ID:        "msg-1",
		From:      "teammate",
		To:        "lead",
		Topic:     mailbox.TopicFinding,
		Body:      map[string]interface{}{"type": "note", "content": "check this"},
		Timestamp: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	hasUnread, count, err = HasUnreadMailboxMessages(ctx, mb)
	if err != nil {
		t.Fatalf("HasUnreadMailboxMessages returned error: %v", err)
	}
	if !hasUnread || count != 1 {
		t.Fatalf("expected one unread message, got hasUnread=%v count=%d", hasUnread, count)
	}
}

func newTestMailbox(t *testing.T) mailbox.Mailbox {
	t.Helper()
	backend := mailbox.NewMemoryBackend(mailbox.DefaultOptions())
	t.Cleanup(func() { _ = backend.Close() })
	mb, err := backend.Open("lead")
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	return mb
}

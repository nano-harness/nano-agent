package ui

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/ui/tview"
)

func TestTViewAdapter_SendEventDoesNotForwardCronEvents(t *testing.T) {
	integration := tview.NewIntegration()
	defer integration.GetModel().GetStateManager().Stop()
	defer integration.Cleanup()
	adapter := &TViewAdapter{integration: integration}

	adapter.sendEvent(event.StreamEvent{
		Type:    event.EventTypeContent,
		Content: "visible",
	})
	waitForConversation(t, integration, 1)

	adapter.sendEvent(event.StreamEvent{
		Type: event.EventTypeCronTaskStarted,
		Metadata: map[string]interface{}{
			"task_id": "cron-a",
		},
	})
	time.Sleep(50 * time.Millisecond)

	history := integration.GetConversationHistory()
	if len(history) != 1 || history[0].Content != "visible" {
		t.Fatalf("unexpected history after cron event: %#v", history)
	}
}

func waitForConversation(t *testing.T, integration *tview.Integration, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(integration.GetConversationHistory()) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("conversation length did not reach %d", want)
}

package tview

import (
	"testing"
)

func drainEventChan(model *Model) {
	select {
	case fn := <-model.eventChan:
		if fn != nil {
			fn()
		}
	default:
	}
}

func TestThinkingMessageToggleAndUpdate(t *testing.T) {
	model := NewModel()
	defer model.Stop()

	// Enqueue initial thinking block
	model.AddThinking("思考中", "", nil)
	drainEventChan(model)

	if len(model.messages) == 0 {
		t.Fatalf("expected thinking message to be added")
	}

	tm, ok := model.messages[0].(*ThinkingMessage)
	if !ok {
		t.Fatalf("expected ThinkingMessage, got %T", model.messages[0])
	}

	if !tm.Collapsed {
		t.Fatalf("new thinking message should start collapsed")
	}

	// Toggle to expand
	model.toggleLatestThinking()
	drainEventChan(model)

	if tm.Collapsed {
		t.Fatalf("thinking message should be expanded after toggle")
	}

	// Update with reasoning and completion metadata – should auto-collapse on completion
	meta := map[string]interface{}{"is_complete": true}
	model.AddThinking("思考完成", "详细推理内容", meta)
	drainEventChan(model)

	if tm.Reasoning == "" {
		t.Fatalf("expected reasoning content to be stored")
	}
	if !tm.Completed {
		t.Fatalf("expected thinking message to be marked completed")
	}
	if !tm.Collapsed {
		t.Fatalf("thinking message should be auto-collapsed when complete")
	}
}

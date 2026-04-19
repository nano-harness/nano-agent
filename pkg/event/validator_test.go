package event

import (
	"strings"
	"testing"
	"time"
)

func TestEventValidator_ValidateEvent_TimestampSecondsNoWarnings(t *testing.T) {
	v := NewEventValidator()
	ev := StreamEvent{
		Type:      EventTypeContent,
		Content:   "ok",
		Timestamp: time.Now().Unix(),
		Source:    "test",
	}

	res := v.ValidateEvent(ev)
	if !res.Valid {
		t.Fatalf("expected valid, got errors=%v warnings=%v", res.Errors, res.Warnings)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", res.Warnings)
	}
}

func TestEventValidator_ValidateEvent_TaskProgressRequiresTaskID(t *testing.T) {
	v := NewEventValidator()
	ev := StreamEvent{
		Type:     EventTypeTaskProgress,
		TaskID:   "",
		Progress: 0.5,
		Source:   "test",
	}

	res := v.ValidateEvent(ev)
	if res.Valid {
		t.Fatalf("expected invalid")
	}
	found := false
	for _, errStr := range res.Errors {
		if strings.Contains(errStr, "TaskID is required") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected TaskID required error, got %v", res.Errors)
	}
}

func TestEventValidator_ValidateEvent_ProgressOutOfRangeInvalid(t *testing.T) {
	v := NewEventValidator()
	ev := StreamEvent{
		Type:     EventTypeTaskProgress,
		TaskID:   "t1",
		Progress: 1.5,
		Source:   "test",
	}

	res := v.ValidateEvent(ev)
	if res.Valid {
		t.Fatalf("expected invalid")
	}
	found := false
	for _, errStr := range res.Errors {
		if strings.Contains(errStr, "Progress must be between 0.0 and 1.0") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected progress range error, got %v", res.Errors)
	}
}

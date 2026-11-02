package permission

import (
	"testing"
)

func TestConfidenceThreshold_Default(t *testing.T) {
	manager := NewManager(ModeDefault, nil)
	threshold := manager.GetConfidenceThreshold()
	if threshold != 0.8 {
		t.Errorf("expected default confidence threshold 0.8, got %f", threshold)
	}
}

func TestConfidenceThreshold_SetAndGet(t *testing.T) {
	manager := NewManager(ModeDefault, nil)

	// Test setting to a different value
	manager.SetConfidenceThreshold(0.9)
	threshold := manager.GetConfidenceThreshold()
	if threshold != 0.9 {
		t.Errorf("expected confidence threshold 0.9, got %f", threshold)
	}

	// Test setting to a lower value
	manager.SetConfidenceThreshold(0.5)
	threshold = manager.GetConfidenceThreshold()
	if threshold != 0.5 {
		t.Errorf("expected confidence threshold 0.5, got %f", threshold)
	}

	// Test setting to 0
	manager.SetConfidenceThreshold(0.0)
	threshold = manager.GetConfidenceThreshold()
	if threshold != 0.0 {
		t.Errorf("expected confidence threshold 0.0, got %f", threshold)
	}

	// Test setting to 1.0
	manager.SetConfidenceThreshold(1.0)
	threshold = manager.GetConfidenceThreshold()
	if threshold != 1.0 {
		t.Errorf("expected confidence threshold 1.0, got %f", threshold)
	}
}

func TestConfidenceThreshold_Concurrency(t *testing.T) {
	manager := NewManager(ModeDefault, nil)

	// Test concurrent read/write safety
	done := make(chan bool, 2)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			manager.SetConfidenceThreshold(0.7)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = manager.GetConfidenceThreshold()
		}
		done <- true
	}()

	// Wait for both to complete
	<-done
	<-done

	// No assertion needed - just verify no race condition occurs
}

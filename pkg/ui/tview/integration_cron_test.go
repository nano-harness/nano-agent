package tview

import (
	"sync"
	"testing"
	"time"
)

func TestIntegration_SetCronTrackerWiresOnChange(t *testing.T) {
	integration := NewIntegration()
	defer integration.model.stateManager.Stop()
	defer integration.Cleanup()

	tracker := &fakeCronTracker{}
	integration.SetCronTracker(tracker)
	tracker.setIndicator("⏰ 1 running")
	tracker.fire()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if getCronIndicator(integration) == "⏰ 1 running" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cronIndicator = %q, want ⏰ 1 running", getCronIndicator(integration))
}

type fakeCronTracker struct {
	mu        sync.RWMutex
	indicator string
	onChange  func()
}

func (f *fakeCronTracker) SetOnChange(fn func()) { f.onChange = fn }
func (f *fakeCronTracker) FormatIndicator() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.indicator
}
func (f *fakeCronTracker) setIndicator(indicator string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.indicator = indicator
}
func (f *fakeCronTracker) fire() {
	if f.onChange != nil {
		f.onChange()
	}
}

func getCronIndicator(integration *Integration) string {
	result := make(chan string, 1)
	integration.eventChan <- func() {
		result <- integration.model.cronIndicator
	}
	return <-result
}

package middleware

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSystemMonitor_StartStop(t *testing.T) {
	sm := NewSystemMonitor(nil)
	sm.Start(context.Background())
	sm.Stop()
}

func TestSystemMonitor_StartIdempotent(t *testing.T) {
	sm := NewSystemMonitor(nil)
	sm.Start(context.Background())
	sm.Start(context.Background()) // second call is a no-op
	sm.Stop()
}

func TestSystemMonitor_StopWithoutStart(t *testing.T) {
	sm := NewSystemMonitor(nil)
	sm.Stop() // should not panic
}

func TestSystemMonitor_EventCallback(t *testing.T) {
	var got []string
	fn := func(eventType, content string) {
		got = append(got, eventType+":"+content)
	}

	sm := NewSystemMonitor(fn)
	sm.Start(context.Background())
	sm.Stop()

	found := false
	for _, e := range got {
		if e == "debug:system monitor started" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected start event, got %v", got)
	}
}

func TestSystemMonitor_RecordAPIRequest(t *testing.T) {
	sm := NewSystemMonitor(nil)

	sm.RecordAPIRequest(10*time.Millisecond, true)
	sm.RecordAPIRequest(20*time.Millisecond, false)

	sm.mu.RLock()
	count := sm.apiRequestCount
	errs := sm.errorCount
	sm.mu.RUnlock()

	if count != 2 {
		t.Errorf("apiRequestCount = %d, want 2", count)
	}
	if errs != 1 {
		t.Errorf("errorCount = %d, want 1", errs)
	}
}

func TestSystemMonitor_RecordToolExecution(t *testing.T) {
	sm := NewSystemMonitor(nil)
	sm.RecordToolExecution(5*time.Millisecond, true)
	sm.RecordToolExecution(5*time.Millisecond, true)

	sm.mu.RLock()
	count := sm.toolExecCount
	sm.mu.RUnlock()

	if count != 2 {
		t.Errorf("toolExecCount = %d, want 2", count)
	}
}

func TestSystemMonitor_RegisterHealthChecker(t *testing.T) {
	sm := NewSystemMonitor(nil)
	sm.RegisterHealthChecker("db", func() (string, error) {
		return "healthy", nil
	})

	h := sm.GetHealthStatus()
	if _, ok := h.Components["db"]; !ok {
		t.Error("expected 'db' component in health status")
	}
}

func TestSystemMonitor_GetCurrentMetrics_Empty(t *testing.T) {
	sm := NewSystemMonitor(nil)
	sys, perf := sm.GetCurrentMetrics()
	// No metrics collected yet – zero values.
	if sys.Goroutines != 0 {
		t.Errorf("expected 0 goroutines in empty metrics, got %d", sys.Goroutines)
	}
	if perf.APIRequestCount != 0 {
		t.Errorf("expected 0 api requests in empty metrics, got %d", perf.APIRequestCount)
	}
}

func TestSystemMonitor_GetMetricsHistory(t *testing.T) {
	sm := NewSystemMonitor(nil)
	// Call collectMetrics directly to populate history.
	sm.collectMetrics()
	sm.collectMetrics()

	sys, perf := sm.GetMetricsHistory(1)
	if len(sys) != 1 {
		t.Errorf("expected 1 sys entry, got %d", len(sys))
	}
	if len(perf) != 1 {
		t.Errorf("expected 1 perf entry, got %d", len(perf))
	}
}

func TestSystemMonitor_HealthStatus_Unhealthy(t *testing.T) {
	sm := NewSystemMonitor(nil)
	sm.RegisterHealthChecker("bad", func() (string, error) {
		return "unhealthy", nil
	})

	sm.checkHealth()

	h := sm.GetHealthStatus()
	if h.Overall != "unhealthy" {
		t.Errorf("overall = %q, want unhealthy", h.Overall)
	}
}

func TestSystemMonitor_ConcurrentRecording(t *testing.T) {
	sm := NewSystemMonitor(nil)
	const n = 100

	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			sm.RecordAPIRequest(1*time.Millisecond, true)
		}()
		go func() {
			defer wg.Done()
			sm.RecordToolExecution(1*time.Millisecond, true)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for goroutines")
	}

	sm.mu.RLock()
	apiCount := sm.apiRequestCount
	toolCount := sm.toolExecCount
	sm.mu.RUnlock()

	if apiCount != n {
		t.Errorf("apiRequestCount = %d, want %d", apiCount, n)
	}
	if toolCount != n {
		t.Errorf("toolExecCount = %d, want %d", toolCount, n)
	}
}

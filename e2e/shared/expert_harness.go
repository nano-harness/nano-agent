//go:build e2e

package shared

import (
	"sort"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/require"
)

// ExpertEventInfo contains metadata extracted from expert events.
type ExpertEventInfo struct {
	ExpertName  string
	DisplayName string
	Source      string
	Success     bool
	StartTime   int64
	FinishTime  int64
	WorkerID    string
}

// CountEventsByWorkerID counts how many times each WorkerID appears for a given event type.
// This is critical for validating parallel execution - each worker should have complete event sequences.
func CountEventsByWorkerID(events []event.StreamEvent, eventType event.EventType) map[string]int {
	counts := make(map[string]int)
	for _, ev := range events {
		if ev.Type == eventType {
			workerID := ""
			if ev.Metadata != nil {
				if wid, ok := ev.Metadata["worker_id"].(string); ok {
					workerID = wid
				}
			}
			// Use empty string for events without worker_id (main agent)
			counts[workerID]++
		}
	}
	return counts
}

// ExtractExpertResults parses all EventTypeExpertFinished events and returns metadata by expert name.
// Returns a map of expert_name → ExpertEventInfo.
func ExtractExpertResults(events []event.StreamEvent) map[string]ExpertEventInfo {
	results := make(map[string]ExpertEventInfo)

	// First pass: find all started events to get start times
	startTimes := make(map[string]int64) // expert_name → start timestamp
	for _, ev := range events {
		if ev.Type == event.EventTypeExpertStarted && ev.Metadata != nil {
			if name, ok := ev.Metadata["expert_name"].(string); ok {
				startTimes[name] = ev.Timestamp
			}
		}
	}

	// Second pass: extract finished event metadata
	for _, ev := range events {
		if ev.Type == event.EventTypeExpertFinished && ev.Metadata != nil {
			info := ExpertEventInfo{
				FinishTime: ev.Timestamp,
			}

			if name, ok := ev.Metadata["expert_name"].(string); ok {
				info.ExpertName = name
				if st, exists := startTimes[name]; exists {
					info.StartTime = st
				}
			}
			if displayName, ok := ev.Metadata["display_name"].(string); ok {
				info.DisplayName = displayName
			}
			if source, ok := ev.Metadata["source"].(string); ok {
				info.Source = source
			}
			if success, ok := ev.Metadata["success"].(bool); ok {
				info.Success = success
			}
			if workerID, ok := ev.Metadata["worker_id"].(string); ok {
				info.WorkerID = workerID
			}

			results[info.ExpertName] = info
		}
	}

	return results
}

// TimeRange represents a time interval.
type TimeRange struct {
	Start int64
	End   int64
}

// AssertParallelExecution verifies that multiple workers executed in parallel by checking timestamp overlap.
// This proves true parallelism rather than serial execution.
func AssertParallelExecution(t *testing.T, events []event.StreamEvent, expectedWorkers int) {
	t.Helper()

	// Extract time ranges for each worker
	workerRanges := make(map[string]TimeRange)

	for _, ev := range events {
		if ev.Metadata == nil {
			continue
		}
		workerID, hasWorkerID := ev.Metadata["worker_id"].(string)
		if !hasWorkerID || workerID == "" {
			continue
		}

		timestamp := ev.Timestamp
		if timestamp == 0 {
			continue
		}

		// Update range for this worker
		if existing, ok := workerRanges[workerID]; ok {
			if timestamp < existing.Start {
				existing.Start = timestamp
			}
			if timestamp > existing.End {
				existing.End = timestamp
			}
			workerRanges[workerID] = existing
		} else {
			workerRanges[workerID] = TimeRange{
				Start: timestamp,
				End:   timestamp,
			}
		}
	}

	// Verify we have expected number of workers
	require.Equal(t, expectedWorkers, len(workerRanges),
		"Expected %d workers but found %d", expectedWorkers, len(workerRanges))

	if expectedWorkers < 2 {
		// Can't verify parallelism with less than 2 workers
		return
	}

	// Check for time overlap between workers
	// If truly parallel, at least some workers should have overlapping execution periods
	ranges := make([]TimeRange, 0, len(workerRanges))
	for _, r := range workerRanges {
		ranges = append(ranges, r)
	}

	// Sort ranges by start time
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	// Check for overlap: if worker N+1 starts before worker N finishes, they overlapped
	hasOverlap := false
	for i := 0; i < len(ranges)-1; i++ {
		if ranges[i+1].Start < ranges[i].End {
			hasOverlap = true
			break
		}
	}

	require.True(t, hasOverlap,
		"Expected parallel execution but workers appear to run serially. "+
		"Check that ForkBatch is using goroutines and semaphore correctly.")
}

// AssertExpertEvent validates that an expert event sequence is complete and correct.
func AssertExpertEvent(t *testing.T, events []event.StreamEvent, expertName string, expectSuccess bool) {
	t.Helper()

	var startEvent, finishEvent *event.StreamEvent

	for i := range events {
		ev := &events[i]
		if ev.Metadata == nil {
			continue
		}

		if name, ok := ev.Metadata["expert_name"].(string); !ok || name != expertName {
			continue
		}

		switch ev.Type {
		case event.EventTypeExpertStarted:
			startEvent = ev
		case event.EventTypeExpertFinished:
			finishEvent = ev
		}
	}

	require.NotNil(t, startEvent, "Expected EventTypeExpertStarted for expert %q", expertName)
	require.NotNil(t, finishEvent, "Expected EventTypeExpertFinished for expert %q", expertName)

	// Validate finish event metadata
	if success, ok := finishEvent.Metadata["success"].(bool); ok {
		require.Equal(t, expectSuccess, success,
			"Expert %q success mismatch", expertName)
	}

	// Validate timing: finish must be after start
	require.Greater(t, finishEvent.Timestamp, startEvent.Timestamp,
		"Expert %q finish timestamp should be after start timestamp", expertName)
}

// WaitForExpertCompletion waits for all expected expert finish events to appear.
// Useful for async tests where expert execution happens in background.
func WaitForExpertCompletion(events *[]event.StreamEvent, expectedCount int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		count := 0
		for _, ev := range *events {
			if ev.Type == event.EventTypeExpertFinished {
				count++
			}
		}
		if count >= expectedCount {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

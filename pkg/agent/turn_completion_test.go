package agent

import (
	"os"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

func TestTurnCompletionImplicit(t *testing.T) {
	tempDir := t.TempDir()
	originalDir, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	_, err := config.LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	t.Run("NonCompletionMetadataDoesNotComplete", func(t *testing.T) {
		turn := newTestTurn()

		err := turn.addToolResultsToContext(map[string]*interfaces.ToolResult{
			"call_1": {
				Success:    true,
				LLMContent: "ok",
				Metadata: map[string]interface{}{
					"tool_name":               "some_tool",
					"task_completion_signal":  true,
					"completion_confidence":   1.0,
					"some_other_completion":   true,
					"another_completion_flag": true,
				},
			},
		})
		if err != nil {
			t.Fatalf("addToolResultsToContext returned error: %v", err)
		}
		// With implicit completion, tools don't trigger completion - only finish_reason="stop" + no tool calls does
		if turn.CompletionCriteria.TaskCompleted {
			t.Fatalf("expected TaskCompleted=false, got true")
		}
	})

	t.Run("MarkTaskCompletedWorks", func(t *testing.T) {
		turn := newTestTurn()

		// Simulate implicit completion (model returned text without tool calls)
		turn.MarkTaskCompleted("natural-completion: model returned text without tool calls")

		if !turn.CompletionCriteria.TaskCompleted {
			t.Fatalf("expected TaskCompleted=true, got false")
		}
	})
}

// TestHasDiminishingReturns_SkipsWhenToolsAllFailed verifies that when
// ConsecutiveErrors > 0 (indicating tool failures), hasDiminishingReturns()
// returns false even when token gain is low. This prevents tool failures
// from being misclassified as "diminishing returns".
func TestHasDiminishingReturns_SkipsWhenToolsAllFailed(t *testing.T) {
	turn := newTestTurn()
	turn.CompletionCriteria.DiminishingReturnsEnabled = true
	turn.CompletionCriteria.DiminishingReturnsWindow = 3
	turn.CompletionCriteria.DiminishingReturnsMinGain = 500

	// Simulate 3 consecutive iterations with low token gain (below threshold)
	turn.CompletionCriteria.diminishingReturnsHistory = []int{10, 20, 15}

	// Without ConsecutiveErrors, this should trigger diminishing returns
	turn.CompletionCriteria.ConsecutiveErrors = 0
	if !turn.hasDiminishingReturns() {
		t.Fatal("expected hasDiminishingReturns()=true when ConsecutiveErrors=0 and all gains < minGain")
	}

	// With ConsecutiveErrors > 0 (tool failures), should skip detection
	turn.CompletionCriteria.ConsecutiveErrors = 1
	if turn.hasDiminishingReturns() {
		t.Fatal("expected hasDiminishingReturns()=false when ConsecutiveErrors > 0 (error recovery mode)")
	}

	// Verify the protection works with higher ConsecutiveErrors values
	turn.CompletionCriteria.ConsecutiveErrors = 3
	if turn.hasDiminishingReturns() {
		t.Fatal("expected hasDiminishingReturns()=false when ConsecutiveErrors=3")
	}
}

// TestRecordTokenGain_SkipsDuringErrorRecovery verifies that recordTokenGain
// does not append samples to diminishingReturnsHistory when ConsecutiveErrors > 0,
// but still updates prevTokens for accurate delta calculation after recovery.
func TestRecordTokenGain_SkipsDuringErrorRecovery(t *testing.T) {
	turn := newTestTurn()
	turn.CompletionCriteria.DiminishingReturnsEnabled = true
	turn.CompletionCriteria.DiminishingReturnsMinGain = 500
	turn.CompletionCriteria.diminishingReturnsPrevTokens = 1000

	// Simulate error recovery mode
	turn.CompletionCriteria.ConsecutiveErrors = 2

	// Add a message to simulate token increase
	turn.Messages = []llm.Message{
		{Role: "user", Content: "test message with some content"},
	}

	// Record token gain during error recovery
	turn.recordTokenGain()

	// Should NOT have added to history
	if len(turn.CompletionCriteria.diminishingReturnsHistory) != 0 {
		t.Fatalf("expected empty history during error recovery, got %d samples", len(turn.CompletionCriteria.diminishingReturnsHistory))
	}

	// But prevTokens should be updated
	if turn.CompletionCriteria.diminishingReturnsPrevTokens == 1000 {
		t.Fatal("expected prevTokens to be updated during error recovery")
	}
}

// TestUpdateConsecutiveErrors_ClearsDiminishingHistoryOnRecovery verifies that
// when tools recover (hasSuccess=true after ConsecutiveErrors > 0), the
// diminishingReturnsHistory is cleared to prevent polluted samples from
// triggering false positives.
func TestUpdateConsecutiveErrors_ClearsDiminishingHistoryOnRecovery(t *testing.T) {
	turn := newTestTurn()
	turn.CompletionCriteria.DiminishingReturnsEnabled = true
	turn.CompletionCriteria.DiminishingReturnsWindow = 3
	turn.CompletionCriteria.DiminishingReturnsMinGain = 500
	turn.CompletionCriteria.ConsecutiveErrors = 3

	// Simulate polluted history from error recovery period
	turn.CompletionCriteria.diminishingReturnsHistory = []int{10, 20, 15}

	// Simulate successful tool recovery
	toolResults := map[string]*interfaces.ToolResult{
		"call_1": {Success: true, LLMContent: "success"},
	}

	turn.updateConsecutiveErrorsFromToolResults(toolResults)

	// ConsecutiveErrors should be reset to 0
	if turn.CompletionCriteria.ConsecutiveErrors != 0 {
		t.Fatalf("expected ConsecutiveErrors=0 after recovery, got %d", turn.CompletionCriteria.ConsecutiveErrors)
	}

	// History should be cleared
	if turn.CompletionCriteria.diminishingReturnsHistory != nil {
		t.Fatalf("expected nil history after recovery, got %v", turn.CompletionCriteria.diminishingReturnsHistory)
	}

	// hasDiminishingReturns should return false (not enough samples)
	if turn.hasDiminishingReturns() {
		t.Fatal("expected hasDiminishingReturns()=false after history cleared")
	}
}

// TestHasDiminishingReturns_DualDeltaCheck_SinglePointPasses verifies that
// when the most recent iteration has significant progress (>= minGain),
// hasDiminishingReturns returns false even if earlier samples are low.
func TestHasDiminishingReturns_DualDeltaCheck_SinglePointPasses(t *testing.T) {
	turn := newTestTurn()
	turn.CompletionCriteria.DiminishingReturnsEnabled = true
	turn.CompletionCriteria.DiminishingReturnsWindow = 3
	turn.CompletionCriteria.DiminishingReturnsMinGain = 500

	// Last delta is >= 500 (passes dimension 1)
	turn.CompletionCriteria.diminishingReturnsHistory = []int{100, 80, 600}

	if turn.hasDiminishingReturns() {
		t.Fatal("expected hasDiminishingReturns()=false when most recent delta >= minGain")
	}
}

// TestHasDiminishingReturns_DualDeltaCheck_CumulativePasses verifies that
// when cumulative gain across the window >= minGain, hasDiminishingReturns
// returns false even if each individual sample is below threshold.
// This is the key improvement over the old implementation.
func TestHasDiminishingReturns_DualDeltaCheck_CumulativePasses(t *testing.T) {
	turn := newTestTurn()
	turn.CompletionCriteria.DiminishingReturnsEnabled = true
	turn.CompletionCriteria.DiminishingReturnsWindow = 3
	turn.CompletionCriteria.DiminishingReturnsMinGain = 500

	// Each individual delta < 500, but cumulative = 1420 >= 500 (passes dimension 2)
	// In the old implementation, this would incorrectly trigger diminishing returns
	turn.CompletionCriteria.diminishingReturnsHistory = []int{450, 480, 490}

	if turn.hasDiminishingReturns() {
		t.Fatal("expected hasDiminishingReturns()=false when cumulative gain >= minGain despite individual points being low")
	}
}

// TestHasDiminishingReturns_DualDeltaCheck_BothFail verifies that when both
// dimensions fail (last delta < minGain AND cumulative < minGain), the function
// correctly identifies true diminishing returns.
func TestHasDiminishingReturns_DualDeltaCheck_BothFail(t *testing.T) {
	turn := newTestTurn()
	turn.CompletionCriteria.DiminishingReturnsEnabled = true
	turn.CompletionCriteria.DiminishingReturnsWindow = 3
	turn.CompletionCriteria.DiminishingReturnsMinGain = 500

	// Last delta = 100 < 500, cumulative = 230 < 500 (both dimensions fail)
	turn.CompletionCriteria.diminishingReturnsHistory = []int{50, 80, 100}

	if !turn.hasDiminishingReturns() {
		t.Fatal("expected hasDiminishingReturns()=true when both single-point and cumulative are below threshold")
	}
}

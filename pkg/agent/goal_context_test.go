package agent

import (
	"strings"
	"sync"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestGoalContextStateTransitions(t *testing.T) {
	ctx := NewGoalContext(nil)
	if ctx.IsActive() {
		t.Fatal("new goal should not be active")
	}
	if err := ctx.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal failed: %v", err)
	}
	if !ctx.IsActive() {
		t.Fatal("goal should be active")
	}
	if got := ctx.Condition(); got != "all tests pass" {
		t.Fatalf("condition = %q", got)
	}
	ctx.MarkEvaluated(12, "not yet")
	if status := ctx.Status(); !strings.Contains(status, "not yet") {
		t.Fatalf("status missing reason: %q", status)
	}
	ctx.MarkAchieved("done")
	if ctx.IsActive() {
		t.Fatal("achieved goal should not remain active")
	}
	if ctx.Snapshot().AchievedAt == nil {
		t.Fatal("achieved time was not recorded")
	}
	ctx.Clear()
	if ctx.Condition() != "" {
		t.Fatal("goal was not cleared")
	}
}

func TestGoalContextConditionLengthLimit(t *testing.T) {
	ctx := NewGoalContext(&config.Config{Goal: &config.GoalConfig{MaxConditionLength: 3}})
	if err := ctx.SetGoal("abcd"); err == nil {
		t.Fatal("expected length error")
	}
}

func TestGoalContextConcurrentAccess(t *testing.T) {
	ctx := NewGoalContext(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ctx.SetGoal("finish")
			ctx.MarkEvaluated(1, "working")
			_ = ctx.Status()
			_ = ctx.Snapshot()
		}()
	}
	wg.Wait()
}

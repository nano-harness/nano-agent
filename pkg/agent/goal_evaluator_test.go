package agent

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

func TestGoalEvaluatorMet(t *testing.T) {
	client := llm.NewMockClient()
	client.DefaultResp = llm.MockResponse{Content: `{"met": true, "reason": "tests passed"}`}
	cfg := config.GoalConfig{}
	ev := NewGoalEvaluator(client, cfg)
	result, err := ev.Evaluate(context.Background(), "tests pass", []llm.Message{{Role: "assistant", Content: "tests passed"}})
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if !result.Met || result.Reason != "tests passed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGoalEvaluatorNotMet(t *testing.T) {
	client := llm.NewMockClient()
	client.DefaultResp = llm.MockResponse{Content: `{"met": false, "reason": "tests still failing"}`}
	cfg := config.GoalConfig{}
	ev := NewGoalEvaluator(client, cfg)
	result, err := ev.Evaluate(context.Background(), "tests pass", nil)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result.Met || result.Reason != "tests still failing" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGoalEvaluatorInvalidJSONDefaultsNotMet(t *testing.T) {
	client := llm.NewMockClient()
	client.DefaultResp = llm.MockResponse{Content: "not json"}
	cfg := config.GoalConfig{}
	ev := NewGoalEvaluator(client, cfg)
	result, err := ev.Evaluate(context.Background(), "tests pass", nil)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if result.Met {
		t.Fatalf("invalid JSON should not be met: %+v", result)
	}
}

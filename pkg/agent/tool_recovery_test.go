package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

type testToolExecutor struct {
	outcomes []testToolOutcome
	calls    int
	onCall   func(call int)
}

type testToolOutcome struct {
	result *interfaces.ToolResult
	err    error
}

func (e *testToolExecutor) Execute(ctx context.Context, name string, params map[string]interface{}) (*interfaces.ToolResult, error) {
	e.calls++
	if e.onCall != nil {
		e.onCall(e.calls)
	}
	idx := e.calls - 1
	if idx < 0 || idx >= len(e.outcomes) {
		return &interfaces.ToolResult{Success: true}, nil
	}
	return e.outcomes[idx].result, e.outcomes[idx].err
}

func TestToolRecovery_RetriesOnToolResultFailure(t *testing.T) {
	trs := NewToolRecoveryStrategy(nil)
	trs.SetToolPolicy("foo", ToolRetryPolicy{
		MaxRetries:        3,
		RetryDelay:        time.Nanosecond,
		BackoffMultiplier: 2,
		MaxDelay:          time.Nanosecond,
		JitterRatio:       0,
	})

	exec := &testToolExecutor{
		outcomes: []testToolOutcome{
			{
				result: &interfaces.ToolResult{
					Success:  false,
					Error:    "temporary network error",
					Metadata: map[string]interface{}{"code": "network"},
				},
				err: nil,
			},
			{
				result: &interfaces.ToolResult{
					Success: true,
				},
				err: nil,
			},
		},
	}

	res := trs.ExecuteWithRecovery(context.Background(), exec, ToolToExecute{
		ID:         "1",
		Name:       "foo",
		Parameters: map[string]interface{}{"x": 1},
	})

	if exec.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", exec.calls)
	}
	if res.Error != nil {
		t.Fatalf("expected success, got error: %v", res.Error)
	}
	if res.Attempts != 2 {
		t.Fatalf("expected Attempts=2, got %d", res.Attempts)
	}
	if res.Result == nil || !res.Result.Success {
		t.Fatalf("expected final tool result success")
	}
}

func TestToolRecovery_DoesNotRetryOnUnrecoverableCode(t *testing.T) {
	trs := NewToolRecoveryStrategy(nil)
	trs.SetToolPolicy("foo", ToolRetryPolicy{
		MaxRetries:        10,
		RetryDelay:        time.Nanosecond,
		BackoffMultiplier: 2,
		MaxDelay:          time.Nanosecond,
		JitterRatio:       0,
	})

	exec := &testToolExecutor{
		outcomes: []testToolOutcome{
			{
				result: &interfaces.ToolResult{
					Success:  false,
					Error:    "missing required parameters",
					Metadata: map[string]interface{}{"code": "missing_required_parameters"},
				},
				err: nil,
			},
		},
	}

	res := trs.ExecuteWithRecovery(context.Background(), exec, ToolToExecute{
		ID:   "1",
		Name: "foo",
	})

	if exec.calls != 1 {
		t.Fatalf("expected 1 call, got %d", exec.calls)
	}
	if res.Error == nil {
		t.Fatalf("expected error")
	}
	if res.Attempts != 1 {
		t.Fatalf("expected Attempts=1, got %d", res.Attempts)
	}
	// missing_required_parameters is now classified as business_failure
	if res.ErrorCategory != ErrorCategoryBusinessFailure {
		t.Fatalf("expected business_failure category, got %s", res.ErrorCategory)
	}
}

func TestToolRecovery_BusinessFailureNotRetried(t *testing.T) {
	trs := NewToolRecoveryStrategy(nil)
	trs.SetToolPolicy("foo", ToolRetryPolicy{
		MaxRetries:        5,
		RetryDelay:        time.Nanosecond,
		BackoffMultiplier: 2,
		MaxDelay:          time.Nanosecond,
		JitterRatio:       0,
	})

	exec := &testToolExecutor{
		outcomes: []testToolOutcome{
			{
				result: &interfaces.ToolResult{
					Success:  false,
					Error:    "file not found",
					Metadata: map[string]interface{}{"code": "file_not_found"},
				},
				err: nil,
			},
		},
	}

	res := trs.ExecuteWithRecovery(context.Background(), exec, ToolToExecute{
		ID:   "1",
		Name: "foo",
	})

	// business failure must not be retried — exactly 1 attempt
	if exec.calls != 1 {
		t.Fatalf("expected 1 call (no retry for business failure), got %d", exec.calls)
	}
	if res.Error == nil {
		t.Fatalf("expected error")
	}
	if res.ErrorCategory != ErrorCategoryBusinessFailure {
		t.Fatalf("expected business_failure category, got %s", res.ErrorCategory)
	}
}

func TestCategorizeToolResultFailureCode_BusinessFailures(t *testing.T) {
	businessCodes := []string{
		"file_not_found",
		"no_such_file",
		"permission_denied",
		"validation_error",
		"invalid_input",
		"invalid_arguments",
		"execution_failed",
		"command_failed",
		"syntax_error",
		"missing_required_parameters",
		"not_allowed",
		"tool_not_found",
	}

	for _, code := range businessCodes {
		category := categorizeToolResultFailureCode(code)
		if category != ErrorCategoryBusinessFailure {
			t.Errorf("code %q: expected business_failure, got %s", code, category)
		}
	}
}

func TestCategorizeToolResultFailureCode_EmptyCodeIsBusinessFailure(t *testing.T) {
	category := categorizeToolResultFailureCode("")
	if category != ErrorCategoryBusinessFailure {
		t.Fatalf("expected business_failure for empty code, got %s", category)
	}
}

func TestCategorizeToolResultFailureCode_UnknownCodeIsBusinessFailure(t *testing.T) {
	category := categorizeToolResultFailureCode("some_unknown_error_xyz")
	if category != ErrorCategoryBusinessFailure {
		t.Fatalf("expected business_failure for unknown code, got %s", category)
	}
}

func TestToolRecovery_MissingParamsIsBusinessFailure(t *testing.T) {
	trs := NewToolRecoveryStrategy(nil)
	err := &ToolResultFailureError{
		ToolName: "some_tool",
		Code:     "missing_required_parameters",
		Message:  "param x is required",
	}
	if !trs.IsBusinessFailure(err) {
		t.Fatalf("expected missing_required_parameters to be business failure")
	}
	if trs.IsRecoverable(err) {
		t.Fatalf("expected missing_required_parameters to not be recoverable (no retry)")
	}
}

func TestComputeBackoffDelay_FirstRetryUsesBaseDelay(t *testing.T) {
	policy := ToolRetryPolicy{
		MaxRetries:        3,
		RetryDelay:        10 * time.Millisecond,
		BackoffMultiplier: 2,
		MaxDelay:          time.Second,
		JitterRatio:       0,
	}

	if d := computeBackoffDelay(policy, 1); d != 0 {
		t.Fatalf("expected attempt 1 delay 0, got %v", d)
	}
	if d := computeBackoffDelay(policy, 2); d != 10*time.Millisecond {
		t.Fatalf("expected attempt 2 delay %v, got %v", 10*time.Millisecond, d)
	}
	if d := computeBackoffDelay(policy, 3); d != 20*time.Millisecond {
		t.Fatalf("expected attempt 3 delay %v, got %v", 20*time.Millisecond, d)
	}
}

func TestToolRecovery_CancelInterruptsBackoffWait(t *testing.T) {
	trs := NewToolRecoveryStrategy(nil)
	trs.SetToolPolicy("foo", ToolRetryPolicy{
		MaxRetries:        3,
		RetryDelay:        200 * time.Millisecond,
		BackoffMultiplier: 2,
		MaxDelay:          200 * time.Millisecond,
		JitterRatio:       0,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	exec := &testToolExecutor{
		outcomes: []testToolOutcome{
			{
				result: nil,
				err:    errors.New("timeout"),
			},
		},
		onCall: func(call int) {
			if call == 1 {
				go func() {
					time.Sleep(10 * time.Millisecond)
					cancel()
				}()
			}
		},
	}

	start := time.Now()
	res := trs.ExecuteWithRecovery(ctx, exec, ToolToExecute{
		ID:   "1",
		Name: "foo",
	})
	elapsed := time.Since(start)

	if elapsed >= 200*time.Millisecond {
		t.Fatalf("expected cancellation before full backoff delay, elapsed=%v", elapsed)
	}
	if res.Error == nil {
		t.Fatalf("expected error")
	}
	if res.ErrorCategory != ErrorCategoryFatal {
		t.Fatalf("expected fatal category, got %s", res.ErrorCategory)
	}
}

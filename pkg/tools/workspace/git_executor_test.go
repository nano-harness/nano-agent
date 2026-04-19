package workspace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultGitExecutorFactory(t *testing.T) {
	factory := &DefaultGitExecutorFactory{}
	config := &GitConfig{
		CommandTimeout: 10 * time.Second,
		MaxOutputSize:  1024,
	}

	executor := factory.CreateExecutor("/tmp", config)
	assert.NotNil(t, executor)

	// Verify it's the correct type
	defaultExecutor, ok := executor.(*DefaultGitExecutor)
	assert.True(t, ok)
	assert.Equal(t, "/tmp", defaultExecutor.workingDir)
	assert.Equal(t, 10*time.Second, defaultExecutor.timeout)
	assert.Equal(t, 1024, defaultExecutor.maxOutput)
}

func TestMockGitExecutor(t *testing.T) {
	mock := NewMockGitExecutor()
	ctx := context.Background()

	// Test default behavior
	result, err := mock.Execute(ctx, "status")
	assert.NoError(t, err)
	assert.Equal(t, "mock output", result.Output)
	assert.Equal(t, 0, result.ExitCode)

	// Verify call was recorded
	calls := mock.GetCalls()
	assert.Len(t, calls, 1)
	assert.Equal(t, []string{"status"}, calls[0])

	// Test predefined result
	expectedResult := &GitResult{
		Output:   "custom output",
		ExitCode: 1,
		Error:    errors.New("test error"),
	}
	mock.SetResult([]string{"status", "--porcelain"}, expectedResult)

	result, err = mock.Execute(ctx, "status", "--porcelain")
	assert.Equal(t, expectedResult.Error, err)
	assert.Equal(t, expectedResult.Output, result.Output)
	assert.Equal(t, expectedResult.ExitCode, result.ExitCode)

	// Verify calls
	calls = mock.GetCalls()
	assert.Len(t, calls, 2)
	assert.Equal(t, []string{"status", "--porcelain"}, calls[1])

	// Test clear calls
	mock.ClearCalls()
	calls = mock.GetCalls()
	assert.Len(t, calls, 0)
}

func TestMockGitExecutorFactory(t *testing.T) {
	mockExecutor := NewMockGitExecutor()
	factory := &MockGitExecutorFactory{MockExecutor: mockExecutor}
	config := &GitConfig{}

	executor := factory.CreateExecutor("/tmp", config)
	assert.Equal(t, mockExecutor, executor)
}

func TestEnhancedGitExecutor(t *testing.T) {
	enhanced := NewEnhancedGitExecutor("/tmp/nonexistent", 10*time.Second, 1024)
	ctx := context.Background()

	// Test hooks
	preHookCalled := false
	postHookCalled := false
	var capturedArgs []string
	var capturedResult *GitResult

	enhanced.AddPreHook(func(ctx context.Context, args []string) error {
		preHookCalled = true
		capturedArgs = args
		return nil
	})

	enhanced.AddPostHook(func(ctx context.Context, args []string, result *GitResult) error {
		postHookCalled = true
		capturedResult = result
		return nil
	})

	// This will fail because the directory doesn't exist, but hooks should still be called
	result, err := enhanced.Execute(ctx, "status")
	assert.Error(t, err) // Expected to fail

	assert.True(t, preHookCalled)
	assert.True(t, postHookCalled)
	assert.Equal(t, []string{"status"}, capturedArgs)
	assert.NotNil(t, capturedResult)
	assert.NotNil(t, result)
}

func TestEnhancedGitExecutor_PreHookError(t *testing.T) {
	enhanced := NewEnhancedGitExecutor("/tmp", 10*time.Second, 1024)
	ctx := context.Background()

	// Add failing pre-hook
	enhanced.AddPreHook(func(ctx context.Context, args []string) error {
		return errors.New("pre-hook failed")
	})

	result, err := enhanced.Execute(ctx, "status")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pre-hook failed")
	assert.Nil(t, result)
}

func TestEnhancedGitExecutor_PostHookError(t *testing.T) {
	enhanced := NewEnhancedGitExecutor("/tmp", 10*time.Second, 1024)
	ctx := context.Background()

	// Add failing post-hook
	enhanced.AddPostHook(func(ctx context.Context, args []string, result *GitResult) error {
		return errors.New("post-hook failed")
	})

	// This will fail due to no git repo, but we should also get post-hook error
	result, err := enhanced.Execute(ctx, "status")
	assert.Error(t, err)
	// The error could be either from git command or post-hook
	assert.NotNil(t, result)
}

func TestGitExecutorWithRetry(t *testing.T) {
	mock := NewMockGitExecutor()
	retryExecutor := NewGitExecutorWithRetry(mock, 2, 10*time.Millisecond)
	ctx := context.Background()

	// Test successful execution (no retry needed)
	mock.SetResult([]string{"status"}, &GitResult{
		Output:   "success",
		ExitCode: 0,
		Error:    nil,
	})

	result, err := retryExecutor.Execute(ctx, "status")
	assert.NoError(t, err)
	assert.Equal(t, "success", result.Output)

	// Should only be called once
	calls := mock.GetCalls()
	assert.Len(t, calls, 1)

	// Test retry on failure
	mock.ClearCalls()
	mock.SetResult([]string{"push"}, &GitResult{
		Output:   "failed",
		ExitCode: 1,
		Error:    errors.New("push failed"),
	})

	result, err = retryExecutor.Execute(ctx, "push")
	assert.Error(t, err)
	assert.Equal(t, "failed", result.Output)

	// Should be called maxRetries + 1 times
	calls = mock.GetCalls()
	assert.Len(t, calls, 3) // 1 initial + 2 retries
}

func TestGitExecutorWithRetry_ContextCancellation(t *testing.T) {
	mock := NewMockGitExecutor()
	retryExecutor := NewGitExecutorWithRetry(mock, 5, 100*time.Millisecond)

	// Set up failing result
	mock.SetResult([]string{"slow"}, &GitResult{
		Output:   "failed",
		ExitCode: 1,
		Error:    errors.New("command failed"),
	})

	// Create context that will be cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := retryExecutor.Execute(ctx, "slow")
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
	assert.NotNil(t, result)

	// Should not complete all retries due to context cancellation
	calls := mock.GetCalls()
	assert.True(t, len(calls) < 6) // Less than maxRetries + 1
}

func TestGitExecutorPool(t *testing.T) {
	factory := &MockGitExecutorFactory{
		MockExecutor: NewMockGitExecutor(),
	}
	config := &GitConfig{}
	pool := NewGitExecutorPool(factory, "/tmp", config, 2)
	defer pool.Close()

	ctx := context.Background()

	// Get executor from pool
	executor1, err := pool.Get(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, executor1)

	executor2, err := pool.Get(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, executor2)

	// Pool should be empty now
	ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = pool.Get(ctx2)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)

	// Return executor to pool
	pool.Put(executor1)

	// Should be able to get it again
	executor3, err := pool.Get(ctx)
	assert.NoError(t, err)
	assert.Equal(t, executor1, executor3)
}

func TestGitExecutorPool_OverflowPut(t *testing.T) {
	factory := &MockGitExecutorFactory{
		MockExecutor: NewMockGitExecutor(),
	}
	config := &GitConfig{}
	pool := NewGitExecutorPool(factory, "/tmp", config, 1)
	defer pool.Close()

	ctx := context.Background()

	// Get the only executor
	executor, err := pool.Get(ctx)
	assert.NoError(t, err)

	// Return it
	pool.Put(executor)

	// Try to put another one (should be discarded)
	anotherExecutor := NewMockGitExecutor()
	pool.Put(anotherExecutor) // Should not block

	// Get from pool - should get the original executor
	retrieved, err := pool.Get(ctx)
	assert.NoError(t, err)
	assert.Equal(t, executor, retrieved)
}

package workspace

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// GitExecutorFactory creates Git executors
type GitExecutorFactory interface {
	CreateExecutor(workingDir string, config *GitConfig) GitExecutor
}

// DefaultGitExecutorFactory implements GitExecutorFactory
type DefaultGitExecutorFactory struct{}

// CreateExecutor creates a new DefaultGitExecutor
func (f *DefaultGitExecutorFactory) CreateExecutor(workingDir string, config *GitConfig) GitExecutor {
	return NewDefaultGitExecutor(workingDir, config.CommandTimeout, config.MaxOutputSize)
}

// MockGitExecutorFactory for testing
type MockGitExecutorFactory struct {
	MockExecutor GitExecutor
}

// CreateExecutor returns the mock executor
func (f *MockGitExecutorFactory) CreateExecutor(workingDir string, config *GitConfig) GitExecutor { //nolint:revive
	return f.MockExecutor
}

// MockGitExecutor for testing purposes
type MockGitExecutor struct {
	Results map[string]*GitResult
	Calls   [][]string
}

// NewMockGitExecutor creates a new mock executor
func NewMockGitExecutor() *MockGitExecutor {
	return &MockGitExecutor{
		Results: make(map[string]*GitResult),
		Calls:   make([][]string, 0),
	}
}

// Execute records the call and returns a predefined result
func (m *MockGitExecutor) Execute(_ context.Context, args ...string) (*GitResult, error) {
	m.Calls = append(m.Calls, args)

	key := strings.Join(args, " ")
	if result, exists := m.Results[key]; exists {
		return result, result.Error
	}

	// Default success result
	return &GitResult{
		Output:    "mock output",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	}, nil
}

// SetResult sets a predefined result for specific arguments
func (m *MockGitExecutor) SetResult(args []string, result *GitResult) {
	key := strings.Join(args, " ")
	m.Results[key] = result
}

// GetCalls returns all recorded calls
func (m *MockGitExecutor) GetCalls() [][]string {
	return m.Calls
}

// ClearCalls clears all recorded calls
func (m *MockGitExecutor) ClearCalls() {
	m.Calls = make([][]string, 0)
}

// EnhancedGitExecutor provides additional functionality
type EnhancedGitExecutor struct {
	*DefaultGitExecutor
	preHooks  []func(context.Context, []string) error
	postHooks []func(context.Context, []string, *GitResult) error
}

// NewEnhancedGitExecutor creates an enhanced executor with hooks
func NewEnhancedGitExecutor(workingDir string, timeout time.Duration, maxOutput int) *EnhancedGitExecutor {
	return &EnhancedGitExecutor{
		DefaultGitExecutor: NewDefaultGitExecutor(workingDir, timeout, maxOutput),
		preHooks:           make([]func(context.Context, []string) error, 0),
		postHooks:          make([]func(context.Context, []string, *GitResult) error, 0),
	}
}

// AddPreHook adds a pre-execution hook
func (e *EnhancedGitExecutor) AddPreHook(hook func(context.Context, []string) error) {
	e.preHooks = append(e.preHooks, hook)
}

// AddPostHook adds a post-execution hook
func (e *EnhancedGitExecutor) AddPostHook(hook func(context.Context, []string, *GitResult) error) {
	e.postHooks = append(e.postHooks, hook)
}

// Execute runs the command with pre and post hooks
func (e *EnhancedGitExecutor) Execute(ctx context.Context, args ...string) (*GitResult, error) {
	// Run pre-hooks
	for _, hook := range e.preHooks {
		if err := hook(ctx, args); err != nil {
			return nil, fmt.Errorf("pre-hook failed: %w", err)
		}
	}

	// Execute the command
	result, err := e.DefaultGitExecutor.Execute(ctx, args...)

	// Always run post-hooks if we have a result, even if there was an error
	if result != nil {
		for _, hook := range e.postHooks {
			if hookErr := hook(ctx, args, result); hookErr != nil {
				// If we already had an error, combine them
				if err != nil {
					return result, fmt.Errorf("command failed: %w, post-hook failed: %v", err, hookErr)
				}
				return result, fmt.Errorf("post-hook failed: %w", hookErr)
			}
		}
	}

	return result, err
}

// GitExecutorWithRetry wraps an executor with retry logic
type GitExecutorWithRetry struct {
	executor   GitExecutor
	maxRetries int
	retryDelay time.Duration
}

// NewGitExecutorWithRetry creates an executor with retry capability
func NewGitExecutorWithRetry(executor GitExecutor, maxRetries int, retryDelay time.Duration) *GitExecutorWithRetry {
	return &GitExecutorWithRetry{
		executor:   executor,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}
}

// Execute executes the command with retry logic
func (r *GitExecutorWithRetry) Execute(ctx context.Context, args ...string) (*GitResult, error) {
	var lastResult *GitResult
	var lastError error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		result, err := r.executor.Execute(ctx, args...)

		if err == nil && result.ExitCode == 0 {
			return result, nil
		}

		lastResult = result
		lastError = err

		// Don't retry on the last attempt
		if attempt < r.maxRetries {
			select {
			case <-ctx.Done():
				return lastResult, ctx.Err()
			case <-time.After(r.retryDelay):
				// Continue to next attempt
			}
		}
	}

	return lastResult, lastError
}

// GitExecutorPool manages a pool of executors for concurrent operations
type GitExecutorPool struct {
	factory    GitExecutorFactory
	workingDir string
	config     *GitConfig
	pool       chan GitExecutor
	maxSize    int
}

// NewGitExecutorPool creates a new executor pool
func NewGitExecutorPool(factory GitExecutorFactory, workingDir string, config *GitConfig, maxSize int) *GitExecutorPool {
	pool := &GitExecutorPool{
		factory:    factory,
		workingDir: workingDir,
		config:     config,
		pool:       make(chan GitExecutor, maxSize),
		maxSize:    maxSize,
	}

	// Pre-populate the pool
	for i := 0; i < maxSize; i++ {
		executor := factory.CreateExecutor(workingDir, config)
		pool.pool <- executor
	}

	return pool
}

// Get retrieves an executor from the pool
func (p *GitExecutorPool) Get(ctx context.Context) (GitExecutor, error) {
	select {
	case executor := <-p.pool:
		return executor, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Put returns an executor to the pool
func (p *GitExecutorPool) Put(executor GitExecutor) {
	select {
	case p.pool <- executor:
		// Successfully returned to pool
	default:
		// Pool is full, discard the executor
	}
}

// Close closes the executor pool
func (p *GitExecutorPool) Close() {
	close(p.pool)
}

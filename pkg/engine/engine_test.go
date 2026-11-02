package engine_test

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalTestConfig returns a Config suitable for unit tests: it bypasses
// expensive auto-detection and uses a fixed system prompt.
func minimalTestConfig() *config.Config {
	return &config.Config{
		APIKey:             "test-key",
		Model:              "test-model",
		CustomSystemPrompt: "You are a test assistant.",
	}
}

func TestNew_nilConfig(t *testing.T) {
	_, err := engine.New(nil, nil)
	assert.Error(t, err, "New with nil config should return an error")
}

func TestNew_minimalConfig(t *testing.T) {
	cfg := minimalTestConfig()
	e, err := engine.New(cfg, nil)
	require.NoError(t, err)
	assert.NotNil(t, e)
	assert.NotNil(t, e.Agent, "Agent should be initialised")
	assert.Nil(t, e.Scheduler, "Scheduler should not be initialised by default")
}

func TestNew_WithScheduler(t *testing.T) {
	cfg := minimalTestConfig()
	e, err := engine.New(cfg, nil, engine.WithScheduler())
	require.NoError(t, err)
	assert.NotNil(t, e)
	assert.NotNil(t, e.Agent, "Agent should be initialised")
	assert.NotNil(t, e.Scheduler, "Scheduler should be initialised when requested")
}

func TestStartShutdown_WithoutScheduler(t *testing.T) {
	cfg := minimalTestConfig()
	e, err := engine.New(cfg, nil)
	require.NoError(t, err)
	require.Nil(t, e.Scheduler)

	require.NoError(t, e.Start())
	require.NoError(t, e.Shutdown())
}

func TestStartShutdown(t *testing.T) {
	cfg := minimalTestConfig()
	e, err := engine.New(cfg, nil, engine.WithScheduler())
	require.NoError(t, err)

	require.NoError(t, e.Start())
	require.NoError(t, e.Shutdown())
}

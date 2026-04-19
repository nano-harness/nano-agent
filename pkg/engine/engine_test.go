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
	assert.NotNil(t, e.Scheduler, "Scheduler should be initialised")
	assert.Nil(t, e.Watcher, "Watcher should be nil when not configured")
}

func TestNew_watcherEnabled(t *testing.T) {
	cfg := minimalTestConfig()
	cfg.Watcher = &config.WatcherConfig{
		Enabled: true,
		Rules: []config.WatchRule{
			{
				ID:       "test-rule",
				Source:   "shell",
				Event:    "custom",
				Command:  "echo {{.OUTPUT}}",
				Interval: 60_000_000_000, // 1 minute
			},
		},
	}

	e, err := engine.New(cfg, nil)
	require.NoError(t, err)
	assert.NotNil(t, e.Watcher, "Watcher should be initialised when enabled")

	rules := e.Watcher.ListRules()
	assert.Len(t, rules, 1, "should have one rule loaded from config")
	assert.Equal(t, "test-rule", rules[0].ID)
}

func TestStartShutdown(t *testing.T) {
	cfg := minimalTestConfig()
	e, err := engine.New(cfg, nil)
	require.NoError(t, err)

	require.NoError(t, e.Start())
	require.NoError(t, e.Shutdown())
}

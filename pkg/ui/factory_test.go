package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFactory_Create tests the basic factory creation logic.
func TestFactory_Create(t *testing.T) {
	cfg := Config{
		APIBaseURL: "http://localhost:8080",
		WorkingDir: "/tmp/test",
		ShowBanner: false,
	}

	factory := NewFactory(cfg)
	require.NotNil(t, factory)

	t.Run("BubbleTea mode", func(t *testing.T) {
		adapter, err := factory.Create(ModeBubbleTea)
		require.NoError(t, err)
		require.NotNil(t, adapter)

		_, ok := adapter.(*BubbleTeaAdapter)
		assert.True(t, ok, "Expected BubbleTeaAdapter type")
	})

	t.Run("TView mode", func(t *testing.T) {
		adapter, err := factory.Create(ModeTView)
		require.NoError(t, err)
		require.NotNil(t, adapter)

		_, ok := adapter.(*TViewAdapter)
		assert.True(t, ok, "Expected TViewAdapter type")
	})

	t.Run("Auto mode defaults to BubbleTea", func(t *testing.T) {
		adapter, err := factory.Create(ModeAuto)
		require.NoError(t, err)
		require.NotNil(t, adapter)

		_, ok := adapter.(*BubbleTeaAdapter)
		assert.True(t, ok, "Auto mode should default to BubbleTea")
	})

	t.Run("Unknown mode returns error", func(t *testing.T) {
		adapter, err := factory.Create(Mode("invalid"))
		assert.Error(t, err)
		assert.Nil(t, adapter)
		assert.Contains(t, err.Error(), "unknown mode")
	})
}

// TestMode_Constants verifies mode constant values.
func TestMode_Constants(t *testing.T) {
	assert.Equal(t, Mode("tview"), ModeTView)
	assert.Equal(t, Mode("bubbletea"), ModeBubbleTea)
	assert.Equal(t, Mode("auto"), ModeAuto)
}

// TestConfig_Creation verifies config can be created with different values.
func TestConfig_Creation(t *testing.T) {
	t.Run("Default config", func(t *testing.T) {
		cfg := Config{}
		assert.Empty(t, cfg.APIBaseURL)
		assert.Empty(t, cfg.WorkingDir)
		assert.False(t, cfg.ShowBanner)
	})

	t.Run("Custom config", func(t *testing.T) {
		cfg := Config{
			APIBaseURL: "https://api.example.com",
			WorkingDir: "/custom/path",
			ShowBanner: true,
		}
		assert.Equal(t, "https://api.example.com", cfg.APIBaseURL)
		assert.Equal(t, "/custom/path", cfg.WorkingDir)
		assert.True(t, cfg.ShowBanner)
	})
}

// TestAdapter_Interface verifies adapters implement the Adapter interface.
func TestAdapter_Interface(t *testing.T) {
	cfg := Config{WorkingDir: "/tmp"}
	factory := NewFactory(cfg)

	t.Run("BubbleTeaAdapter implements Adapter", func(t *testing.T) {
		adapter, err := factory.Create(ModeBubbleTea)
		require.NoError(t, err)

		var _ Adapter = adapter
	})

	t.Run("TViewAdapter implements Adapter", func(t *testing.T) {
		adapter, err := factory.Create(ModeTView)
		require.NoError(t, err)

		var _ Adapter = adapter
	})
}

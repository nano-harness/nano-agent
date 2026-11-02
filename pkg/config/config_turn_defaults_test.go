package config

import "testing"

func TestDefaultConfig_TurnConfigExists(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Turn == nil {
		t.Fatalf("expected default turn config")
	}
	// TurnExecutionConfig is now empty - implicit completion is the default behavior
}

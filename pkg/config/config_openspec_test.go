package config

import "testing"

func TestDefaultConfig_OpenSpecEnabled(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.OpenSpec == nil {
		t.Fatal("expected OpenSpec config to be non-nil")
	}
	if !cfg.OpenSpec.Enabled {
		t.Error("expected OpenSpec to be enabled by default")
	}
	if cfg.OpenSpec.RootDir != "openspec" {
		t.Errorf("expected RootDir 'openspec', got %q", cfg.OpenSpec.RootDir)
	}
	if cfg.OpenSpec.DefaultSchema != "spec-driven" {
		t.Errorf("expected DefaultSchema 'spec-driven', got %q", cfg.OpenSpec.DefaultSchema)
	}
	if !cfg.OpenSpec.AutoDetect {
		t.Error("expected AutoDetect to be true by default")
	}
	if cfg.OpenSpec.ApplyMode != "sequential" {
		t.Errorf("expected ApplyMode 'sequential', got %q", cfg.OpenSpec.ApplyMode)
	}
	if !cfg.OpenSpec.VerifyBeforeArchive {
		t.Error("expected VerifyBeforeArchive to be true by default")
	}
	if !cfg.OpenSpec.InjectContext {
		t.Error("expected InjectContext to be true by default")
	}
	if cfg.OpenSpec.MaxArtifactSize != 512*1024 {
		t.Errorf("expected MaxArtifactSize 512KB, got %d", cfg.OpenSpec.MaxArtifactSize)
	}
}

func TestDefaultConfig_EnabledToolsIncludeOpenSpec(t *testing.T) {
	cfg := DefaultConfig()

	expectedTools := []string{
		"opsx_status",
		"opsx_read_artifact",
		"opsx_write_artifact",
		"opsx_update_task",
		"opsx_list_changes",
	}

	toolSet := make(map[string]bool)
	for _, tool := range cfg.EnabledTools {
		toolSet[tool] = true
	}

	for _, expected := range expectedTools {
		if !toolSet[expected] {
			t.Errorf("expected %q in EnabledTools", expected)
		}
	}
}

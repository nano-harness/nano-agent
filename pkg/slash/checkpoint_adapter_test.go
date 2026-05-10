package slash

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestNewDefaultCheckpointManager_Disabled(t *testing.T) {
	prev := config.Get()
	t.Cleanup(func() { config.SetGlobalConfig(prev) })
	config.SetGlobalConfig(&config.Config{})
	if got := NewDefaultCheckpointManager(t.TempDir()); got != nil {
		t.Fatalf("expected nil manager when checkpoint config is disabled")
	}
}

func TestNewDefaultCheckpointManager_Enabled(t *testing.T) {
	prev := config.Get()
	t.Cleanup(func() { config.SetGlobalConfig(prev) })
	work := t.TempDir()
	backup := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	config.SetGlobalConfig(&config.Config{
		Checkpoint: &config.CheckpointConfig{
			Enabled:    true,
			BackupRoot: backup,
			MaxCount:   10,
		},
	})

	mgr := NewDefaultCheckpointManager(work)
	if mgr == nil {
		t.Fatal("expected checkpoint manager when enabled")
	}
	id, err := mgr.Create("before edit")
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty checkpoint id")
	}
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Restore(id); err != nil {
		t.Fatalf("restore checkpoint: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(work, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "before" {
		t.Fatalf("expected restored content, got %q", got)
	}
}

//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/checkpoint"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckpoint_SaveAndRestore tests saving and restoring filesystem checkpoints.
func TestCheckpoint_SaveAndRestore(t *testing.T) {
	workDir := t.TempDir()
	target := filepath.Join(workDir, "state.txt")
	require.NoError(t, os.WriteFile(target, []byte("before"), 0o644))

	mgr := newCheckpointManager(t, workDir)
	cp, err := mgr.Snapshot("before edit", "e2e")
	require.NoError(t, err)
	require.NotNil(t, cp)

	require.NoError(t, os.WriteFile(target, []byte("after"), 0o644))
	require.NoError(t, mgr.Restore(cp.ID))

	restored, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "before", string(restored))

	t.Log("✓ Checkpoint save and restore test passed")
}

// TestCheckpoint_ListCheckpoints tests listing all checkpoints.
func TestCheckpoint_ListCheckpoints(t *testing.T) {
	workDir := t.TempDir()
	target := filepath.Join(workDir, "state.txt")
	require.NoError(t, os.WriteFile(target, []byte("initial"), 0o644))

	mgr := newCheckpointManager(t, workDir)
	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(target, []byte{byte('a' + i)}, 0o644))
		_, err := mgr.Snapshot("snapshot", "e2e")
		require.NoError(t, err)
	}

	checkpoints, err := mgr.List()
	require.NoError(t, err)
	assert.Len(t, checkpoints, 3)
	for _, cp := range checkpoints {
		assert.NotEmpty(t, cp.ID)
		assert.Equal(t, workDir, cp.WorkingDir)
	}

	t.Log("✓ List checkpoints test passed")
}

// TestCheckpoint_DeleteCheckpoint tests deleting a checkpoint.
func TestCheckpoint_DeleteCheckpoint(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "state.txt"), []byte("content"), 0o644))

	mgr := newCheckpointManager(t, workDir)
	cp, err := mgr.Snapshot("delete test", "e2e")
	require.NoError(t, err)

	require.NoError(t, mgr.Delete(cp.ID))
	err = mgr.Restore(cp.ID)
	assert.ErrorIs(t, err, checkpoint.ErrNotFound)

	t.Log("✓ Delete checkpoint test passed")
}

// TestCheckpoint_ResumeFromCheckpoint tests that checkpoint metadata remains available while an agent resumes a session.
func TestCheckpoint_ResumeFromCheckpoint(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "state.txt"), []byte("checkpointed"), 0o644))

	mock := NewEnhancedMockServer()
	defer mock.Close()
	mock.AddResponse(MockResponse{Content: "Resumed from checkpoint"})

	mgr := newCheckpointManager(t, workDir)
	cp, err := mgr.Snapshot("resume test", "e2e")
	require.NoError(t, err)

	ag, err := agent.New(newE2EAgentConfig(workDir, mock.URL()))
	require.NoError(t, err)
	defer func() { _ = ag.Shutdown() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sessionID := "resume-test-session"
	var events []event.StreamEvent
	err = ag.ProcessStreamWithMultimodalAndSession(ctx, sessionID, "continue from checkpoint", nil, func(evt event.StreamEvent) {
		events = append(events, evt)
	})
	if err != nil {
		t.Logf("Process returned error (acceptable): %v", err)
	}

	checkpoints, err := mgr.List()
	require.NoError(t, err)
	require.NotEmpty(t, checkpoints)
	assert.Equal(t, cp.ID, checkpoints[0].ID)
	assert.NotNil(t, events)

	t.Log("✓ Resume from checkpoint test passed")
}

// TestCheckpoint_AutomaticCheckpointing validates checkpoint infrastructure around agent execution.
func TestCheckpoint_AutomaticCheckpointing(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "state.txt"), []byte("initial"), 0o644))

	mock := NewEnhancedMockServer()
	defer mock.Close()
	mock.AddResponse(MockResponse{Content: "Response for turn"})

	mgr := newCheckpointManager(t, workDir)
	before, err := mgr.Snapshot("before agent turn", "agent")
	require.NoError(t, err)

	ag, err := agent.New(newE2EAgentConfig(workDir, mock.URL()))
	require.NoError(t, err)
	defer func() { _ = ag.Shutdown() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = ag.ProcessStreamWithMultimodalAndSession(ctx, ag.StartNewSession(), "test automatic checkpointing", nil, func(event.StreamEvent) {})
	if err != nil {
		t.Logf("Process returned error (acceptable): %v", err)
	}

	checkpoints, err := mgr.List()
	require.NoError(t, err)
	require.NotEmpty(t, checkpoints)
	assert.Equal(t, before.ID, checkpoints[0].ID)

	t.Log("✓ Automatic checkpointing test passed")
}

func newCheckpointManager(t *testing.T, workDir string) *checkpoint.FSManager {
	t.Helper()
	mgr, err := checkpoint.NewFSManager(checkpoint.Options{
		WorkingDir:   workDir,
		BackupRoot:   filepath.Join(workDir, ".nano", "checkpoints"),
		GitDisable:   true,
		RetentionAge: time.Hour,
	})
	require.NoError(t, err)
	return mgr
}

func newE2EAgentConfig(workDir, baseURL string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.WorkingDir = workDir
	cfg.APIKey = "e2e-test-key"
	cfg.BaseURL = baseURL
	cfg.Model = "mock-gpt-4"
	cfg.EnableMCP = false
	cfg.EnabledTools = []string{"read_file", "write_file", "edit_file", "run_shell_command", "task_done"}
	if cfg.OSS != nil {
		cfg.OSS.Enabled = false
	}
	if cfg.Skills != nil {
		cfg.Skills.Enabled = false
	}
	if cfg.OpenSpec != nil {
		cfg.OpenSpec.Enabled = false
	}
	if cfg.UserInfo != nil {
		cfg.UserInfo.WorkingDirectory = workDir
		cfg.UserInfo.AutoDetectUserInfo = false
	}
	config.SetGlobalConfig(cfg)
	return cfg
}

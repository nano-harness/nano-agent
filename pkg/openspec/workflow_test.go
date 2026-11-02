package openspec

import (
	"testing"
)

func TestWorkflowEngine_HandlePropose(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	cmd := &Command{
		Type:       CommandPropose,
		ChangeName: "test-feature",
		Args:       []string{"test-feature"},
		RawInput:   "/opsx:propose test-feature",
	}

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Change == nil {
		t.Fatal("expected change to be created")
	}
	if result.Change.Name != "test-feature" {
		t.Errorf("change name = %q, want 'test-feature'", result.Change.Name)
	}
	if result.UserMessageOverride == "" {
		t.Error("expected UserMessageOverride to be set")
	}
	if result.SystemPromptAddition == "" {
		t.Error("expected SystemPromptAddition to be set")
	}
}

func TestWorkflowEngine_HandleProposeNoName(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	cmd := &Command{
		Type: CommandPropose,
	}

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result.UserMessageOverride == "" {
		t.Error("expected prompt for change name")
	}
}

func TestWorkflowEngine_HandleNew(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	cmd := &Command{
		Type:       CommandNew,
		ChangeName: "new-feature",
	}

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result.StatusMessage == "" {
		t.Error("expected status message")
	}
}

func TestWorkflowEngine_HandleStatus(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	// No changes yet
	cmd := &Command{Type: CommandStatus}
	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result.StatusMessage == "" {
		t.Error("expected status message")
	}

	// Create a change
	am.CreateChange("status-test", "spec-driven")
	result, err = engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result.StatusMessage == "" {
		t.Error("expected status message with change info")
	}
}

func TestWorkflowEngine_HandleApplyNoTasks(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	am.CreateChange("no-tasks", "spec-driven")

	cmd := &Command{
		Type:       CommandApply,
		ChangeName: "no-tasks",
	}

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	// Should tell user to create tasks first
	if result.UserMessageOverride == "" {
		t.Error("expected message about missing tasks")
	}
}

func TestWorkflowEngine_HandleApplyWithTasks(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	am.CreateChange("with-tasks", "spec-driven")
	am.WriteArtifact("with-tasks", "proposal", "# Proposal\n")
	am.WriteArtifact("with-tasks", "design", "# Design\n")
	am.WriteArtifact("with-tasks", "tasks", "# Tasks\n- [ ] 1.1 Do something\n- [ ] 1.2 Do another\n")

	cmd := &Command{
		Type:       CommandApply,
		ChangeName: "with-tasks",
	}

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result.UserMessageOverride == "" {
		t.Error("expected apply instructions")
	}
	if result.SystemPromptAddition == "" {
		t.Error("expected system prompt context")
	}
}

func TestWorkflowEngine_HandleArchive(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	am.CreateChange("to-archive", "spec-driven")

	cmd := &Command{
		Type:       CommandArchive,
		ChangeName: "to-archive",
	}

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result.StatusMessage == "" {
		t.Error("expected archive confirmation")
	}

	// Should no longer be listed
	changes, _ := am.ListChanges()
	for _, name := range changes {
		if name == "to-archive" {
			t.Error("archived change should not appear")
		}
	}
}

func TestWorkflowEngine_HandleExplore(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	cmd := &Command{
		Type: CommandExplore,
		Args: []string{"how", "to", "handle", "auth"},
	}

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result.UserMessageOverride == "" {
		t.Error("expected explore instructions")
	}
}

func TestWorkflowEngine_HandleVerify(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	am.CreateChange("verify-test", "spec-driven")

	cmd := &Command{
		Type:       CommandVerify,
		ChangeName: "verify-test",
	}

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result.UserMessageOverride == "" {
		t.Error("expected verify instructions")
	}
}

func TestWorkflowEngine_ResolveChangeName(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	engine := NewWorkflowEngine(am, "spec-driven")

	// No changes → error
	cmd := &Command{Type: CommandContinue}
	result, err := engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	if result.UserMessageOverride == "" {
		t.Error("expected error about no changes")
	}

	// Single change → auto-resolve
	am.CreateChange("only-change", "spec-driven")
	cmd = &Command{Type: CommandContinue}
	result, err = engine.HandleCommand(cmd)
	if err != nil {
		t.Fatalf("HandleCommand failed: %v", err)
	}
	// Should work since only one change
	if result.UserMessageOverride == "" && result.StatusMessage == "" {
		t.Error("expected some response")
	}
}

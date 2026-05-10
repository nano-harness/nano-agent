package swarm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/team"
)

func TestWithTeammate_FromContext_RoundTrip(t *testing.T) {
	// Create a teammate identity
	identity := &TeammateIdentity{
		AgentID:          "researcher@alpha",
		AgentName:        "researcher",
		TeamName:         "alpha",
		Color:            "#FF5733",
		PermissionMode:   "acceptEdits",
		AllowedTools:     []string{"read_file", "run_shell_command"},
		Model:            "gpt-5-mini",
		ContextProviders: []string{"memory", "skills"},
		PlanModeRequired: true,
		ParentSessionID:  "session-123",
	}

	// Add to context
	ctx := WithTeammate(context.Background(), identity)

	// Retrieve from context
	retrieved, ok := FromContext(ctx)
	if !ok {
		t.Fatal("expected to find teammate identity in context")
	}

	// Verify all fields
	if retrieved.AgentID != identity.AgentID {
		t.Errorf("AgentID mismatch: got %q, want %q", retrieved.AgentID, identity.AgentID)
	}
	if retrieved.AgentName != identity.AgentName {
		t.Errorf("AgentName mismatch: got %q, want %q", retrieved.AgentName, identity.AgentName)
	}
	if retrieved.TeamName != identity.TeamName {
		t.Errorf("TeamName mismatch: got %q, want %q", retrieved.TeamName, identity.TeamName)
	}
	if retrieved.Color != identity.Color {
		t.Errorf("Color mismatch: got %q, want %q", retrieved.Color, identity.Color)
	}
	if retrieved.PermissionMode != identity.PermissionMode {
		t.Errorf("PermissionMode mismatch: got %q, want %q", retrieved.PermissionMode, identity.PermissionMode)
	}
	if len(retrieved.AllowedTools) != 2 || retrieved.AllowedTools[0] != "read_file" || retrieved.AllowedTools[1] != "run_shell_command" {
		t.Errorf("AllowedTools mismatch: got %#v", retrieved.AllowedTools)
	}
	if retrieved.Model != "gpt-5-mini" {
		t.Errorf("Model = %q, want gpt-5-mini", retrieved.Model)
	}
	if len(retrieved.ContextProviders) != 2 || retrieved.ContextProviders[0] != "memory" || retrieved.ContextProviders[1] != "skills" {
		t.Errorf("ContextProviders mismatch: got %#v", retrieved.ContextProviders)
	}
	if retrieved.PlanModeRequired != identity.PlanModeRequired {
		t.Errorf("PlanModeRequired mismatch: got %v, want %v", retrieved.PlanModeRequired, identity.PlanModeRequired)
	}
	if retrieved.ParentSessionID != identity.ParentSessionID {
		t.Errorf("ParentSessionID mismatch: got %q, want %q", retrieved.ParentSessionID, identity.ParentSessionID)
	}
}

func TestFromContext_EmptyContext(t *testing.T) {
	// Empty context should return nil, false
	_, ok := FromContext(context.Background())
	if ok {
		t.Error("expected no teammate identity in empty context")
	}
}

func TestFromContext_NilContext(t *testing.T) {
	// Nil context should return nil, false
	_, ok := FromContext(nil)
	if ok {
		t.Error("expected no teammate identity in nil context")
	}
}

func TestIsTeammate_WithIdentity(t *testing.T) {
	identity := &TeammateIdentity{
		AgentID:   "researcher@alpha",
		AgentName: "researcher",
		TeamName:  "alpha",
	}
	ctx := WithTeammate(context.Background(), identity)

	if !IsTeammate(ctx) {
		t.Error("expected IsTeammate to return true for context with identity")
	}
}

func TestIsTeammate_WithoutIdentity(t *testing.T) {
	if IsTeammate(context.Background()) {
		t.Error("expected IsTeammate to return false for empty context")
	}
}

func TestIsTeamLead_WithIdentity(t *testing.T) {
	identity := &TeammateIdentity{
		AgentID:   "researcher@alpha",
		AgentName: "researcher",
		TeamName:  "alpha",
	}
	ctx := WithTeammate(context.Background(), identity)

	if IsTeamLead(ctx) {
		t.Error("expected IsTeamLead to return false for context with teammate identity")
	}
}

func TestIsTeamLead_WithoutIdentity(t *testing.T) {
	if !IsTeamLead(context.Background()) {
		t.Error("expected IsTeamLead to return true for empty context")
	}
}

func TestGetAgentName_Teammate(t *testing.T) {
	identity := &TeammateIdentity{
		AgentID:   "researcher@alpha",
		AgentName: "researcher",
		TeamName:  "alpha",
	}
	ctx := WithTeammate(context.Background(), identity)

	name := GetAgentName(ctx)
	if name != "researcher" {
		t.Errorf("expected agent name %q, got %q", "researcher", name)
	}
}

func TestGetAgentName_TeamLead(t *testing.T) {
	name := GetAgentName(context.Background())
	if name != "team-lead" {
		t.Errorf("expected agent name %q, got %q", "team-lead", name)
	}
}

func TestNestedContext_NewIdentityOverridesOld(t *testing.T) {
	// First identity
	identity1 := &TeammateIdentity{
		AgentID:   "researcher@alpha",
		AgentName: "researcher",
		TeamName:  "alpha",
	}
	ctx1 := WithTeammate(context.Background(), identity1)

	// Second identity (nested)
	identity2 := &TeammateIdentity{
		AgentID:   "coder@alpha",
		AgentName: "coder",
		TeamName:  "alpha",
	}
	ctx2 := WithTeammate(ctx1, identity2)

	// Verify second context has second identity
	retrieved, ok := FromContext(ctx2)
	if !ok {
		t.Fatal("expected to find teammate identity in nested context")
	}
	if retrieved.AgentID != identity2.AgentID {
		t.Errorf("expected nested context to have second identity, got %q", retrieved.AgentID)
	}

	// Verify first context still has first identity (immutability)
	retrieved1, ok := FromContext(ctx1)
	if !ok {
		t.Fatal("expected to find teammate identity in first context")
	}
	if retrieved1.AgentID != identity1.AgentID {
		t.Errorf("expected first context to remain unchanged, got %q", retrieved1.AgentID)
	}
}

func TestWithTeammate_NilParent(t *testing.T) {
	identity := &TeammateIdentity{
		AgentID:   "researcher@alpha",
		AgentName: "researcher",
		TeamName:  "alpha",
	}

	// Should not panic with nil parent
	ctx := WithTeammate(nil, identity)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	retrieved, ok := FromContext(ctx)
	if !ok {
		t.Fatal("expected to find teammate identity")
	}
	if retrieved.AgentID != identity.AgentID {
		t.Errorf("AgentID mismatch: got %q, want %q", retrieved.AgentID, identity.AgentID)
	}
}

type captureRunner struct {
	identity *TeammateIdentity
}

func (r *captureRunner) Run(_ context.Context, identity *TeammateIdentity, _ string) error {
	r.identity = identity
	return nil
}

func TestSpawnInProcessCarriesAllowedTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := team.CreateTeam("alpha", "test", "team-lead@alpha", "lead-alpha-chat-1"); err != nil {
		t.Fatal(err)
	}
	runner := &captureRunner{}
	handle, err := SpawnInProcess(context.Background(), SpawnOptions{
		TeamName:      "alpha",
		Name:          "reviewer",
		InitialPrompt: "review",
		AllowedTools:  []string{"read_file"},
		Runner:        runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-handle.Done; err != nil {
		t.Fatal(err)
	}
	if runner.identity == nil || len(runner.identity.AllowedTools) != 1 || runner.identity.AllowedTools[0] != "read_file" {
		t.Fatalf("allowed tools not propagated: %+v", runner.identity)
	}
}

func TestSpawnInProcessCarriesModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := team.CreateTeam("alpha", "test", "team-lead@alpha", "lead-alpha-chat-1"); err != nil {
		t.Fatal(err)
	}
	runner := &captureRunner{}
	handle, err := SpawnInProcess(context.Background(), SpawnOptions{
		TeamName:      "alpha",
		Name:          "reviewer",
		InitialPrompt: "review",
		Model:         "gpt-5-mini",
		Runner:        runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-handle.Done; err != nil {
		t.Fatal(err)
	}
	if runner.identity == nil || runner.identity.Model != "gpt-5-mini" {
		t.Fatalf("model not propagated: %+v", runner.identity)
	}
	created, err := team.ReadTeam("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Members) != 1 || created.Members[0].Model != "gpt-5-mini" {
		t.Fatalf("member model not recorded: %+v", created.Members)
	}
}

func TestSpawnInProcessCarriesContextProviders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := team.CreateTeam("alpha", "test", "team-lead@alpha", "lead-alpha-chat-1"); err != nil {
		t.Fatal(err)
	}
	runner := &captureRunner{}
	handle, err := SpawnInProcess(context.Background(), SpawnOptions{
		TeamName:         "alpha",
		Name:             "reviewer",
		InitialPrompt:    "review",
		ContextProviders: []string{"memory", "skills"},
		Runner:           runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-handle.Done; err != nil {
		t.Fatal(err)
	}
	if runner.identity == nil || len(runner.identity.ContextProviders) != 2 {
		t.Fatalf("context providers not propagated: %+v", runner.identity)
	}
	created, err := team.ReadTeam("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Members) != 1 || len(created.Members[0].ContextProviders) != 2 {
		t.Fatalf("member context providers not recorded: %+v", created.Members)
	}
}

type waitForCancelRunner struct {
	seenCtx context.Context
}

func (r *waitForCancelRunner) Run(ctx context.Context, _ *TeammateIdentity, _ string) error {
	r.seenCtx = ctx
	<-ctx.Done()
	return ctx.Err()
}

func TestSpawnInProcessEnforcesMaxRuntime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := team.CreateTeam("alpha", "test", "team-lead@alpha", "lead-alpha-chat-1"); err != nil {
		t.Fatal(err)
	}
	runner := &waitForCancelRunner{}
	handle, err := SpawnInProcess(context.Background(), SpawnOptions{
		TeamName:      "alpha",
		Name:          "reviewer",
		InitialPrompt: "review",
		MaxRuntimeSec: 1,
		Runner:        runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-handle.Done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Done error = %v, want DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("teammate did not stop after max runtime")
	}
	if runner.seenCtx == nil {
		t.Fatal("runner did not receive context")
	}
	created, err := team.ReadTeam("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Members) != 1 || created.Members[0].IsActive {
		t.Fatalf("teammate was not deactivated after max runtime: %+v", created.Members)
	}
	if created.Members[0].MaxRuntimeSec != 1 {
		t.Fatalf("member MaxRuntimeSec = %d, want 1", created.Members[0].MaxRuntimeSec)
	}
}

package openspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetSchema(t *testing.T) {
	s := GetSchema("spec-driven")
	if s == nil {
		t.Fatal("expected spec-driven schema")
	}
	if s.Name != "spec-driven" {
		t.Fatalf("expected name 'spec-driven', got %q", s.Name)
	}
	if len(s.Artifacts) != 4 {
		t.Fatalf("expected 4 artifacts, got %d", len(s.Artifacts))
	}

	// Verify dependency graph
	ids := make(map[string]SchemaArtifact)
	for _, a := range s.Artifacts {
		ids[a.ID] = a
	}
	if len(ids["proposal"].Requires) != 0 {
		t.Error("proposal should have no dependencies")
	}
	if len(ids["specs"].Requires) != 1 || ids["specs"].Requires[0] != "proposal" {
		t.Error("specs should depend on proposal only")
	}
	if len(ids["design"].Requires) != 1 || ids["design"].Requires[0] != "proposal" {
		t.Error("design should depend on proposal only")
	}
	if len(ids["tasks"].Requires) != 2 {
		t.Error("tasks should depend on specs and design")
	}
}

func TestGetSchemaUnknown(t *testing.T) {
	s := GetSchema("nonexistent")
	if s != nil {
		t.Fatal("expected nil for unknown schema")
	}
}

func TestGetReadyArtifacts(t *testing.T) {
	schema := GetSchema("spec-driven")

	tests := []struct {
		name     string
		statuses map[string]ArtifactStatus
		want     []string
	}{
		{
			name:     "nothing created",
			statuses: map[string]ArtifactStatus{},
			want:     []string{"proposal"},
		},
		{
			name: "proposal created",
			statuses: map[string]ArtifactStatus{
				"proposal": ArtifactStatusCreated,
			},
			want: []string{"specs", "design"},
		},
		{
			name: "proposal and specs created",
			statuses: map[string]ArtifactStatus{
				"proposal": ArtifactStatusCreated,
				"specs":    ArtifactStatusCreated,
			},
			want: []string{"design"},
		},
		{
			name: "all except tasks",
			statuses: map[string]ArtifactStatus{
				"proposal": ArtifactStatusCreated,
				"specs":    ArtifactStatusCreated,
				"design":   ArtifactStatusCreated,
			},
			want: []string{"tasks"},
		},
		{
			name: "all created",
			statuses: map[string]ArtifactStatus{
				"proposal": ArtifactStatusCreated,
				"specs":    ArtifactStatusCreated,
				"design":   ArtifactStatusCreated,
				"tasks":    ArtifactStatusCreated,
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GetReadyArtifacts(schema, tc.statuses)
			if len(got) != len(tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("index %d: expected %q, got %q", i, w, got[i])
				}
			}
		})
	}
}

func TestGetArtifactOrder(t *testing.T) {
	schema := GetSchema("spec-driven")
	order := GetArtifactOrder(schema)

	// proposal must come first, tasks must come last
	if order[0] != "proposal" {
		t.Errorf("expected proposal first, got %q", order[0])
	}
	if order[len(order)-1] != "tasks" {
		t.Errorf("expected tasks last, got %q", order[len(order)-1])
	}
}

func TestArtifactManager_CreateAndGetChange(t *testing.T) {
	tmpDir := t.TempDir()

	am := NewArtifactManager("openspec", tmpDir)
	change, err := am.CreateChange("test-change", "spec-driven")
	if err != nil {
		t.Fatalf("CreateChange failed: %v", err)
	}
	if change.Name != "test-change" {
		t.Errorf("expected name 'test-change', got %q", change.Name)
	}
	if change.Schema != "spec-driven" {
		t.Errorf("expected schema 'spec-driven', got %q", change.Schema)
	}

	// Verify directory was created
	changePath := filepath.Join(tmpDir, "openspec", "changes", "test-change")
	if _, err := os.Stat(changePath); err != nil {
		t.Errorf("change directory not created: %v", err)
	}

	// Verify .openspec.yaml was created
	metaPath := filepath.Join(changePath, ".openspec.yaml")
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf(".openspec.yaml not created: %v", err)
	}

	// Get the change back
	change2, err := am.GetChange("test-change")
	if err != nil {
		t.Fatalf("GetChange failed: %v", err)
	}
	if change2.Name != "test-change" {
		t.Errorf("expected name 'test-change', got %q", change2.Name)
	}

	// Proposal should be ready (no dependencies)
	if change2.Artifacts["proposal"].Status != ArtifactStatusReady {
		t.Errorf("expected proposal to be ready, got %v", change2.Artifacts["proposal"].Status)
	}
}

func TestArtifactManager_CreateChangeDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)

	_, err := am.CreateChange("dup-test", "spec-driven")
	if err != nil {
		t.Fatalf("first CreateChange failed: %v", err)
	}

	_, err = am.CreateChange("dup-test", "spec-driven")
	if err == nil {
		t.Fatal("expected error for duplicate change")
	}
}

func TestArtifactManager_WriteAndReadArtifact(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)

	_, err := am.CreateChange("rw-test", "spec-driven")
	if err != nil {
		t.Fatalf("CreateChange failed: %v", err)
	}

	// Write proposal
	content := "# Proposal: Test\n\n## Intent\nTesting\n"
	if err := am.WriteArtifact("rw-test", "proposal", content); err != nil {
		t.Fatalf("WriteArtifact failed: %v", err)
	}

	// Read it back
	got, err := am.ReadArtifact("rw-test", "proposal")
	if err != nil {
		t.Fatalf("ReadArtifact failed: %v", err)
	}
	if got != content {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}

	// Now get the change again — proposal should be created
	change, _ := am.GetChange("rw-test")
	if change.Artifacts["proposal"].Status != ArtifactStatusCreated {
		t.Errorf("expected proposal to be created, got %v", change.Artifacts["proposal"].Status)
	}
	// Specs and design should now be ready
	if change.Artifacts["specs"].Status != ArtifactStatusReady {
		t.Errorf("expected specs to be ready after proposal, got %v", change.Artifacts["specs"].Status)
	}
	if change.Artifacts["design"].Status != ArtifactStatusReady {
		t.Errorf("expected design to be ready after proposal, got %v", change.Artifacts["design"].Status)
	}
}

func TestArtifactManager_ListChanges(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)

	// No changes yet
	names, err := am.ListChanges()
	if err != nil {
		t.Fatalf("ListChanges failed: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected 0 changes, got %d", len(names))
	}

	// Create some changes
	am.CreateChange("a-change", "spec-driven")
	am.CreateChange("b-change", "spec-driven")

	names, err = am.ListChanges()
	if err != nil {
		t.Fatalf("ListChanges failed: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(names))
	}
	if names[0] != "a-change" || names[1] != "b-change" {
		t.Errorf("unexpected order: %v", names)
	}
}

func TestArtifactManager_ArchiveChange(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)

	am.CreateChange("to-archive", "spec-driven")

	err := am.ArchiveChange("to-archive")
	if err != nil {
		t.Fatalf("ArchiveChange failed: %v", err)
	}

	// Should no longer be listed
	names, _ := am.ListChanges()
	for _, n := range names {
		if n == "to-archive" {
			t.Error("archived change should not appear in list")
		}
	}

	// Archive directory should exist
	archiveDir := filepath.Join(tmpDir, "openspec", "changes", "archive")
	entries, _ := os.ReadDir(archiveDir)
	if len(entries) == 0 {
		t.Error("expected archived change in archive directory")
	}
}

func TestArtifactManager_HasOpenSpecDir(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)

	if am.HasOpenSpecDir() {
		t.Error("should not detect openspec dir before creation")
	}

	am.EnsureDirectories()
	if !am.HasOpenSpecDir() {
		t.Error("should detect openspec dir after creation")
	}
}

func TestArtifactManager_GetChangeStatus(t *testing.T) {
	tmpDir := t.TempDir()
	am := NewArtifactManager("openspec", tmpDir)
	am.CreateChange("status-test", "spec-driven")

	// Write tasks with some complete
	tasks := "# Tasks\n\n## Setup\n- [x] 1.1 Init project\n- [ ] 1.2 Add deps\n- [x] 1.3 Configure\n"
	am.WriteArtifact("status-test", "tasks", tasks)

	status, err := am.GetChangeStatus("status-test")
	if err != nil {
		t.Fatalf("GetChangeStatus failed: %v", err)
	}
	if status.TasksTotal != 3 {
		t.Errorf("expected 3 tasks, got %d", status.TasksTotal)
	}
	if status.TasksCompleted != 2 {
		t.Errorf("expected 2 completed, got %d", status.TasksCompleted)
	}
}

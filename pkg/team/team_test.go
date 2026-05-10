package team

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCreateAndReadTeam(t *testing.T) {
	// Use a temp directory for testing
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "test-team"
	desc := "A test team"
	leadAgentID := "team-lead@test-team"
	leadSessionID := "session-123"

	// Create team
	team, err := CreateTeam(name, desc, leadAgentID, leadSessionID)
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Verify fields
	if team.Name != name {
		t.Errorf("Name: got %q, want %q", team.Name, name)
	}
	if team.Description != desc {
		t.Errorf("Description: got %q, want %q", team.Description, desc)
	}
	if team.LeadAgentID != leadAgentID {
		t.Errorf("LeadAgentID: got %q, want %q", team.LeadAgentID, leadAgentID)
	}
	if team.LeadSessionID != leadSessionID {
		t.Errorf("LeadSessionID: got %q, want %q", team.LeadSessionID, leadSessionID)
	}
	if team.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if len(team.Members) != 0 {
		t.Errorf("Members: got %d, want 0", len(team.Members))
	}

	// Read team back
	readTeam, err := ReadTeam(name)
	if err != nil {
		t.Fatalf("ReadTeam failed: %v", err)
	}

	// Verify fields match
	if readTeam.Name != team.Name {
		t.Errorf("Name mismatch after read: got %q, want %q", readTeam.Name, team.Name)
	}
	if readTeam.Description != team.Description {
		t.Errorf("Description mismatch after read")
	}
	if readTeam.LeadAgentID != team.LeadAgentID {
		t.Errorf("LeadAgentID mismatch after read")
	}
	if readTeam.LeadSessionID != team.LeadSessionID {
		t.Errorf("LeadSessionID mismatch after read")
	}
}

func TestCreateTeam_DuplicateName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "duplicate-team"
	_, err := CreateTeam(name, "First", "lead1@team", "session1")
	if err != nil {
		t.Fatalf("First CreateTeam failed: %v", err)
	}

	// Second create should fail
	_, err = CreateTeam(name, "Second", "lead2@team", "session2")
	if err == nil {
		t.Fatal("Expected error when creating duplicate team")
	}
}

func TestDeleteTeam(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "delete-me"
	_, err := CreateTeam(name, "To be deleted", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Verify team exists
	_, err = ReadTeam(name)
	if err != nil {
		t.Fatalf("ReadTeam failed: %v", err)
	}

	// Delete team
	err = DeleteTeam(name)
	if err != nil {
		t.Fatalf("DeleteTeam failed: %v", err)
	}

	// Verify team no longer exists
	_, err = ReadTeam(name)
	if err == nil {
		t.Fatal("Expected error reading deleted team")
	}
}

func TestAddMember(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "member-team"
	_, err := CreateTeam(name, "Team with members", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Add a member
	member := TeamMember{
		AgentID:   "researcher@member-team",
		Name:      "researcher",
		Color:     "#FF5733",
		Mode:      "auto",
		IsActive:  true,
		SessionID: "session-r1",
		Kind:      KindInProcess,
		Sandbox: SandboxPolicy{
			Backend:   "docker",
			Lifecycle: "task",
			Scope:     "subagent",
			SessionID: "session-r1",
		},
	}

	err = AddMember(name, member)
	if err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	// Read back and verify
	team, err := ReadTeam(name)
	if err != nil {
		t.Fatalf("ReadTeam failed: %v", err)
	}

	if len(team.Members) != 1 {
		t.Fatalf("Expected 1 member, got %d", len(team.Members))
	}

	m := team.Members[0]
	if m.AgentID != member.AgentID {
		t.Errorf("AgentID: got %q, want %q", m.AgentID, member.AgentID)
	}
	if m.Name != member.Name {
		t.Errorf("Name: got %q, want %q", m.Name, member.Name)
	}
	if m.Color != member.Color {
		t.Errorf("Color: got %q, want %q", m.Color, member.Color)
	}
	if m.IsActive != member.IsActive {
		t.Errorf("IsActive: got %v, want %v", m.IsActive, member.IsActive)
	}
	if m.Kind != member.Kind {
		t.Errorf("Kind: got %q, want %q", m.Kind, member.Kind)
	}
	if m.Sandbox.Backend != "docker" || m.Sandbox.Lifecycle != "task" || m.Sandbox.Scope != "subagent" {
		t.Errorf("Sandbox: got %#v", m.Sandbox)
	}
}

func TestAddMember_DuplicateAgentID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "dup-member-team"
	_, err := CreateTeam(name, "Team", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	member := TeamMember{
		AgentID:   "researcher@team",
		Name:      "researcher",
		IsActive:  true,
		SessionID: "session1",
		Kind:      KindInProcess,
	}

	// First add should succeed
	err = AddMember(name, member)
	if err != nil {
		t.Fatalf("First AddMember failed: %v", err)
	}

	// Second add with same AgentID should fail
	err = AddMember(name, member)
	if err == nil {
		t.Fatal("Expected error when adding duplicate member")
	}
}

func TestRemoveMemberByAgentID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "remove-member-team"
	_, err := CreateTeam(name, "Team", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Add two members
	member1 := TeamMember{
		AgentID:   "researcher@team",
		Name:      "researcher",
		IsActive:  true,
		SessionID: "session1",
		Kind:      KindInProcess,
	}
	member2 := TeamMember{
		AgentID:   "coder@team",
		Name:      "coder",
		IsActive:  true,
		SessionID: "session2",
		Kind:      KindInProcess,
	}

	_ = AddMember(name, member1)
	_ = AddMember(name, member2)

	// Remove first member
	err = RemoveMemberByAgentID(name, member1.AgentID)
	if err != nil {
		t.Fatalf("RemoveMemberByAgentID failed: %v", err)
	}

	// Verify only one member remains
	team, err := ReadTeam(name)
	if err != nil {
		t.Fatalf("ReadTeam failed: %v", err)
	}

	if len(team.Members) != 1 {
		t.Fatalf("Expected 1 member, got %d", len(team.Members))
	}

	if team.Members[0].AgentID != member2.AgentID {
		t.Errorf("Wrong member remains: got %q, want %q", team.Members[0].AgentID, member2.AgentID)
	}
}

func TestSetMemberActive(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "active-team"
	_, err := CreateTeam(name, "Team", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	member := TeamMember{
		AgentID:   "researcher@team",
		Name:      "researcher",
		IsActive:  true,
		SessionID: "session1",
		Kind:      KindInProcess,
	}
	_ = AddMember(name, member)

	// Set inactive
	err = SetMemberActive(name, member.AgentID, false)
	if err != nil {
		t.Fatalf("SetMemberActive failed: %v", err)
	}

	// Verify
	team, err := ReadTeam(name)
	if err != nil {
		t.Fatalf("ReadTeam failed: %v", err)
	}

	if team.Members[0].IsActive {
		t.Error("Expected member to be inactive")
	}
}

func TestSetMemberMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "mode-team"
	_, err := CreateTeam(name, "Team", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	member := TeamMember{
		AgentID:   "researcher@team",
		Name:      "researcher",
		Mode:      "auto",
		IsActive:  true,
		SessionID: "session1",
		Kind:      KindInProcess,
	}
	_ = AddMember(name, member)

	// Change mode
	newMode := "manual"
	err = SetMemberMode(name, member.AgentID, newMode)
	if err != nil {
		t.Fatalf("SetMemberMode failed: %v", err)
	}

	// Verify
	team, err := ReadTeam(name)
	if err != nil {
		t.Fatalf("ReadTeam failed: %v", err)
	}

	if team.Members[0].Mode != newMode {
		t.Errorf("Mode: got %q, want %q", team.Members[0].Mode, newMode)
	}
}

func TestListAllTeams(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create multiple teams
	_, _ = CreateTeam("team1", "First", "lead1@team", "session1")
	_, _ = CreateTeam("team2", "Second", "lead2@team", "session2")
	_, _ = CreateTeam("team3", "Third", "lead3@team", "session3")

	teams, err := ListAllTeams()
	if err != nil {
		t.Fatalf("ListAllTeams failed: %v", err)
	}

	if len(teams) != 3 {
		t.Fatalf("Expected 3 teams, got %d", len(teams))
	}

	// Verify team names (order not guaranteed)
	names := make(map[string]bool)
	for _, team := range teams {
		names[team.Name] = true
	}

	expectedNames := []string{"team1", "team2", "team3"}
	for _, name := range expectedNames {
		if !names[name] {
			t.Errorf("Expected to find team %q", name)
		}
	}
}

func TestListAllTeams_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	teams, err := ListAllTeams()
	if err != nil {
		t.Fatalf("ListAllTeams failed: %v", err)
	}

	if len(teams) != 0 {
		t.Errorf("Expected 0 teams, got %d", len(teams))
	}
}

func TestPathHelpers(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Test TeamsRoot
	root := TeamsRoot()
	expected := filepath.Join(tmpDir, ".nano", "teams")
	if root != expected {
		t.Errorf("TeamsRoot: got %q, want %q", root, expected)
	}

	// Test TeamDir
	teamDir := TeamDir("myteam")
	expected = filepath.Join(root, "myteam")
	if teamDir != expected {
		t.Errorf("TeamDir: got %q, want %q", teamDir, expected)
	}

	// Test ConfigPath
	configPath := ConfigPath("myteam")
	expected = filepath.Join(teamDir, "config.json")
	if configPath != expected {
		t.Errorf("ConfigPath: got %q, want %q", configPath, expected)
	}

	// Test InboxPath
	inboxPath := InboxPath("myteam", "researcher")
	expected = filepath.Join(teamDir, "inboxes", "researcher.json")
	if inboxPath != expected {
		t.Errorf("InboxPath: got %q, want %q", inboxPath, expected)
	}
}

func TestConcurrentAddMember(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "concurrent-team"
	_, err := CreateTeam(name, "Team", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Add 100 members concurrently
	var wg sync.WaitGroup
	successCount := make(chan int, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			member := TeamMember{
				AgentID:   fmt.Sprintf("member%d@team", id),
				Name:      fmt.Sprintf("member%d", id),
				IsActive:  true,
				SessionID: fmt.Sprintf("session%d", id),
				Kind:      KindInProcess,
			}
			if err := AddMember(name, member); err == nil {
				successCount <- 1
			}
		}(i)
	}

	wg.Wait()
	close(successCount)

	// Count successful additions
	count := 0
	for range successCount {
		count++
	}

	// Verify at least most members were added (allow some race conditions)
	// With proper locking, all 100 should succeed
	if count < 90 {
		t.Errorf("Too many failed concurrent additions: only %d/100 succeeded", count)
	}

	// Verify final member count matches successful additions
	team, err := ReadTeam(name)
	if err != nil {
		t.Fatalf("ReadTeam failed: %v", err)
	}

	if len(team.Members) != count {
		t.Errorf("Member count mismatch: expected %d, got %d", count, len(team.Members))
	}
}

func TestValidation_EmptyTeamName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := CreateTeam("", "Desc", "lead@team", "session")
	if err == nil {
		t.Fatal("Expected error for empty team name")
	}
}

func TestValidation_EmptyLeadAgentID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := CreateTeam("team", "Desc", "", "session")
	if err == nil {
		t.Fatal("Expected error for empty lead agent ID")
	}
}

func TestValidation_EmptyMemberName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := CreateTeam("team", "Desc", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	member := TeamMember{
		AgentID:   "researcher@team",
		Name:      "", // Empty name
		IsActive:  true,
		SessionID: "session1",
		Kind:      KindInProcess,
	}

	err = AddMember("team", member)
	if err == nil {
		t.Fatal("Expected error for empty member name")
	}
}

func TestInboxesDirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	name := "inbox-team"
	team, err := CreateTeam(name, "Team", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	// Verify inboxes directory was created
	inboxesDir := filepath.Join(TeamDir(name), "inboxes")
	info, err := os.Stat(inboxesDir)
	if err != nil {
		t.Fatalf("Inboxes directory not created: %v", err)
	}

	if !info.IsDir() {
		t.Error("Inboxes path is not a directory")
	}

	// Write team again to ensure directory is maintained
	err = WriteTeam(team)
	if err != nil {
		t.Fatalf("WriteTeam failed: %v", err)
	}

	// Verify directory still exists
	_, err = os.Stat(inboxesDir)
	if err != nil {
		t.Fatalf("Inboxes directory lost after WriteTeam: %v", err)
	}
}

func TestReadTeam_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := ReadTeam("nonexistent")
	if err == nil {
		t.Fatal("Expected error reading non-existent team")
	}
}

func TestRemoveMemberByAgentID_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := CreateTeam("team", "Team", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	err = RemoveMemberByAgentID("team", "nonexistent@team")
	if err == nil {
		t.Fatal("Expected error removing non-existent member")
	}
}

func TestSetMemberActive_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := CreateTeam("team", "Team", "lead@team", "session")
	if err != nil {
		t.Fatalf("CreateTeam failed: %v", err)
	}

	err = SetMemberActive("team", "nonexistent@team", false)
	if err == nil {
		t.Fatal("Expected error setting active for non-existent member")
	}
}

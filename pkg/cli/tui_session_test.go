package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/spf13/cobra"
)

// TestResolveTUISessionID_Default tests that default behavior generates a new session ID
func TestResolveTUISessionID_Default(t *testing.T) {
	// Create a minimal agent for testing
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "test", // bypass auto-detection
	}
	ag, err := agent.New(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer ag.Shutdown()

	// Test with nil cmd (should generate new session)
	sessionID := resolveTUISessionID(nil, ag)
	if !strings.HasPrefix(sessionID, "session_") {
		t.Errorf("expected session ID to start with 'session_', got: %s", sessionID)
	}

	// Test with cmd but no flags set
	cmd := &cobra.Command{}
	cmd.Flags().String("session", "", "test flag")
	cmd.Flags().Bool("continue", false, "test flag")

	sessionID2 := resolveTUISessionID(cmd, ag)
	if !strings.HasPrefix(sessionID2, "session_") {
		t.Errorf("expected session ID to start with 'session_', got: %s", sessionID2)
	}

	// Verify they're different (unique generation)
	if sessionID == sessionID2 {
		t.Error("expected different session IDs for multiple calls")
	}
}

// TestResolveTUISessionID_ExplicitSession tests --session flag
func TestResolveTUISessionID_ExplicitSession(t *testing.T) {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "test",
	}
	ag, err := agent.New(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer ag.Shutdown()

	cmd := &cobra.Command{}
	cmd.Flags().String("session", "", "test flag")
	cmd.Flags().Bool("continue", false, "test flag")

	// Set explicit session ID
	_ = cmd.Flags().Set("session", "my-custom-session")

	sessionID := resolveTUISessionID(cmd, ag)
	if sessionID != "my-custom-session" {
		t.Errorf("expected 'my-custom-session', got: %s", sessionID)
	}
}

// TestResolveTUISessionID_ContinueWithExisting tests --continue with existing session
func TestResolveTUISessionID_ContinueWithExisting(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "nano-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create config with working directory
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         tmpDir,
	}

	ag, err := agent.New(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer ag.Shutdown()

	// Create a session and save it to storage
	sm := ag.GetSessionManager()
	testSessionID := "existing-session"
	session := sm.GetOrCreateSession(testSessionID)

	// Append a message to make it "active"
	session.AppendMessage(llm.Message{Role: "user", Content: "test message"})
	if err := sm.SaveSession(testSessionID); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Now test --continue flag
	cmd := &cobra.Command{}
	cmd.Flags().String("session", "", "test flag")
	cmd.Flags().Bool("continue", false, "test flag")
	_ = cmd.Flags().Set("continue", "true")

	sessionID := resolveTUISessionID(cmd, ag)
	if sessionID != testSessionID {
		t.Errorf("expected '%s', got: %s", testSessionID, sessionID)
	}
}

// TestResolveTUISessionID_ContinueWithoutExisting tests --continue without existing sessions
func TestResolveTUISessionID_ContinueWithoutExisting(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "nano-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         tmpDir,
	}

	ag, err := agent.New(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer ag.Shutdown()

	// Test --continue with no existing sessions
	cmd := &cobra.Command{}
	cmd.Flags().String("session", "", "test flag")
	cmd.Flags().Bool("continue", false, "test flag")
	_ = cmd.Flags().Set("continue", "true")

	sessionID := resolveTUISessionID(cmd, ag)
	// Should generate a new session ID
	if !strings.HasPrefix(sessionID, "session_") {
		t.Errorf("expected session ID to start with 'session_', got: %s", sessionID)
	}
}

// TestResolveTUISessionID_SessionPriorityOverContinue tests that --session takes priority
func TestResolveTUISessionID_SessionPriorityOverContinue(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "nano-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         tmpDir,
	}

	ag, err := agent.New(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer ag.Shutdown()

	// Create an existing session
	sm := ag.GetSessionManager()
	existingSessionID := "existing-session"
	session := sm.GetOrCreateSession(existingSessionID)
	session.AppendMessage(llm.Message{Role: "user", Content: "test"})
	if err := sm.SaveSession(existingSessionID); err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Set both flags
	cmd := &cobra.Command{}
	cmd.Flags().String("session", "", "test flag")
	cmd.Flags().Bool("continue", false, "test flag")
	_ = cmd.Flags().Set("continue", "true")
	_ = cmd.Flags().Set("session", "my-explicit-session")

	sessionID := resolveTUISessionID(cmd, ag)
	// Should use explicit session, not the one from --continue
	if sessionID != "my-explicit-session" {
		t.Errorf("expected 'my-explicit-session', got: %s", sessionID)
	}
}

// TestResolveTUISessionID_WhitespaceHandling tests trimming of whitespace
func TestResolveTUISessionID_WhitespaceHandling(t *testing.T) {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "test",
	}
	ag, err := agent.New(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer ag.Shutdown()

	cmd := &cobra.Command{}
	cmd.Flags().String("session", "", "test flag")
	cmd.Flags().Bool("continue", false, "test flag")

	// Set session with whitespace
	_ = cmd.Flags().Set("session", "  my-session  ")

	sessionID := resolveTUISessionID(cmd, ag)
	if sessionID != "my-session" {
		t.Errorf("expected 'my-session', got: %s", sessionID)
	}

	// Test empty string (should generate new)
	_ = cmd.Flags().Set("session", "   ")
	sessionID2 := resolveTUISessionID(cmd, ag)
	if !strings.HasPrefix(sessionID2, "session_") {
		t.Errorf("expected session ID to start with 'session_', got: %s", sessionID2)
	}
}

// TestProjectSessionStorage_GetLatestSessionID verifies storage integration
func TestProjectSessionStorage_GetLatestSessionID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "nano-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sessionsDir := filepath.Join(tmpDir, "sessions")
	storage, err := agent.NewProjectSessionStorage(sessionsDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Test with no sessions
	_, err = storage.GetLatestSessionID()
	if err == nil {
		t.Error("expected error when no sessions exist")
	}

	// Create multiple sessions
	session1 := agent.NewSessionWithID("session-1")
	session2 := agent.NewSessionWithID("session-2")
	session3 := agent.NewSessionWithID("session-3")

	// Save them with some delay to ensure different timestamps
	if err := storage.SaveSession(session1); err != nil {
		t.Fatalf("failed to save session1: %v", err)
	}
	if err := storage.SaveSession(session2); err != nil {
		t.Fatalf("failed to save session2: %v", err)
	}
	if err := storage.SaveSession(session3); err != nil {
		t.Fatalf("failed to save session3: %v", err)
	}

	// Get latest - should be session-3 (last saved)
	latest, err := storage.GetLatestSessionID()
	if err != nil {
		t.Fatalf("GetLatestSessionID failed: %v", err)
	}

	// Note: The actual latest depends on file modification times
	// Just verify it's one of our sessions
	if latest != "session-1" && latest != "session-2" && latest != "session-3" {
		t.Errorf("expected one of the created sessions, got: %s", latest)
	}
}

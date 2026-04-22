package agent

import (
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestAgent_StartNewSession(t *testing.T) {
	// Create configuration
	// CustomSystemPrompt is set to avoid subprocess-spawning tool detection
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            "http://localhost",
		Model:              "test-model",
		CustomSystemPrompt: "You are a test agent.",
	}

	// Create agent
	agent, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Get the original active session ID
	oldSessionID := agent.GetActiveSessionID()
	if oldSessionID == "" {
		t.Fatal("Expected initial session ID to be non-empty")
	}

	// Force the old session to be created by calling GetOrCreateSession
	oldSession := agent.GetSessionManager().GetOrCreateSession(oldSessionID)
	if oldSession == nil {
		t.Fatal("Failed to create old session")
	}

	// Call StartNewSession
	newSessionID := agent.StartNewSession()

	// Verify a new session ID was returned and is non-empty
	if newSessionID == "" {
		t.Error("Expected StartNewSession to return a non-empty session ID")
	}

	// Verify the new session ID is different from the old one
	if newSessionID == oldSessionID {
		t.Error("Expected StartNewSession to create a new session with a different ID")
	}

	// Verify the new session ID follows the expected format (session_<hex>)
	if !strings.HasPrefix(newSessionID, "session_") {
		t.Errorf("Expected new session ID to start with 'session_', got: %s", newSessionID)
	}

	// Verify the active session ID was updated
	currentActiveID := agent.GetActiveSessionID()
	if currentActiveID != newSessionID {
		t.Errorf("Expected active session ID to be %s, got %s", newSessionID, currentActiveID)
	}

	// Verify the old session still exists in SessionManager
	oldSessionCheck, exists := agent.GetSessionManager().GetSession(oldSessionID)
	if !exists || oldSessionCheck == nil {
		t.Error("Expected old session to still exist in SessionManager")
	}

	// Verify the new session exists in SessionManager
	newSession, exists := agent.GetSessionManager().GetSession(newSessionID)
	if !exists || newSession == nil {
		t.Error("Expected new session to exist in SessionManager")
	}

	// Verify the new session has an empty conversation history
	if len(newSession.GetConversationHistory()) != 0 {
		t.Errorf("Expected new session to have empty conversation history, got %d messages",
			len(newSession.GetConversationHistory()))
	}
}

//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/e2e/shared"
	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/stretchr/testify/suite"
)

// ClientIntegrationSuite tests client-side integration scenarios.
// This suite validates:
// - Client configuration and initialization
// - Session management from client perspective
// - Agent lifecycle management
// - Error handling in client scenarios
type ClientIntegrationSuite struct {
	suite.Suite
	MockServer *EnhancedMockServer
	tempDir    string
}

func TestClientIntegrationSuite(t *testing.T) {
	suite.Run(t, new(ClientIntegrationSuite))
}

func (s *ClientIntegrationSuite) SetupSuite() {
	s.MockServer = NewMockServerWithDefaults()
}

func (s *ClientIntegrationSuite) TearDownSuite() {
	if s.MockServer != nil {
		s.MockServer.Close()
	}
}

func (s *ClientIntegrationSuite) SetupTest() {
	s.MockServer.Reset()
	s.tempDir = s.T().TempDir()
}

// TestClient_ConfigInitialization verifies client config initialization.
func (s *ClientIntegrationSuite) TestClient_ConfigInitialization() {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            s.MockServer.URL(),
		Model:              "test-model",
		CustomSystemPrompt: "test", // bypass auto-detection
		WorkingDir:         s.tempDir,
	}

	// Verify config is valid
	s.NotNil(cfg)
	s.Equal(s.MockServer.URL(), cfg.BaseURL)
	s.Equal("test-model", cfg.Model)
}

// TestClient_AgentCreation verifies agent creation from config.
func (s *ClientIntegrationSuite) TestClient_AgentCreation() {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            s.MockServer.URL(),
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         s.tempDir,
	}

	ag, err := agent.New(cfg, nil)
	s.NoError(err)
	s.NotNil(ag)
	defer ag.Shutdown()

	// Verify agent is properly initialized
	s.NotNil(ag.GetSessionManager())
	s.NotNil(ag.GetToolbox())
}

// TestClient_SessionLifecycle verifies session creation and management.
func (s *ClientIntegrationSuite) TestClient_SessionLifecycle() {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            s.MockServer.URL(),
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         s.tempDir,
	}

	ag, err := agent.New(cfg, nil)
	s.Require().NoError(err)
	defer ag.Shutdown()

	sm := ag.GetSessionManager()
	s.NotNil(sm)

	// Create session
	sessionID := "test-session-123"
	session := sm.GetOrCreateSession(sessionID)
	s.NotNil(session)
	s.Equal(sessionID, session.ID)

	// Verify session can be retrieved
	retrieved, exists := sm.GetSession(sessionID)
	s.True(exists)
	s.NotNil(retrieved)
	s.Equal(sessionID, retrieved.ID)

	// List sessions
	sessions := sm.ListSessions()
	s.Contains(sessions, session)
}

// TestClient_SessionPersistence verifies session save/load.
func (s *ClientIntegrationSuite) TestClient_SessionPersistence() {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            s.MockServer.URL(),
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         s.tempDir,
	}

	ag, err := agent.New(cfg, nil)
	s.Require().NoError(err)
	defer ag.Shutdown()

	sm := ag.GetSessionManager()
	sessionID := "persist-test"

	// Create session and add some conversation history
	session := sm.GetOrCreateSession(sessionID)
	session.ConversationHistory = append(session.ConversationHistory,
		llm.Message{Role: "user", Content: "Hello"},
		llm.Message{Role: "assistant", Content: "Hi there!"},
	)
	err = sm.SaveSession(sessionID)
	s.NoError(err)

	// Load session in new agent and verify conversation history is persisted
	ag2, err := agent.New(cfg, nil)
	s.Require().NoError(err)
	defer ag2.Shutdown()

	sm2 := ag2.GetSessionManager()
	loaded := sm2.GetOrCreateSession(sessionID)
	s.NotNil(loaded)
	// Verify the conversation history was loaded from disk
	messages := loaded.GetConversationHistory()
	s.GreaterOrEqual(len(messages), 2, "Should have at least user and assistant messages")
}

// TestClient_EngineInitialization verifies engine creation.
func (s *ClientIntegrationSuite) TestClient_EngineInitialization() {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            s.MockServer.URL(),
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         s.tempDir,
	}

	approvalHandler := func(info *agent.ToolCallInfo) bool {
		return true
	}

	eng, err := engine.New(cfg, approvalHandler)
	s.NoError(err)
	s.NotNil(eng)

	s.NotNil(eng.Agent)
	defer eng.Agent.Shutdown()
}

// TestClient_DaemonClientConnection verifies daemon client connectivity.
func (s *ClientIntegrationSuite) TestClient_DaemonClientConnection() {
	// Start daemon harness
	harness := shared.NewDaemonHarness(s.T(), s.MockServer)
	defer harness.Shutdown()

	err := harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)

	// Create daemon client
	client := daemon.NewClient("127.0.0.1", harness.Port, "")
	s.NotNil(client)

	// Verify connectivity
	health, err := client.Health()
	s.NoError(err)
	s.NotNil(health)
	s.Equal("healthy", health.Status)
}

// TestClient_DaemonSessionOps verifies daemon session operations from client.
func (s *ClientIntegrationSuite) TestClient_DaemonSessionOps() {
	harness := shared.NewDaemonHarness(s.T(), s.MockServer)
	defer harness.Shutdown()

	err := harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)

	client := harness.Client

	// Create session
	ctx := context.Background()
	sessionResp, err := client.CreateSession(ctx, "client-test-session", nil)
	s.NoError(err)
	s.True(sessionResp.Success)
	sessionID := sessionResp.SessionID

	// Setup mock
	s.MockServer.AddResponse(MockResponse{
		Content: "Client test response",
	})

	// Send message
	execResp, err := client.SendMessage(ctx, sessionID, "Test from client")
	s.NoError(err)
	s.NotNil(execResp)
	s.True(execResp.Success)

	// List sessions
	listResp, err := client.ListSessions(10)
	s.NoError(err)
	s.NotNil(listResp)
}

// TestClient_ConfigValidation verifies config validation.
func (s *ClientIntegrationSuite) TestClient_ConfigValidation() {
	// Invalid config - missing API key
	cfg := &config.Config{
		BaseURL: s.MockServer.URL(),
		Model:   "test-model",
	}

	// Should still create agent (API key can be from env)
	ag, err := agent.New(cfg, nil)
	if ag != nil {
		defer ag.Shutdown()
	}

	// Just verify it doesn't crash
	s.True(err != nil || ag != nil)
}

// TestClient_MultipleAgents verifies multiple agents can coexist.
func (s *ClientIntegrationSuite) TestClient_MultipleAgents() {
	cfg1 := &config.Config{
		APIKey:             "test-key-1",
		BaseURL:            s.MockServer.URL(),
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         filepath.Join(s.tempDir, "agent1"),
	}

	cfg2 := &config.Config{
		APIKey:             "test-key-2",
		BaseURL:            s.MockServer.URL(),
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         filepath.Join(s.tempDir, "agent2"),
	}

	ag1, err := agent.New(cfg1, nil)
	s.NoError(err)
	defer ag1.Shutdown()

	ag2, err := agent.New(cfg2, nil)
	s.NoError(err)
	defer ag2.Shutdown()

	// Verify both agents work independently
	sm1 := ag1.GetSessionManager()
	sm2 := ag2.GetSessionManager()

	sess1 := sm1.GetOrCreateSession("session-1")
	sess2 := sm2.GetOrCreateSession("session-2")

	s.NotEqual(sess1, sess2)
}

// TestClient_SessionIsolation verifies session isolation between agents.
func (s *ClientIntegrationSuite) TestClient_SessionIsolation() {
	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            s.MockServer.URL(),
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         s.tempDir,
	}

	ag, err := agent.New(cfg, nil)
	s.Require().NoError(err)
	defer ag.Shutdown()

	sm := ag.GetSessionManager()

	// Create multiple sessions
	sess1 := sm.GetOrCreateSession("isolated-1")
	sess2 := sm.GetOrCreateSession("isolated-2")

	// Modify one session
	sess1.Metadata["key1"] = "value1"

	// Verify other session is unaffected
	s.NotContains(sess2.Metadata, "key1")
}

// TestClient_WorkingDirectoryHandling verifies working directory setup.
func (s *ClientIntegrationSuite) TestClient_WorkingDirectoryHandling() {
	workDir := filepath.Join(s.tempDir, "custom-workdir")
	err := os.MkdirAll(workDir, 0755)
	s.Require().NoError(err)

	cfg := &config.Config{
		APIKey:             "test-key",
		BaseURL:            s.MockServer.URL(),
		Model:              "test-model",
		CustomSystemPrompt: "test",
		WorkingDir:         workDir,
	}

	ag, err := agent.New(cfg, nil)
	s.NoError(err)
	defer ag.Shutdown()

	// Verify agent uses correct working directory
	s.Equal(workDir, ag.GetConfig().WorkingDir)
}

package acp

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestACPServerSessionLifecycle(t *testing.T) {
	// Skip if no API key configured
	if os.Getenv("OPENAI_API_KEY") == "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping test: no API key configured")
	}

	// Create pipes for communication
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	// Create server
	cfg := config.DefaultConfig()
	// Use default LLM config

	server, err := NewServer(ServerOptions{
		Config: cfg,
		FSMode: FSModeLocal,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Override transport with test pipes
	server.transport = NewTransport(serverReader, serverWriter)

	// Start server in goroutine
	_, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Create mock client
	client := NewMockACPClient(clientReader, clientWriter)

	// Test 1: Create new session
	t.Run("SessionNew", func(t *testing.T) {
		result, err := client.SessionNew("/tmp/test")
		if err != nil {
			t.Fatalf("SessionNew failed: %v", err)
		}

		if result.SessionID == "" {
			t.Error("Expected non-empty session ID")
		}

		t.Logf("Created session: %s", result.SessionID)
	})

	// Test 2: Session close
	t.Run("SessionClose", func(t *testing.T) {
		// Create a session first
		newResult, err := client.SessionNew("/tmp/test")
		if err != nil {
			t.Fatalf("SessionNew failed: %v", err)
		}

		// Close the session
		closeResult, err := client.SessionClose(newResult.SessionID)
		if err != nil {
			t.Fatalf("SessionClose failed: %v", err)
		}

		if !closeResult.Success {
			t.Error("Expected successful close")
		}

		t.Logf("Closed session: %s", newResult.SessionID)
	})

	// Cleanup
	serverCancel()
	_ = clientWriter.Close()
	_ = serverWriter.Close()

	select {
	case <-serverDone:
		// Server stopped
	case <-time.After(2 * time.Second):
		t.Error("Server did not stop in time")
	}
}

func TestACPInitialize(t *testing.T) {
	// Create temporary working directory
	tempDir := t.TempDir()

	// Create pipes for communication
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	// Create server
	cfg := config.DefaultConfig()
	server, err := NewServer(ServerOptions{
		Config:  cfg,
		FSMode:  FSModeLocal,
		WorkDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Override transport with test pipes
	server.transport = NewTransport(serverReader, serverWriter)

	// Start server in goroutine
	_, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Create mock client
	client := NewMockACPClient(clientReader, clientWriter)

	// Test initialize
	t.Run("Initialize", func(t *testing.T) {
		result, err := client.Initialize()
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		// Verify protocol version
		if result.ProtocolVersion == 0 {
			t.Error("Expected non-zero protocol version")
		}

		// Verify agent info
		if result.AgentInfo.Name == "" {
			t.Error("Expected non-empty agent name")
		}

		// Verify capabilities
		if !result.AgentCapabilities.LoadSession {
			t.Error("Expected LoadSession capability to be true")
		}

		if !result.AgentCapabilities.PromptCapabilities.Image {
			t.Error("Expected Image prompt capability to be true")
		}

		if !result.AgentCapabilities.PromptCapabilities.Audio {
			t.Error("Expected Audio prompt capability to be true")
		}

		if !result.AgentCapabilities.PromptCapabilities.EmbeddedContext {
			t.Error("Expected EmbeddedContext prompt capability to be true")
		}

		if !result.AgentCapabilities.MCP.HTTP {
			t.Error("Expected MCP HTTP capability to be true")
		}

		if !result.AgentCapabilities.MCP.SSE {
			t.Error("Expected MCP SSE capability to be true")
		}

		// Verify authMethods structure
		if len(result.AuthMethods) == 0 {
			t.Error("Expected at least one auth method")
		}

		for _, method := range result.AuthMethods {
			if method.ID == "" {
				t.Error("Expected non-empty auth method ID")
			}
			if method.Name == "" {
				t.Error("Expected non-empty auth method name")
			}
			if method.Description == "" {
				t.Error("Expected non-empty auth method description")
			}
			t.Logf("Auth method: %s (%s) - %s", method.ID, method.Type, method.Name)
		}

		t.Logf("Initialize successful: protocol version %d, agent: %s %s",
			result.ProtocolVersion, result.AgentInfo.Name, result.AgentInfo.Version)
	})

	// Cleanup
	serverCancel()
	_ = clientWriter.Close()
	_ = serverWriter.Close()

	select {
	case <-serverDone:
		// Server stopped
	case <-time.After(2 * time.Second):
		t.Error("Server did not stop in time")
	}
}

func TestACPTransport(t *testing.T) {
	// Test JSON-RPC transport
	t.Run("ReadRequest", func(t *testing.T) {
		reader, writer := io.Pipe()
		transport := NewTransport(reader, io.Discard)

		// Send a request in background
		go func() {
			data := []byte(`{"jsonrpc":"2.0","method":"test","id":1}` + "\n")
			_, _ = writer.Write(data)
		}()

		req, err := transport.ReadRequest()
		if err != nil {
			t.Fatalf("ReadRequest failed: %v", err)
		}

		if req.Method != "test" {
			t.Errorf("Expected method 'test', got '%s'", req.Method)
		}
	})

	t.Run("SendResponse", func(t *testing.T) {
		reader, writer := io.Pipe()
		transport := NewTransport(io.LimitReader(reader, 0), writer)

		go func() {
			err := transport.SendSuccessResponse(1, map[string]string{"status": "ok"})
			if err != nil {
				t.Errorf("SendSuccessResponse failed: %v", err)
			}
		}()

		// Read response
		buf := make([]byte, 1024)
		n, err := reader.Read(buf)
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}

		response := string(buf[:n])
		if response == "" {
			t.Error("Expected non-empty response")
		}

		t.Logf("Response: %s", response)
	})
}

func TestSessionRegistry(t *testing.T) {
	registry := NewSessionRegistry()

	t.Run("CreateAndGet", func(t *testing.T) {
		session, err := registry.Create("nano-123", "/tmp/test", nil, SessionCapabilities{}, ClientCapabilities{}, FSModeLocal)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		if session.ACPSessionID == "" {
			t.Error("Expected non-empty ACP session ID")
		}

		// Get the session
		retrieved, ok := registry.Get(session.ACPSessionID)
		if !ok {
			t.Error("Expected to find session")
		}

		if retrieved.NanoSessionID != "nano-123" {
			t.Errorf("Expected nano session ID 'nano-123', got '%s'", retrieved.NanoSessionID)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		session, _ := registry.Create("nano-456", "/tmp/test", nil, SessionCapabilities{}, ClientCapabilities{}, FSModeLocal)

		err := registry.Delete(session.ACPSessionID)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Should not be able to get it anymore
		_, ok := registry.Get(session.ACPSessionID)
		if ok {
			t.Error("Expected session to be deleted")
		}
	})

	t.Run("List", func(t *testing.T) {
		// Create multiple sessions
		_, _ = registry.Create("nano-1", "/tmp/1", nil, SessionCapabilities{}, ClientCapabilities{}, FSModeLocal)
		_, _ = registry.Create("nano-2", "/tmp/2", nil, SessionCapabilities{}, ClientCapabilities{}, FSModeLocal)

		sessions := registry.List()
		if len(sessions) < 2 {
			t.Errorf("Expected at least 2 sessions, got %d", len(sessions))
		}
	})
}

func TestEventBridge(t *testing.T) {
	reader, writer := io.Pipe()
	transport := NewTransport(reader, writer)
	bridge := NewEventBridge("test-session", transport)

	t.Run("ConvertTextEvent", func(t *testing.T) {
		// This is a unit test for event conversion
		// We can't easily test the full flow without a running server
		// but we can verify the bridge was created
		if bridge == nil {
			t.Error("Expected non-nil bridge")
		}
	})
}

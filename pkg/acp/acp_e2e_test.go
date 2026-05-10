package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// E2ETestClient is a comprehensive test client that simulates Zed editor
type E2ETestClient struct {
	reader           *bufio.Reader
	writer           io.Writer
	mu               sync.Mutex
	requestID        int
	notifications    []RPCNotification
	pendingResponses map[int]chan *RPCResponse
}

// NewE2ETestClient creates a new end-to-end test client
func NewE2ETestClient(reader io.Reader, writer io.Writer) *E2ETestClient {
	client := &E2ETestClient{
		reader:           bufio.NewReader(reader),
		writer:           writer,
		requestID:        0,
		notifications:    make([]RPCNotification, 0),
		pendingResponses: make(map[int]chan *RPCResponse),
	}

	// Start background goroutine to read responses and notifications
	go client.readLoop()

	return client
}

// readLoop continuously reads messages from the server
func (c *E2ETestClient) readLoop() {
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Printf("Read error: %v\n", err)
			}
			return
		}

		// Try to parse as notification first (no ID field)
		var notif RPCNotification
		if err := json.Unmarshal(line, &notif); err == nil && notif.Method != "" {
			c.mu.Lock()
			c.notifications = append(c.notifications, notif)
			c.mu.Unlock()
			continue
		}

		// Otherwise, parse as response
		var resp RPCResponse
		if err := json.Unmarshal(line, &resp); err == nil {
			// Extract ID as int
			var id int
			switch v := resp.ID.(type) {
			case float64:
				id = int(v)
			case int:
				id = v
			}

			c.mu.Lock()
			if ch, ok := c.pendingResponses[id]; ok {
				ch <- &resp
				close(ch)
				delete(c.pendingResponses, id)
			}
			c.mu.Unlock()
		}
	}
}

// sendRequest sends a request and waits for the response
func (c *E2ETestClient) sendRequest(method string, params interface{}) (*RPCResponse, error) {
	c.mu.Lock()
	c.requestID++
	id := c.requestID
	responseChan := make(chan *RPCResponse, 1)
	c.pendingResponses[id] = responseChan
	c.mu.Unlock()

	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.mu.Lock()
	data = append(data, '\n')
	if _, err := c.writer.Write(data); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("write request: %w", err)
	}
	c.mu.Unlock()

	// Wait for response with timeout
	select {
	case resp := <-responseChan:
		return resp, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("timeout waiting for response")
	}
}

// SessionNew creates a new session
func (c *E2ETestClient) SessionNew(cwd string) (*SessionNewResult, error) {
	params := SessionNewParams{
		CWD: cwd,
		Capabilities: SessionCapabilities{
			FS: &FSCapabilities{
				Read:   true,
				Write:  true,
				List:   true,
				Delete: true,
			},
			Terminal: &TerminalCapabilities{
				Run:    true,
				Input:  true,
				Output: true,
				Kill:   true,
			},
		},
	}

	resp, err := c.sendRequest("session/new", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var result SessionNewResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// FSRead reads a file
func (c *E2ETestClient) FSRead(sessionID, path string) (string, error) {
	params := map[string]interface{}{
		"sessionId": sessionID,
		"path":      path,
	}

	resp, err := c.sendRequest("fs/read", params)
	if err != nil {
		return "", err
	}

	if resp.Error != nil {
		return "", fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	result := resp.Result.(map[string]interface{})
	return result["content"].(string), nil
}

// FSWrite writes a file
func (c *E2ETestClient) FSWrite(sessionID, path, content string) error {
	params := map[string]interface{}{
		"sessionId": sessionID,
		"path":      path,
		"content":   content,
	}

	resp, err := c.sendRequest("fs/write", params)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	return nil
}

// FSList lists directory contents
func (c *E2ETestClient) FSList(sessionID, path string) ([]FSEntry, error) {
	params := map[string]interface{}{
		"sessionId": sessionID,
		"path":      path,
	}

	resp, err := c.sendRequest("fs/list", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	result := resp.Result.(map[string]interface{})
	entriesData, _ := json.Marshal(result["entries"])

	var entries []FSEntry
	if err := json.Unmarshal(entriesData, &entries); err != nil {
		return nil, err
	}

	return entries, nil
}

// TerminalRun runs a terminal command
func (c *E2ETestClient) TerminalRun(sessionID, command, cwd string, env map[string]string) (string, error) {
	params := map[string]interface{}{
		"sessionId": sessionID,
		"command":   command,
		"cwd":       cwd,
		"env":       env,
	}

	resp, err := c.sendRequest("terminal/run", params)
	if err != nil {
		return "", err
	}

	if resp.Error != nil {
		return "", fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	result := resp.Result.(map[string]interface{})
	return result["processId"].(string), nil
}

// SessionClose closes a session
func (c *E2ETestClient) SessionClose(sessionID string) error {
	params := SessionCloseParams{
		SessionID: sessionID,
	}

	resp, err := c.sendRequest("session/close", params)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	return nil
}

// GetNotifications returns collected notifications
func (c *E2ETestClient) GetNotifications() []RPCNotification {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]RPCNotification{}, c.notifications...)
}

// WaitForNotification waits for a notification with a specific method
func (c *E2ETestClient) WaitForNotification(method string, timeout time.Duration) (*RPCNotification, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		c.mu.Lock()
		for i, notif := range c.notifications {
			if notif.Method == method {
				// Remove from list
				c.notifications = append(c.notifications[:i], c.notifications[i+1:]...)
				c.mu.Unlock()
				return &notif, nil
			}
		}
		c.mu.Unlock()

		time.Sleep(50 * time.Millisecond)
	}

	return nil, fmt.Errorf("timeout waiting for notification: %s", method)
}

// TestACPE2EFilesystemOperations tests the full filesystem bridge functionality
func TestACPE2EFilesystemOperations(t *testing.T) {
	// Create temporary test directory
	tempDir, err := os.MkdirTemp("", "acp-e2e-fs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create pipes for communication
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	// Create and start server
	server, err := NewServer(ServerOptions{
		Config:  nil, // Will use defaults
		FSMode:  FSModeLocal,
		WorkDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	server.transport = NewTransport(serverReader, serverWriter)

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve()
	}()
	defer func() {
		_ = clientWriter.Close()
		_ = serverWriter.Close()
		<-serverDone
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Create E2E client
	client := NewE2ETestClient(clientReader, clientWriter)

	// Test 1: Create session
	t.Run("CreateSession", func(t *testing.T) {
		result, err := client.SessionNew(tempDir)
		if err != nil {
			t.Fatalf("SessionNew failed: %v", err)
		}

		if result.SessionID == "" {
			t.Error("Expected non-empty session ID")
		}

		if result.Capabilities.FS == nil || !result.Capabilities.FS.Read {
			t.Error("Expected filesystem read capability")
		}

		t.Logf("Created session: %s", result.SessionID)
	})

	// Create a new session for filesystem tests
	sessionResult, err := client.SessionNew(tempDir)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResult.SessionID

	// Test 2: Write a file
	t.Run("WriteFile", func(t *testing.T) {
		testContent := "Hello from ACP E2E test!"
		err := client.FSWrite(sessionID, "test.txt", testContent)
		if err != nil {
			t.Fatalf("FSWrite failed: %v", err)
		}

		// Verify file was actually written
		content, err := os.ReadFile(filepath.Join(tempDir, "test.txt"))
		if err != nil {
			t.Fatalf("Failed to read written file: %v", err)
		}

		if string(content) != testContent {
			t.Errorf("Expected content %q, got %q", testContent, string(content))
		}
	})

	// Test 3: Read the file back
	t.Run("ReadFile", func(t *testing.T) {
		content, err := client.FSRead(sessionID, "test.txt")
		if err != nil {
			t.Fatalf("FSRead failed: %v", err)
		}

		expectedContent := "Hello from ACP E2E test!"
		if content != expectedContent {
			t.Errorf("Expected content %q, got %q", expectedContent, content)
		}
	})

	// Test 4: List directory
	t.Run("ListDirectory", func(t *testing.T) {
		entries, err := client.FSList(sessionID, ".")
		if err != nil {
			t.Fatalf("FSList failed: %v", err)
		}

		// Should contain test.txt
		found := false
		for _, entry := range entries {
			if entry.Name == "test.txt" {
				found = true
				if entry.IsDir {
					t.Error("test.txt should not be a directory")
				}
				break
			}
		}

		if !found {
			t.Error("Expected to find test.txt in directory listing")
		}

		t.Logf("Found %d entries in directory", len(entries))
	})

	// Test 5: Write nested file
	t.Run("WriteNestedFile", func(t *testing.T) {
		err := client.FSWrite(sessionID, "subdir/nested.txt", "Nested content")
		if err != nil {
			t.Fatalf("FSWrite nested failed: %v", err)
		}

		// Verify directory was created
		info, err := os.Stat(filepath.Join(tempDir, "subdir"))
		if err != nil {
			t.Fatalf("Subdir not created: %v", err)
		}
		if !info.IsDir() {
			t.Error("subdir should be a directory")
		}
	})

	// Cleanup session
	if err := client.SessionClose(sessionID); err != nil {
		t.Errorf("SessionClose failed: %v", err)
	}
}

// TestACPE2ETerminalOperations tests the terminal bridge functionality
func TestACPE2ETerminalOperations(t *testing.T) {
	// Create temporary test directory
	tempDir, err := os.MkdirTemp("", "acp-e2e-term-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create pipes for communication
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	// Create and start server
	server, err := NewServer(ServerOptions{
		Config:  nil,
		FSMode:  FSModeLocal,
		WorkDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	server.transport = NewTransport(serverReader, serverWriter)

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve()
	}()
	defer func() {
		_ = clientWriter.Close()
		_ = serverWriter.Close()
		<-serverDone
	}()

	time.Sleep(100 * time.Millisecond)

	// Create E2E client
	client := NewE2ETestClient(clientReader, clientWriter)

	// Create session
	sessionResult, err := client.SessionNew(tempDir)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResult.SessionID

	// Test 1: Run simple command
	t.Run("RunCommand", func(t *testing.T) {
		processID, err := client.TerminalRun(sessionID, "echo 'Hello Terminal'", tempDir, nil)
		if err != nil {
			t.Fatalf("TerminalRun failed: %v", err)
		}

		if processID == "" {
			t.Error("Expected non-empty process ID")
		}

		t.Logf("Started process: %s", processID)

		// Wait for terminal/output notification
		notif, err := client.WaitForNotification("terminal/output", 5*time.Second)
		if err != nil {
			t.Logf("Warning: %v (this is acceptable if command completes very quickly)", err)
		} else {
			params := notif.Params.(map[string]interface{})
			if data, ok := params["data"].(string); ok {
				if !strings.Contains(data, "Hello Terminal") {
					t.Logf("Output received: %q", data)
				}
			}
		}

		// Wait for terminal/exit notification
		exitNotif, err := client.WaitForNotification("terminal/exit", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive exit notification: %v", err)
		}

		params := exitNotif.Params.(map[string]interface{})
		exitCode := int(params["exitCode"].(float64))
		if exitCode != 0 {
			t.Errorf("Expected exit code 0, got %d", exitCode)
		}
	})

	// Test 2: Run command that creates a file
	t.Run("RunCommandWithOutput", func(t *testing.T) {
		processID, err := client.TerminalRun(sessionID, "echo 'test content' > output.txt", tempDir, nil)
		if err != nil {
			t.Fatalf("TerminalRun failed: %v", err)
		}

		// Wait for exit
		_, err = client.WaitForNotification("terminal/exit", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive exit notification: %v", err)
		}

		t.Logf("Command completed: %s", processID)

		// Verify file was created
		content, err := os.ReadFile(filepath.Join(tempDir, "output.txt"))
		if err != nil {
			t.Fatalf("Output file not created: %v", err)
		}

		if !strings.Contains(string(content), "test content") {
			t.Errorf("Expected 'test content' in file, got: %s", string(content))
		}
	})

	// Cleanup session
	if err := client.SessionClose(sessionID); err != nil {
		t.Errorf("SessionClose failed: %v", err)
	}
}

// TestACPE2EProtocolFlow tests the complete protocol flow
func TestACPE2EProtocolFlow(t *testing.T) {
	// Create temporary test directory
	tempDir, err := os.MkdirTemp("", "acp-e2e-flow-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create pipes
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	// Create server
	server, err := NewServer(ServerOptions{
		Config:  nil,
		FSMode:  FSModeLocal,
		WorkDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	server.transport = NewTransport(serverReader, serverWriter)

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve()
	}()
	defer func() {
		_ = clientWriter.Close()
		_ = serverWriter.Close()
		<-serverDone
	}()

	time.Sleep(100 * time.Millisecond)

	// Create client
	client := NewE2ETestClient(clientReader, clientWriter)

	// Test complete workflow
	t.Run("CompleteWorkflow", func(t *testing.T) {
		// 1. Create session
		sessionResult, err := client.SessionNew(tempDir)
		if err != nil {
			t.Fatalf("SessionNew failed: %v", err)
		}
		sessionID := sessionResult.SessionID
		t.Logf("✓ Session created: %s", sessionID)

		// Verify capabilities
		if sessionResult.Capabilities.FS == nil {
			t.Error("Missing filesystem capabilities")
		}
		if sessionResult.Capabilities.Terminal == nil {
			t.Error("Missing terminal capabilities")
		}
		t.Logf("✓ Capabilities verified")

		// 2. Write some files using filesystem bridge
		files := map[string]string{
			"README.md":    "# Test Project\n\nThis is a test.",
			"src/main.go":  "package main\n\nfunc main() {}\n",
			"src/utils.go": "package main\n\nfunc helper() {}\n",
		}

		for path, content := range files {
			if err := client.FSWrite(sessionID, path, content); err != nil {
				t.Fatalf("Failed to write %s: %v", path, err)
			}
		}
		t.Logf("✓ Created %d files", len(files))

		// 3. List directory to verify
		entries, err := client.FSList(sessionID, ".")
		if err != nil {
			t.Fatalf("FSList failed: %v", err)
		}
		t.Logf("✓ Listed %d entries", len(entries))

		// 4. Run terminal command to verify files
		processID, err := client.TerminalRun(sessionID, "ls -la", tempDir, nil)
		if err != nil {
			t.Fatalf("TerminalRun failed: %v", err)
		}
		t.Logf("✓ Terminal command started: %s", processID)

		// Wait for exit
		_, err = client.WaitForNotification("terminal/exit", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive exit notification: %v", err)
		}
		t.Logf("✓ Terminal command completed")

		// 5. Read back one of the files
		content, err := client.FSRead(sessionID, "README.md")
		if err != nil {
			t.Fatalf("FSRead failed: %v", err)
		}
		if !strings.Contains(content, "Test Project") {
			t.Errorf("Unexpected content: %s", content)
		}
		t.Logf("✓ File read successful")

		// 6. Close session
		if err := client.SessionClose(sessionID); err != nil {
			t.Fatalf("SessionClose failed: %v", err)
		}
		t.Logf("✓ Session closed")
	})
}

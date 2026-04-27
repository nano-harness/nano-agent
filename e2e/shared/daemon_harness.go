//go:build e2e

package shared

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/stretchr/testify/require"
)

// DaemonHarness provides an in-process daemon server for testing.
// It starts a real HTTP server on a random port and provides a daemon client for interaction.
type DaemonHarness struct {
	Server     *daemon.Server
	Client     *daemon.Client
	Engine     *engine.Engine
	MockServer interface{} // EnhancedMockServer - using interface{} to avoid circular dependency
	Port       int
	WorkDir    string
	httpServer *http.Server
	listener   net.Listener

	t *testing.T
}

// NewDaemonHarness creates and starts a new in-process daemon server for testing.
// The mockServer parameter should be an *e2e.EnhancedMockServer.
func NewDaemonHarness(t *testing.T, mockServer interface{}) *DaemonHarness {
	t.Helper()

	// Create temporary working directory
	workDir := t.TempDir()

	// Get mock server URL (using type assertion since we can't import e2e package here)
	type urlGetter interface {
		URL() string
	}
	mockURL := mockServer.(urlGetter).URL()

	// Create test config
	cfg := NewTestConfig(mockURL, workDir, true)
	config.SetGlobalConfig(cfg)

	// Create agent with approval handler that always approves
	approvalHandler := func(info *agent.ToolCallInfo) bool {
		return true
	}

	// Create engine (which creates the agent internally)
	eng, err := engine.New(cfg, approvalHandler)
	require.NoError(t, err, "failed to create engine")

	// Configure daemon settings
	daemonCfg := &config.DaemonConfig{
		Host:       "127.0.0.1",
		Port:       0, // Will be assigned dynamically
		EnableCORS: true,
		APIKey:     "",
	}

	// Create server
	server := daemon.NewServerWithEngine(eng, daemonCfg)

	// Use exported test method to get router
	router := server.SetupRoutesForTest()

	// Create listener on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "failed to create listener")

	port := listener.Addr().(*net.TCPAddr).Port

	// Create HTTP server
	httpServer := &http.Server{
		Handler: router,
	}

	harness := &DaemonHarness{
		Server:     server,
		Engine:     eng,
		MockServer: mockServer,
		Port:       port,
		WorkDir:    workDir,
		httpServer: httpServer,
		listener:   listener,
		t:          t,
	}

	// Start server in background
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Logf("Daemon HTTP server error: %v", err)
		}
	}()

	// Create client
	client := daemon.NewClient("127.0.0.1", port, "")
	harness.Client = client

	// Register cleanup
	t.Cleanup(func() {
		harness.Shutdown()
	})

	return harness
}

// Shutdown gracefully shuts down the daemon harness.
func (h *DaemonHarness) Shutdown() {
	if h.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.httpServer.Shutdown(ctx)
	}
	if h.listener != nil {
		_ = h.listener.Close()
	}
	if h.Engine != nil && h.Engine.Agent != nil {
		_ = h.Engine.Agent.Shutdown()
	}
}

// WaitReady waits for the daemon server to be ready to accept requests.
// Returns an error if the server doesn't become ready within the timeout.
func (h *DaemonHarness) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", h.Port))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon not ready after %v", timeout)
}

// URL returns the base URL of the daemon server.
func (h *DaemonHarness) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", h.Port)
}

// BaseURL returns the base URL of the daemon server.
func (h *DaemonHarness) BaseURL() string {
	return h.URL()
}

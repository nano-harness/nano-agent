//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/e2e/shared"
	"github.com/stretchr/testify/suite"
)

// DaemonLifecycleSuite tests daemon server lifecycle operations.
// This suite validates:
// - Server startup and shutdown
// - Health check endpoint
// - Status endpoint
// - Server readiness
type DaemonLifecycleSuite struct {
	suite.Suite
	MockServer *EnhancedMockServer
	Harness    *shared.DaemonHarness
}

func TestDaemonLifecycleSuite(t *testing.T) {
	suite.Run(t, new(DaemonLifecycleSuite))
}

func (s *DaemonLifecycleSuite) SetupSuite() {
	// Start mock LLM server
	s.MockServer = NewMockServerWithDefaults()
}

func (s *DaemonLifecycleSuite) TearDownSuite() {
	if s.MockServer != nil {
		s.MockServer.Close()
	}
}

func (s *DaemonLifecycleSuite) SetupTest() {
	// Reset mock server
	s.MockServer.Reset()

	// Start daemon harness
	s.Harness = shared.NewDaemonHarness(s.T(), s.MockServer)
}

func (s *DaemonLifecycleSuite) TearDownTest() {
	if s.Harness != nil {
		s.Harness.Shutdown()
	}
}

// TestDaemon_ServerStartup verifies daemon server starts correctly.
func (s *DaemonLifecycleSuite) TestDaemon_ServerStartup() {
	// Server should be running
	s.NotNil(s.Harness.Server)
	s.NotNil(s.Harness.Client)
	s.Greater(s.Harness.Port, 0)

	// Server should become ready quickly
	err := s.Harness.WaitReady(2 * time.Second)
	s.NoError(err, "Server should become ready")
}

// TestDaemon_HealthCheck verifies health check endpoint.
func (s *DaemonLifecycleSuite) TestDaemon_HealthCheck() {
	// Wait for server to be ready
	err := s.Harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)

	// Call health endpoint
	resp, err := s.Harness.Client.Health()
	s.NoError(err)
	s.NotNil(resp)

	// Verify response fields
	s.Equal("healthy", resp.Status)
	s.Greater(resp.Timestamp, int64(0))
	s.NotEmpty(resp.Version)
	s.GreaterOrEqual(resp.Uptime, 0.0)
}

// TestDaemon_StatusCheck verifies status endpoint.
func (s *DaemonLifecycleSuite) TestDaemon_StatusCheck() {
	err := s.Harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)

	// Call status endpoint
	resp, err := s.Harness.Client.Status()
	s.NoError(err)
	s.NotNil(resp)

	// Verify response structure
	s.NotEmpty(resp.AgentStatus)
	// MCP and memory fields may vary based on config
}

// TestDaemon_GracefulShutdown verifies graceful shutdown.
func (s *DaemonLifecycleSuite) TestDaemon_GracefulShutdown() {
	err := s.Harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)

	// Health check should work before shutdown
	resp, err := s.Harness.Client.Health()
	s.NoError(err)
	s.Equal("healthy", resp.Status)

	// Shutdown server
	s.Harness.Shutdown()

	// Health check should fail after shutdown
	time.Sleep(100 * time.Millisecond)
	_, err = s.Harness.Client.Health()
	s.Error(err, "Health check should fail after shutdown")
}

// TestDaemon_MultipleRequests verifies server handles concurrent requests.
func (s *DaemonLifecycleSuite) TestDaemon_MultipleRequests() {
	err := s.Harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)

	// Make multiple concurrent requests
	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_, err := s.Harness.Client.Health()
			done <- err
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < 10; i++ {
		err := <-done
		s.NoError(err, "Concurrent request should succeed")
	}
}

// TestDaemon_Uptime verifies uptime tracking.
func (s *DaemonLifecycleSuite) TestDaemon_Uptime() {
	err := s.Harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)

	// First health check
	resp1, err := s.Harness.Client.Health()
	s.NoError(err)
	uptime1 := resp1.Uptime

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Second health check
	resp2, err := s.Harness.Client.Health()
	s.NoError(err)
	uptime2 := resp2.Uptime

	// Uptime should increase
	s.Greater(uptime2, uptime1, "Uptime should increase over time")
}

// TestDaemon_PortBinding verifies daemon binds to assigned port.
func (s *DaemonLifecycleSuite) TestDaemon_PortBinding() {
	// Port should be assigned
	s.Greater(s.Harness.Port, 0)
	s.Less(s.Harness.Port, 65536)

	// URL should be accessible
	url := s.Harness.URL()
	s.Contains(url, "127.0.0.1")
	s.Contains(url, string(rune('0'+s.Harness.Port/10000%10))) // Port in URL
}

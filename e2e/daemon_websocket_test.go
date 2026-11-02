//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/e2e/shared"
	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/stretchr/testify/suite"
)

// DaemonWebSocketSuite tests WebSocket streaming functionality.
// This suite validates:
// - WebSocket connection establishment
// - Event streaming via WebSocket
// - Session event subscription
// - Event ordering and delivery
type DaemonWebSocketSuite struct {
	suite.Suite
	MockServer *EnhancedMockServer
	Harness    *shared.DaemonHarness
}

func TestDaemonWebSocketSuite(t *testing.T) {
	suite.Run(t, new(DaemonWebSocketSuite))
}

func (s *DaemonWebSocketSuite) SetupSuite() {
	s.MockServer = NewMockServerWithDefaults()
}

func (s *DaemonWebSocketSuite) TearDownSuite() {
	if s.MockServer != nil {
		s.MockServer.Close()
	}
}

func (s *DaemonWebSocketSuite) SetupTest() {
	s.MockServer.Reset()
	s.Harness = shared.NewDaemonHarness(s.T(), s.MockServer)
	err := s.Harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)
}

func (s *DaemonWebSocketSuite) TearDownTest() {
	if s.Harness != nil {
		s.Harness.Shutdown()
	}
}

// TestWebSocket_CreateAndStream verifies creating session and streaming events.
func (s *DaemonWebSocketSuite) TestWebSocket_CreateAndStream() {
	ctx := context.Background()

	// Create session
	sessionResp, err := s.Harness.Client.CreateSession(ctx, "test-session", &daemon.SessionConfig{
		WorkingDir: s.Harness.WorkDir,
	})
	s.NoError(err)
	s.NotNil(sessionResp)
	s.True(sessionResp.Success)
	s.NotEmpty(sessionResp.SessionID)

	sessionID := sessionResp.SessionID

	// Setup mock response
	s.MockServer.AddResponse(MockResponse{
		Content: "Hello from WebSocket test",
	})

	// Collect events via streaming
	events := make([]event.StreamEvent, 0)
	eventChan := make(chan event.StreamEvent, 100)
	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()

	// Start streaming in background
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- s.Harness.Client.StreamEvents(streamCtx, sessionID, func(ev event.StreamEvent) error {
			eventChan <- ev
			return nil
		})
	}()

	// Wait for WebSocket to connect
	time.Sleep(500 * time.Millisecond)

	// Send message to trigger events
	_, err = s.Harness.Client.SendMessage(ctx, sessionID, "Test message")
	s.NoError(err)

	// Collect events
	timeout := time.After(5 * time.Second)
	done := false
	for !done {
		select {
		case ev := <-eventChan:
			events = append(events, ev)
			// Check for completion
			if ev.Type == event.EventTypeTaskCompletion {
				done = true
			}
		case <-timeout:
			done = true
		}
	}

	streamCancel()

	// Verify we received events
	s.NotEmpty(events, "Should receive events via WebSocket")

	// Verify we got content events
	hasContent := false
	for _, ev := range events {
		if ev.Type == event.EventTypeStreamContent || ev.Type == event.EventTypeContent {
			hasContent = true
			break
		}
	}
	s.True(hasContent, "Should receive content events")
}

// TestWebSocket_SendMessage verifies SendMessage API.
func (s *DaemonWebSocketSuite) TestWebSocket_SendMessage() {
	ctx := context.Background()

	// Create session
	sessionResp, err := s.Harness.Client.CreateSession(ctx, "", nil)
	s.NoError(err)
	sessionID := sessionResp.SessionID

	// Setup mock
	s.MockServer.AddResponse(MockResponse{
		Content: "Response to message",
	})

	// Send message
	execResp, err := s.Harness.Client.SendMessage(ctx, sessionID, "Test message")
	s.NoError(err)
	s.NotNil(execResp)
	s.True(execResp.Success)
}

// TestWebSocket_MultipleMessages verifies sequential messages in same session.
func (s *DaemonWebSocketSuite) TestWebSocket_MultipleMessages() {
	ctx := context.Background()

	// Create session
	sessionResp, err := s.Harness.Client.CreateSession(ctx, "", nil)
	s.NoError(err)
	sessionID := sessionResp.SessionID

	// Setup mock responses
	s.MockServer.AddResponse(MockResponse{Content: "First response"})
	s.MockServer.AddResponse(MockResponse{Content: "Second response"})
	s.MockServer.AddResponse(MockResponse{Content: "Third response"})

	// Send multiple messages
	for i := 0; i < 3; i++ {
		_, err := s.Harness.Client.SendMessage(ctx, sessionID, "Message")
		s.NoError(err)
	}

	// Verify all succeeded
	s.NoError(err)
}

// TestWebSocket_EventOrdering verifies events are received in order.
func (s *DaemonWebSocketSuite) TestWebSocket_EventOrdering() {
	ctx := context.Background()

	sessionResp, err := s.Harness.Client.CreateSession(ctx, "", nil)
	s.NoError(err)
	sessionID := sessionResp.SessionID

	s.MockServer.AddResponse(MockResponse{
		Content: "Event ordering test",
	})

	// Collect events
	events := make([]event.StreamEvent, 0)
	eventChan := make(chan event.StreamEvent, 100)
	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()

	go func() {
		_ = s.Harness.Client.StreamEvents(streamCtx, sessionID, func(ev event.StreamEvent) error {
			eventChan <- ev
			return nil
		})
	}()

	time.Sleep(300 * time.Millisecond)

	// Send message
	_, err = s.Harness.Client.SendMessage(ctx, sessionID, "Test")
	s.NoError(err)

	// Collect events
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev := <-eventChan:
			events = append(events, ev)
			if ev.Type == event.EventTypeTaskCompletion {
				goto done
			}
		case <-timeout:
			goto done
		}
	}
done:
	streamCancel()

	// Verify timestamps are increasing
	if len(events) > 1 {
		for i := 1; i < len(events); i++ {
			s.GreaterOrEqual(events[i].Timestamp, events[i-1].Timestamp,
				"Events should have non-decreasing timestamps")
		}
	}
}

// TestWebSocket_ConnectionFailure verifies handling of connection failures.
func (s *DaemonWebSocketSuite) TestWebSocket_ConnectionFailure() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try to stream from invalid session
	err := s.Harness.Client.StreamEvents(ctx, "", func(ev event.StreamEvent) error {
		return nil
	})

	s.Error(err, "Should error with empty session ID")
}

// TestWebSocket_ContextCancellation verifies context cancellation.
func (s *DaemonWebSocketSuite) TestWebSocket_ContextCancellation() {
	sessionResp, err := s.Harness.Client.CreateSession(context.Background(), "", nil)
	s.NoError(err)
	sessionID := sessionResp.SessionID

	ctx, cancel := context.WithCancel(context.Background())

	// Start streaming
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- s.Harness.Client.StreamEvents(ctx, sessionID, func(ev event.StreamEvent) error {
			return nil
		})
	}()

	// Wait a bit then cancel
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Verify stream stops. The stream may exit gracefully or with an error
	// depending on timing of the WebSocket shutdown; the important contract is
	// that cancellation makes it return promptly.
	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		s.Fail("Stream did not stop after context cancellation")
	}
}

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

// ExpertViaDaemonSuite tests expert/sub-agent events via daemon WebSocket.
// This suite validates:
// - Expert events are streamed via WebSocket
// - WorkerID attribution in daemon mode
// - Parallel expert execution event streaming
// - Session isolation for expert events
type ExpertViaDaemonSuite struct {
	suite.Suite
	MockServer *EnhancedMockServer
	Harness    *shared.DaemonHarness
}

func TestExpertViaDaemonSuite(t *testing.T) {
	suite.Run(t, new(ExpertViaDaemonSuite))
}

func (s *ExpertViaDaemonSuite) SetupSuite() {
	s.MockServer = NewMockServerWithDefaults()
}

func (s *ExpertViaDaemonSuite) TearDownSuite() {
	if s.MockServer != nil {
		s.MockServer.Close()
	}
}

func (s *ExpertViaDaemonSuite) SetupTest() {
	s.MockServer.Reset()
	s.Harness = shared.NewDaemonHarness(s.T(), s.MockServer)
	err := s.Harness.WaitReady(2 * time.Second)
	s.Require().NoError(err)
}

func (s *ExpertViaDaemonSuite) TearDownTest() {
	if s.Harness != nil {
		s.Harness.Shutdown()
	}
}

// TestDaemonExpert_SingleExpertStreaming verifies single expert event streaming.
func (s *ExpertViaDaemonSuite) TestDaemonExpert_SingleExpertStreaming() {
	ctx := context.Background()

	// Create session
	sessionResp, err := s.Harness.Client.CreateSession(ctx, "", &daemon.SessionConfig{
		WorkingDir: s.Harness.WorkDir,
	})
	s.NoError(err)
	sessionID := sessionResp.SessionID

	// Setup mock: parent delegates to expert
	s.MockServer.AddResponse(MockResponse{
		Content: "I'll delegate this task.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_expert",
				Name:      "task",
				Arguments: `{"prompt":"Analyze the code","subagent_type":"execute","description":"code-analyst"}`,
			},
		},
	})

	// Expert response
	s.MockServer.AddResponse(MockResponse{
		Content: "Code analysis complete.",
	})

	// Parent final response
	s.MockServer.AddResponse(MockResponse{
		Content: "Expert task completed.",
	})

	// Collect events
	events := make([]event.StreamEvent, 0)
	eventChan := make(chan event.StreamEvent, 100)
	streamCtx, streamCancel := context.WithTimeout(ctx, 10*time.Second)
	defer streamCancel()

	// Start streaming
	go func() {
		_ = s.Harness.Client.StreamEvents(streamCtx, sessionID, func(ev event.StreamEvent) error {
			eventChan <- ev
			return nil
		})
	}()

	time.Sleep(300 * time.Millisecond)

	// Send message
	_, err = s.Harness.Client.SendMessage(ctx, sessionID, "Analyze my code")
	s.NoError(err)

	// Collect events
	timeout := time.After(8 * time.Second)
	done := false
	for !done {
		select {
		case ev := <-eventChan:
			events = append(events, ev)
			if ev.Type == event.EventTypeTaskCompletion {
				done = true
			}
		case <-timeout:
			done = true
		}
	}

	streamCancel()

	// Verify expert events
	var startedEvent, finishedEvent *event.StreamEvent
	for i := range events {
		ev := &events[i]
		if ev.Type == event.EventTypeExpertStarted {
			startedEvent = ev
		}
		if ev.Type == event.EventTypeExpertFinished {
			finishedEvent = ev
		}
	}

	s.NotNil(startedEvent, "Should receive ExpertStarted event")
	s.NotNil(finishedEvent, "Should receive ExpertFinished event")

	// Verify worker ID
	if finishedEvent != nil {
		s.NotEmpty(finishedEvent.WorkerID, "Expert finished event should have WorkerID")
		s.Contains(finishedEvent.WorkerID, "code-analyst")
	}
}

// TestDaemonExpert_ParallelExperts verifies parallel expert event streaming.
func (s *ExpertViaDaemonSuite) TestDaemonExpert_ParallelExperts() {
	ctx := context.Background()

	sessionResp, err := s.Harness.Client.CreateSession(ctx, "", nil)
	s.NoError(err)
	sessionID := sessionResp.SessionID

	// Parent dispatches 3 parallel experts
	s.MockServer.AddResponse(MockResponse{
		Content: "Dispatching three experts.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_alpha",
				Name:      "task",
				Arguments: `{"prompt":"Alpha analysis","subagent_type":"execute","description":"alpha"}`,
			},
			{
				ID:        "call_beta",
				Name:      "task",
				Arguments: `{"prompt":"Beta analysis","subagent_type":"execute","description":"beta"}`,
			},
			{
				ID:        "call_gamma",
				Name:      "task",
				Arguments: `{"prompt":"Gamma analysis","subagent_type":"execute","description":"gamma"}`,
			},
		},
	})

	// Setup rule-based routing
	s.MockServer.AddRule(MockRule{
		Name:    "alpha",
		Matcher: MatchTaskFieldContains("Alpha"),
		Response: MockResponse{
			Content: "Alpha done.",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "beta",
		Matcher: MatchTaskFieldContains("Beta"),
		Response: MockResponse{
			Content: "Beta done.",
		},
	})
	s.MockServer.AddRule(MockRule{
		Name:    "gamma",
		Matcher: MatchTaskFieldContains("Gamma"),
		Response: MockResponse{
			Content: "Gamma done.",
		},
	})

	// Parent final response
	s.MockServer.AddResponse(MockResponse{
		Content: "All experts completed.",
	})

	// Collect events
	events := make([]event.StreamEvent, 0)
	eventChan := make(chan event.StreamEvent, 100)
	streamCtx, streamCancel := context.WithTimeout(ctx, 15*time.Second)
	defer streamCancel()

	go func() {
		_ = s.Harness.Client.StreamEvents(streamCtx, sessionID, func(ev event.StreamEvent) error {
			eventChan <- ev
			return nil
		})
	}()

	time.Sleep(300 * time.Millisecond)

	// Send message
	_, err = s.Harness.Client.SendMessage(ctx, sessionID, "Run three parallel analyses")
	s.NoError(err)

	// Collect events until all experts finish
	timeout := time.After(12 * time.Second)
	finishedCount := 0
	for finishedCount < 3 {
		select {
		case ev := <-eventChan:
			events = append(events, ev)
			if ev.Type == event.EventTypeExpertFinished {
				finishedCount++
			}
		case <-timeout:
			s.FailNow("Timeout waiting for 3 expert finished events")
		}
	}

	// Collect remaining events
	time.Sleep(500 * time.Millisecond)
	for len(eventChan) > 0 {
		events = append(events, <-eventChan)
	}

	streamCancel()

	// Verify we got started and finished events
	startedEvents := filterEventsByType(events, event.EventTypeExpertStarted)
	finishedEvents := filterEventsByType(events, event.EventTypeExpertFinished)

	s.GreaterOrEqual(len(startedEvents), 3, "Should have at least 3 started events")
	s.Equal(3, len(finishedEvents), "Should have exactly 3 finished events")

	// Verify unique worker IDs
	workerIDs := make(map[string]bool)
	for _, ev := range finishedEvents {
		if ev.WorkerID != "" {
			workerIDs[ev.WorkerID] = true
		}
	}

	s.Equal(3, len(workerIDs), "Should have 3 unique worker IDs")
}

// TestDaemonExpert_EventAttribution verifies correct event attribution.
func (s *ExpertViaDaemonSuite) TestDaemonExpert_EventAttribution() {
	ctx := context.Background()

	sessionResp, err := s.Harness.Client.CreateSession(ctx, "", nil)
	s.NoError(err)
	sessionID := sessionResp.SessionID

	// Setup expert execution
	s.MockServer.AddResponse(MockResponse{
		Content: "Delegating to expert.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_expert",
				Name:      "task",
				Arguments: `{"prompt":"Test task","subagent_type":"execute","description":"test-expert"}`,
			},
		},
	})

	s.MockServer.AddResponse(MockResponse{
		Content: "Expert work done.",
	})

	s.MockServer.AddResponse(MockResponse{
		Content: "Task complete.",
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

	_, err = s.Harness.Client.SendMessage(ctx, sessionID, "Test")
	s.NoError(err)

	timeout := time.After(8 * time.Second)
	done := false
	for !done {
		select {
		case ev := <-eventChan:
			events = append(events, ev)
			if ev.Type == event.EventTypeTaskCompletion {
				done = true
			}
		case <-timeout:
			done = true
		}
	}

	streamCancel()

	// Find expert finished event
	var finishedEvent *event.StreamEvent
	for i := range events {
		if events[i].Type == event.EventTypeExpertFinished {
			finishedEvent = &events[i]
			break
		}
	}

	s.Require().NotNil(finishedEvent, "Should have expert finished event")

	// Verify attribution fields
	s.NotEmpty(finishedEvent.WorkerID)
	s.Contains(finishedEvent.WorkerID, "test-expert")

	// Verify metadata
	if meta := finishedEvent.Metadata; meta != nil {
		s.NotNil(meta, "Should have metadata")
	}
}

// filterEventsByType filters events by type
func filterEventsByType(events []event.StreamEvent, eventType event.EventType) []event.StreamEvent {
	filtered := make([]event.StreamEvent, 0)
	for _, ev := range events {
		if ev.Type == eventType {
			filtered = append(filtered, ev)
		}
	}
	return filtered
}

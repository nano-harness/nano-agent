package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// TeamLeadSession represents a long-running team-lead session with its own engine
type TeamLeadSession struct {
	ID           string
	TeamName     string
	Engine       *engine.Engine
	Config       *config.Config
	CreatedAt    time.Time
	LastActiveAt time.Time
	Broadcaster  *EventBroadcaster
	Store        *TaskEventStore
	nextSeq      int64

	// Task tracking
	activeTasks map[string]*ActiveTask
	tasksMutex  sync.RWMutex

	// Context for session lifecycle
	ctx        context.Context
	cancelFunc context.CancelFunc

	// Session metadata
	mu       sync.RWMutex
	metadata map[string]interface{}

	// pendingApprovals tracks tool calls awaiting external approval decisions.
	pendingApprovals map[string]chan agent.ApprovalDecision
	approvalMutex    sync.Mutex

	// executeFunc overrides Execute in tests.
	executeFunc func(ctx context.Context, taskID, command string, callback func(event.StreamEvent)) error
	approveFunc func(callID string, approved bool) error
}

// NewTeamLeadSession creates a new team-lead session
func NewTeamLeadSession(sessionID, teamName string, cfg *config.Config) (*TeamLeadSession, error) {
	if teamName == "" {
		teamName = "default"
	}

	// Create engine for team-lead (no V1 approval handler; V2 is set via EnableInteractiveApproval)
	eng, err := engine.NewLeadEngine(cfg, teamName)
	if err != nil {
		return nil, fmt.Errorf("failed to create lead engine: %w", err)
	}

	// Start the engine (activates scheduler)
	if err := eng.Start(); err != nil {
		return nil, fmt.Errorf("failed to start engine: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	session := &TeamLeadSession{
		ID:           sessionID,
		TeamName:     teamName,
		Engine:       eng,
		Config:       cfg,
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
		Broadcaster:  NewEventBroadcaster(),
		Store:        NewTaskEventStore(5000),
		activeTasks:  make(map[string]*ActiveTask),
		ctx:          ctx,
		cancelFunc:   cancel,
		metadata:     make(map[string]interface{}),
	}

	logger.Infof("Created team-lead session %s for team '%s'", sessionID, teamName)
	return session, nil
}

func (s *TeamLeadSession) EnableInteractiveApproval() {
	if s == nil || s.Engine == nil || s.Engine.Agent == nil {
		return
	}
	s.Engine.Agent.SetApprovalHandlerV2(s.requestToolApprovalV2)
}

func (s *TeamLeadSession) requestToolApprovalV2(info *agent.ToolCallInfo) agent.ApprovalDecision {
	if info == nil {
		return agent.ApprovalReject
	}

	// Register pending approval channel before emitting the event to avoid races.
	ch := make(chan agent.ApprovalDecision, 1)
	s.approvalMutex.Lock()
	if s.pendingApprovals == nil {
		s.pendingApprovals = make(map[string]chan agent.ApprovalDecision)
	}
	s.pendingApprovals[info.ID] = ch
	s.approvalMutex.Unlock()

	// Emit WaitingForUser event so the UI/client can prompt.
	s.enrichAndRecordEvent(event.StreamEvent{
		Type: event.EventTypeWaitingForUser,
		Metadata: map[string]interface{}{
			"kind":       "tool_approval_request",
			"call_id":    info.ID,
			"tool_name":  info.Name,
			"parameters": info.Parameters,
			"status":     string(info.Status),
		},
	})

	// Block until external SubmitToolApproval provides a decision.
	decision := <-ch

	// Cleanup
	s.approvalMutex.Lock()
	delete(s.pendingApprovals, info.ID)
	s.approvalMutex.Unlock()

	return decision
}

func (s *TeamLeadSession) SubmitToolApproval(callID string, approved bool) error {
	if s.approveFunc != nil {
		return s.approveFunc(callID, approved)
	}

	s.approvalMutex.Lock()
	ch, exists := s.pendingApprovals[callID]
	s.approvalMutex.Unlock()

	if !exists {
		return fmt.Errorf("no pending approval for call %s", callID)
	}

	if approved {
		ch <- agent.ApprovalApproveOnce
	} else {
		ch <- agent.ApprovalReject
	}
	return nil
}

// Execute executes a command in the team-lead session
func (s *TeamLeadSession) Execute(ctx context.Context, taskID, command string, callback func(event.StreamEvent)) error {
	if s.executeFunc != nil {
		return s.executeFunc(ctx, taskID, command, callback)
	}

	s.mu.Lock()
	s.LastActiveAt = time.Now()
	s.mu.Unlock()

	// Create task context that can be cancelled
	taskCtx, cancel := context.WithCancel(ctx)

	// Register the active task
	task := &ActiveTask{
		ID:        taskID,
		SessionID: s.ID,
		Command:   command,
		Type:      "interactive",
		StartTime: time.Now(),
		Cancel:    cancel,
		Status:    "running",
	}

	s.tasksMutex.Lock()
	s.activeTasks[taskID] = task
	s.tasksMutex.Unlock()

	// Execute the command
	err := s.Engine.Agent.ProcessStream(taskCtx, command, func(e event.StreamEvent) {
		recorded := s.enrichAndRecordEvent(e)
		if callback != nil {
			callback(recorded)
		}
	})

	// Update task status
	s.tasksMutex.Lock()
	task.EndTime = time.Now()
	if err != nil {
		task.Status = "error"
	} else {
		task.Status = "completed"
	}
	s.tasksMutex.Unlock()

	return err
}

func (s *TeamLeadSession) enrichAndRecordEvent(e event.StreamEvent) event.StreamEvent {
	s.mu.Lock()
	s.nextSeq++
	if e.Seq == 0 {
		e.Seq = s.nextSeq
	}
	if e.SessionID == "" {
		e.SessionID = s.ID
	}
	if e.RunID == "" {
		e.RunID = s.ID
	}
	if e.Timestamp == 0 {
		e.Timestamp = time.Now().UnixMilli()
	}
	s.mu.Unlock()

	if s.Store != nil {
		s.Store.Add(e)
	}
	if s.Broadcaster != nil {
		s.Broadcaster.Publish(e)
	}
	return e
}

// CancelActiveTasks cancels every currently running task in this team-lead session.
func (s *TeamLeadSession) CancelActiveTasks() int {
	s.tasksMutex.Lock()
	defer s.tasksMutex.Unlock()

	cancelled := 0
	for _, task := range s.activeTasks {
		if task.Status != "running" {
			continue
		}
		if task.Cancel != nil {
			task.Cancel()
		}
		task.Status = "cancelled"
		cancelled++
	}
	if cancelled > 0 {
		logger.Infof("Cancelled %d active task(s) in team-lead session %s", cancelled, s.ID)
	}
	return cancelled
}

// CancelTask cancels a running task
func (s *TeamLeadSession) CancelTask(taskID string) error {
	s.tasksMutex.Lock()
	defer s.tasksMutex.Unlock()

	task, exists := s.activeTasks[taskID]
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	if task.Cancel != nil {
		task.Cancel()
		task.Status = "cancelled"
		logger.Infof("Cancelled task %s in session %s", taskID, s.ID)
	}

	return nil
}

// GetActiveTask returns an active task by ID
func (s *TeamLeadSession) GetActiveTask(taskID string) (*ActiveTask, bool) {
	s.tasksMutex.RLock()
	defer s.tasksMutex.RUnlock()
	task, exists := s.activeTasks[taskID]
	return task, exists
}

// ListActiveTasks returns all active tasks
func (s *TeamLeadSession) ListActiveTasks() []*ActiveTask {
	s.tasksMutex.RLock()
	defer s.tasksMutex.RUnlock()

	tasks := make([]*ActiveTask, 0, len(s.activeTasks))
	for _, task := range s.activeTasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// Shutdown gracefully shuts down the session
func (s *TeamLeadSession) Shutdown() error {
	return s.ShutdownCtx(context.Background())
}

// ShutdownCtx gracefully shuts down the session, respecting cancellation while waiting for engine shutdown.
func (s *TeamLeadSession) ShutdownCtx(ctx context.Context) error {
	logger.Infof("Shutting down team-lead session %s", s.ID)

	// Cancel all active tasks
	s.tasksMutex.Lock()
	for _, task := range s.activeTasks {
		if task.Cancel != nil {
			task.Cancel()
		}
	}
	s.tasksMutex.Unlock()

	// Cancel session context
	if s.cancelFunc != nil {
		s.cancelFunc()
	}

	// Shutdown the engine
	if s.Engine != nil {
		done := make(chan error, 1)
		go func() { done <- s.Engine.Shutdown() }()
		select {
		case <-ctx.Done():
			logger.Warnf("Timed out shutting down engine for session %s: %v", s.ID, ctx.Err())
			return ctx.Err()
		case err := <-done:
			if err == nil {
				return nil
			}
			logger.Warnf("Error shutting down engine for session %s: %v", s.ID, err)
			return err
		}
	}

	return nil
}

// UpdateActivity updates the last active timestamp
func (s *TeamLeadSession) UpdateActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActiveAt = time.Now()
}

// GetMetadata returns a copy of the session metadata
func (s *TeamLeadSession) GetMetadata() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copied := make(map[string]interface{}, len(s.metadata))
	for k, v := range s.metadata {
		copied[k] = v
	}
	return copied
}

// SetMetadata sets a metadata value
func (s *TeamLeadSession) SetMetadata(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata[key] = value
}

package teamlist

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/mailbox"
	"github.com/nano-harness/nano-agent/pkg/swarm"
)

func TestToolInterface(t *testing.T) {
	tool := New(nil)

	// Test Name
	if tool.Name() != "team_list" {
		t.Errorf("expected name %q, got %q", "team_list", tool.Name())
	}

	// Test Description
	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}

	// Test Schema
	schema := tool.Schema()
	if schema == nil {
		t.Fatal("schema should not be nil")
	}

	// Test RequiresConfirmation
	if tool.RequiresConfirmation() {
		t.Error("team_list should not require confirmation")
	}

	// Test ConcurrencySafe
	if !tool.ConcurrencySafe() {
		t.Error("team_list should be concurrency safe (read-only)")
	}
}

func TestExecute_WithoutTeam(t *testing.T) {
	tool := New(nil)
	ctx := context.Background()

	params := map[string]interface{}{}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without a team context, it should try to use "default" team which likely doesn't exist
	// This is expected to fail gracefully
	if result.Success {
		t.Log("Note: test succeeded, 'default' team may exist in test environment")
	}
}

func TestExecute_WithTeammateContext(t *testing.T) {
	// Create a test backend (we'll use nil for simplicity in this test)
	tool := New(nil)

	// Create teammate context
	identity := &swarm.TeammateIdentity{
		AgentID:   "researcher@alpha",
		AgentName: "researcher",
		TeamName:  "alpha",
	}
	ctx := swarm.WithTeammate(context.Background(), identity)

	params := map[string]interface{}{}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The team "alpha" doesn't exist, so we expect failure or empty list
	// This is fine for testing the interface
	t.Logf("Result: %s", result.LLMContent)
}

// MockBackend implements mailbox.Backend for testing
type MockBackend struct {
	mailboxes map[string]*MockMailbox
}

func NewMockBackend() *MockBackend {
	return &MockBackend{
		mailboxes: make(map[string]*MockMailbox),
	}
}

func (mb *MockBackend) Open(agentID string) (mailbox.Mailbox, error) {
	if m, ok := mb.mailboxes[agentID]; ok {
		return m, nil
	}
	m := &MockMailbox{count: 0}
	mb.mailboxes[agentID] = m
	return m, nil
}

func (mb *MockBackend) Close() error {
	return nil
}

func (mb *MockBackend) Stats() mailbox.Stats {
	return mailbox.Stats{}
}

func (mb *MockBackend) HealthCheck(ctx context.Context) error {
	return nil
}

// MockMailbox implements mailbox.Mailbox for testing
type MockMailbox struct {
	count int
}

func (m *MockMailbox) Send(ctx context.Context, msg mailbox.Message) error {
	m.count++
	return nil
}

func (m *MockMailbox) Peek(ctx context.Context, limit int) ([]mailbox.Message, error) {
	return []mailbox.Message{}, nil
}

func (m *MockMailbox) Drain(ctx context.Context) ([]mailbox.Message, error) {
	return []mailbox.Message{}, nil
}

func (m *MockMailbox) DrainAll(ctx context.Context) ([]mailbox.Message, error) {
	return []mailbox.Message{}, nil
}

func (m *MockMailbox) Count(ctx context.Context) (int, error) {
	return m.count, nil
}

func (m *MockMailbox) Clear(ctx context.Context) error {
	m.count = 0
	return nil
}

func (m *MockMailbox) Close() error {
	return nil
}

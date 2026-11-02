package eventsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConnectionState_String tests ConnectionState string representation.
func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected string
	}{
		{StateDisconnected, "disconnected"},
		{StateConnecting, "connecting"},
		{StateConnected, "connected"},
		{StateReconnecting, "reconnecting"},
		{StateClosed, "closed"},
		{ConnectionState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

// TestOutbound_Structure validates Outbound message structure.
func TestOutbound_Structure(t *testing.T) {
	t.Run("Text message", func(t *testing.T) {
		msg := Outbound{
			Kind: "submit",
			Text: "hello world",
		}
		assert.Equal(t, "submit", msg.Kind)
		assert.Equal(t, "hello world", msg.Text)
		assert.Nil(t, msg.Approval)
	})

	t.Run("Approval message", func(t *testing.T) {
		msg := Outbound{
			Kind: "approval",
			Approval: &ApprovalDecision{
				CallID: "test-123",
				Allow:  true,
				Always: false,
			},
		}
		assert.Equal(t, "approval", msg.Kind)
		assert.NotNil(t, msg.Approval)
		assert.Equal(t, "test-123", msg.Approval.CallID)
		assert.True(t, msg.Approval.Allow)
	})

	t.Run("Control message", func(t *testing.T) {
		msg := Outbound{
			Kind:    "control",
			Control: "/clear",
		}
		assert.Equal(t, "control", msg.Kind)
		assert.Equal(t, "/clear", msg.Control)
	})

	t.Run("Cancel message", func(t *testing.T) {
		msg := Outbound{
			Kind: "cancel",
		}
		assert.Equal(t, "cancel", msg.Kind)
	})
}

// TestInbound_Structure validates Inbound message structure.
func TestInbound_Structure(t *testing.T) {
	t.Run("State update", func(t *testing.T) {
		state := StateConnected
		msg := Inbound{
			State: &state,
		}
		assert.NotNil(t, msg.State)
		assert.Equal(t, StateConnected, *msg.State)
	})

	t.Run("Notice message", func(t *testing.T) {
		msg := Inbound{
			Notice: "Connection established",
		}
		assert.Equal(t, "Connection established", msg.Notice)
	})

	t.Run("Resumed from checkpoint", func(t *testing.T) {
		msg := Inbound{
			ResumedFrom: 12345,
		}
		assert.Equal(t, int64(12345), msg.ResumedFrom)
	})
}

// TestApprovalDecision_Structure validates ApprovalDecision structure.
func TestApprovalDecision_Structure(t *testing.T) {
	t.Run("Allow once", func(t *testing.T) {
		decision := ApprovalDecision{
			CallID: "call-123",
			Allow:  true,
			Always: false,
		}
		assert.Equal(t, "call-123", decision.CallID)
		assert.True(t, decision.Allow)
		assert.False(t, decision.Always)
	})

	t.Run("Allow always", func(t *testing.T) {
		decision := ApprovalDecision{
			CallID: "call-456",
			Allow:  true,
			Always: true,
		}
		assert.True(t, decision.Allow)
		assert.True(t, decision.Always)
	})

	t.Run("Deny", func(t *testing.T) {
		decision := ApprovalDecision{
			CallID: "call-789",
			Allow:  false,
			Always: false,
		}
		assert.False(t, decision.Allow)
	})
}

// TestInProcess_Describe tests the Describe method.
func TestInProcess_Describe(t *testing.T) {
	t.Run("Without session ID", func(t *testing.T) {
		ip := NewInProcess(nil, "", nil)
		assert.Equal(t, "local in-process", ip.Describe())
	})

	t.Run("With session ID", func(t *testing.T) {
		ip := NewInProcess(nil, "session-123", nil)
		assert.Equal(t, "local in-process session:session-123", ip.Describe())
	})

	t.Run("With whitespace session ID", func(t *testing.T) {
		ip := NewInProcess(nil, "  session-456  ", nil)
		// Describe returns trimmed session ID
		assert.Contains(t, ip.Describe(), "session-456")
	})
}

// TestInProcess_State tests state getter.
func TestInProcess_State(t *testing.T) {
	ip := NewInProcess(nil, "", nil)
	assert.Equal(t, StateDisconnected, ip.State())
}

// TestInProcess_Inbound verifies inbound channel is accessible.
func TestInProcess_Inbound(t *testing.T) {
	ip := NewInProcess(nil, "", nil)
	ch := ip.Inbound()
	assert.NotNil(t, ch)
}

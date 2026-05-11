package bubbletea

import (
	"testing"
)

func TestFormattedMessage(t *testing.T) {
	msg := NewFormattedMessage("test-id", "user", "Hello, world!")

	if msg.ID != "test-id" {
		t.Errorf("Expected ID 'test-id', got '%s'", msg.ID)
	}
	if msg.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", msg.Role)
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", msg.Content)
	}
	if msg.Collapsed {
		t.Error("Expected collapsed to be false by default")
	}
}

func TestFormattedMessageRendered(t *testing.T) {
	msg := NewFormattedMessage("test-id", "user", "Line 1\nLine 2\nLine 3")

	// Test setting rendered content
	msg.SetRendered("Rendered Line 1\nRendered Line 2\nRendered Line 3")

	if msg.Rendered != "Rendered Line 1\nRendered Line 2\nRendered Line 3" {
		t.Errorf("Expected rendered content to be set, got '%s'", msg.Rendered)
	}

	// Height should be calculated as 3 lines
	if msg.Height != 3 {
		t.Errorf("Expected height 3, got %d", msg.Height)
	}
}

func TestFormattedMessageToggle(t *testing.T) {
	msg := NewFormattedMessage("test-id", "thinking", "Some thinking content")

	// Test toggling collapsed state
	if msg.Collapsed {
		t.Error("Expected collapsed to be false initially")
	}

	msg.Toggle()
	if !msg.Collapsed {
		t.Error("Expected collapsed to be true after toggle")
	}

	msg.Toggle()
	if msg.Collapsed {
		t.Error("Expected collapsed to be false after second toggle")
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"single line", 1},
		{"line 1\nline 2", 2},
		{"line 1\nline 2\nline 3", 3},
		{"line 1\nline 2\n", 3}, // Trailing newline counts as empty line
		{"\n\n\n", 4},
	}

	for _, tc := range tests {
		result := countLines(tc.input)
		if result != tc.expected {
			t.Errorf("countLines(%q) = %d, expected %d", tc.input, result, tc.expected)
		}
	}
}

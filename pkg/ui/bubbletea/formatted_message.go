package bubbletea

import (
	"time"
)

// FormattedMessage represents a structured message in the fullscreen TUI.
// It caches rendered content and height for efficient virtual scrolling.
type FormattedMessage struct {
	ID        string                 // Unique identifier
	Role      string                 // user/assistant/tool/thinking/error/system
	Content   string                 // Raw content
	Rendered  string                 // Rendered output with formatting (cached)
	Height    int                    // Height in lines (cached)
	Collapsed bool                   // Whether content is collapsed (for thinking)
	Timestamp time.Time              // When the message was created
	Metadata  map[string]interface{} // Additional metadata
}

// NewFormattedMessage creates a new formatted message.
func NewFormattedMessage(id, role, content string) *FormattedMessage {
	return &FormattedMessage{
		ID:        id,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
}

// SetRendered caches the rendered output and calculates height.
func (m *FormattedMessage) SetRendered(rendered string) {
	m.Rendered = rendered
	m.Height = countLines(rendered)
}

// Toggle toggles the collapsed state (for thinking messages).
func (m *FormattedMessage) Toggle() {
	m.Collapsed = !m.Collapsed
}

// countLines counts the number of lines in a string.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	count := 1
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			count++
		}
	}
	return count
}

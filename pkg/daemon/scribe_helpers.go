package daemon

import (
	"fmt"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

func isSignificantEvent(ev event.StreamEvent) bool {
	switch ev.Type {
	case event.EventTypeToolResult, event.EventTypeDone, event.EventTypeError, event.EventTypeTaskCompletion:
		return true
	default:
		return false
	}
}

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
		if t, ok := val.(time.Time); ok {
			return t.Format(time.RFC3339)
		}
		return fmt.Sprintf("%v", val)
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case float32:
			return int(v)
		case int64:
			return int(v)
		}
	}
	return 0
}

// sanitizeImages returns a copy of the image slice with Base64 data removed,
// keeping only URL and MimeType. This prevents large binary payloads from
// being persisted in the journal or sent over WebSocket frames.
func sanitizeImages(images []llm.MultimodalImage) []llm.MultimodalImage {
	if images == nil {
		return nil
	}
	sanitized := make([]llm.MultimodalImage, len(images))
	for i, img := range images {
		sanitized[i] = llm.MultimodalImage{
			URL:      img.URL,
			MimeType: img.MimeType,
		}
	}
	return sanitized
}

package event

import "time"

// NewStreamEvent creates a new StreamEvent with timestamp and source
func NewStreamEvent(eventType EventType, source string) StreamEvent {
	return StreamEvent{
		Type:      eventType,
		Timestamp: time.Now().Unix(),
		Source:    source,
	}
}

// WithID adds an ID to the event
func (e StreamEvent) WithID(id string) StreamEvent {
	e.ID = id
	return e
}

// WithContent adds content to the event
func (e StreamEvent) WithContent(content string) StreamEvent {
	e.Content = content
	return e
}

// WithError adds error information to the event
func (e StreamEvent) WithError(err string, severity string) StreamEvent {
	e.Error = err
	e.Severity = severity
	return e
}

// WithProgress adds progress information to the event
func (e StreamEvent) WithProgress(progress float64) StreamEvent {
	e.Progress = progress
	return e
}

// WithTaskID adds task ID to the event
func (e StreamEvent) WithTaskID(taskID string) StreamEvent {
	e.TaskID = taskID
	return e
}

// WithMetadata adds metadata to the event
func (e StreamEvent) WithMetadata(key string, value interface{}) StreamEvent {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// WithRetryCount adds retry count to the event
func (e StreamEvent) WithRetryCount(count int) StreamEvent {
	e.RetryCount = count
	return e
}

// WithCorrelationID adds correlation ID to the event
func (e StreamEvent) WithCorrelationID(id string) StreamEvent {
	e.CorrelationID = id
	return e
}

// WithSessionID adds session ID to the event
func (e StreamEvent) WithSessionID(sessionID string) StreamEvent {
	e.SessionID = sessionID
	return e
}

// WithPayload adds a typed payload while preserving legacy top-level fields.
func (e StreamEvent) WithPayload(payload interface{}) StreamEvent {
	e.Payload = payload
	return e
}

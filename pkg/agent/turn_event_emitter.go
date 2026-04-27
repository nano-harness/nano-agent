package agent

import "github.com/nano-harness/nano-agent/pkg/event"

type turnEventEmitter struct {
	turnID  string
	handler func(event.StreamEvent)
}

func (t *Turn) events() turnEventEmitter {
	return turnEventEmitter{
		turnID:  t.ID,
		handler: t.eventHandler,
	}
}

func (e turnEventEmitter) emit(eventType event.EventType, content string, metadata map[string]interface{}) {
	e.emitPayload(eventType, content, metadata, nil)
}

func (e turnEventEmitter) emitPayload(eventType event.EventType, content string, metadata map[string]interface{}, payload interface{}) {
	if e.handler == nil {
		return
	}
	ev := event.NewStreamEvent(eventType, "agent_turn").WithContent(content)
	if payload != nil {
		ev = ev.WithPayload(payload)
	}
	ev = ev.WithMetadata("turn_id", e.turnID)
	for key, value := range metadata {
		ev = ev.WithMetadata(key, value)
	}
	e.handler(ev)
}

func (e turnEventEmitter) plannerPlanSnapshot(steps []map[string]interface{}) {
	payloadSteps := make([]event.PlannerPlanStepPayload, 0, len(steps))
	for _, step := range steps {
		payloadSteps = append(payloadSteps, event.PlannerPlanStepPayload{
			ID:     stringFromMap(step, "id"),
			Title:  stringFromMap(step, "title"),
			Status: stringFromMap(step, "status"),
		})
	}
	e.emitPayload(event.EventTypePlannerPlanSnapshot, "planner plan snapshot", map[string]interface{}{
		"steps": steps,
	}, event.PlannerPlanSnapshotPayload{TurnID: e.turnID, Steps: payloadSteps})
}

func (e turnEventEmitter) executorState(state string, metadata map[string]interface{}) {
	e.emitPayload(event.EventTypeExecutorState, state, metadata, event.ExecutorStatePayload{
		TurnID:   e.turnID,
		State:    state,
		Metadata: metadata,
	})
}

func (e turnEventEmitter) plannerDecision(decision string, metadata map[string]interface{}) {
	e.emitPayload(event.EventTypePlannerDecision, decision, metadata, event.PlannerDecisionPayload{
		TurnID:   e.turnID,
		Decision: decision,
		Metadata: metadata,
	})
}

func (e turnEventEmitter) executorSchedule(toolsToExecute []ToolToExecute) {
	if len(toolsToExecute) == 0 {
		return
	}
	names := make([]string, 0, len(toolsToExecute))
	for _, te := range toolsToExecute {
		if te.Name != "" {
			names = append(names, te.Name)
		}
	}
	e.emitPayload(event.EventTypeExecutorSchedule, "scheduled_workers", map[string]interface{}{
		"workers_count": len(toolsToExecute),
		"tool_names":    names,
	}, event.ExecutorSchedulePayload{
		TurnID:       e.turnID,
		WorkersCount: len(toolsToExecute),
		ToolNames:    names,
	})
}

func stringFromMap(values map[string]interface{}, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

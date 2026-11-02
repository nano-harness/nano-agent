package event

type PlannerPlanStepPayload struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type PlannerPlanSnapshotPayload struct {
	TurnID string                   `json:"turn_id"`
	Steps  []PlannerPlanStepPayload `json:"steps"`
}

type PlannerDecisionPayload struct {
	TurnID   string                 `json:"turn_id"`
	Decision string                 `json:"decision"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type ExecutorStatePayload struct {
	TurnID   string                 `json:"turn_id"`
	State    string                 `json:"state"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type ExecutorSchedulePayload struct {
	TurnID       string   `json:"turn_id"`
	WorkersCount int      `json:"workers_count"`
	ToolNames    []string `json:"tool_names,omitempty"`
}

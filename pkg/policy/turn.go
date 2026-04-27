package policy

type TurnState struct {
	ContextErr               error
	TaskCompleted            bool
	ConsecutiveErrors        int
	ErrorThreshold           int
	DiminishingReturns       bool
	DiminishingReturnsWindow int
	SimilarContentLoop       bool
	SimilarContentThreshold  int
}

type TurnPolicy struct{}

func NewTurnPolicy() TurnPolicy {
	return TurnPolicy{}
}

func (TurnPolicy) Evaluate(state TurnState) Decision {
	if state.ContextErr != nil {
		return NewDecision(ActionTerminate, state.ContextErr.Error()).
			WithMetadata("reason", "context_done")
	}
	if state.TaskCompleted {
		return NewDecision(ActionTerminate, "task_completed").
			WithMetadata("reason", "task_completed")
	}
	if state.ErrorThreshold > 0 && state.ConsecutiveErrors >= state.ErrorThreshold {
		return NewDecision(ActionTerminate, "error_threshold_exceeded").
			WithMetadata("reason", "error_threshold").
			WithMetadata("consecutive_errors", state.ConsecutiveErrors).
			WithMetadata("error_threshold", state.ErrorThreshold)
	}
	if state.DiminishingReturns {
		return NewDecision(ActionTerminate, "diminishing_returns").
			WithMetadata("reason", "diminishing_returns").
			WithMetadata("window", state.DiminishingReturnsWindow)
	}
	if state.SimilarContentLoop {
		return NewDecision(ActionTerminate, "similar_content_loop").
			WithMetadata("reason", "similar_content_loop").
			WithMetadata("threshold", state.SimilarContentThreshold)
	}
	return NewDecision(ActionContinue, "")
}

package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
)

const (
	defaultGoalMaxConditionLength = 4000
	defaultGoalMaxTurns           = 50
)

// GoalState is the serializable snapshot of a goal attached to a session.
type GoalState struct {
	Condition      string     `json:"condition,omitempty"`
	Active         bool       `json:"active,omitempty"`
	StartedAt      time.Time  `json:"started_at,omitempty"`
	TurnsEvaluated int        `json:"turns_evaluated,omitempty"`
	TokensSpent    int        `json:"tokens_spent,omitempty"`
	MaxTurns       int        `json:"max_turns,omitempty"`
	LastReason     string     `json:"last_reason,omitempty"`
	AchievedAt     *time.Time `json:"achieved_at,omitempty"`
}

// GoalContext tracks /goal lifecycle state for a session.
type GoalContext struct {
	mu                 sync.Mutex
	condition          string
	active             bool
	startedAt          time.Time
	turnsEvaluated     int
	tokensSpent        int
	lastReason         string
	achievedAt         *time.Time
	maxConditionLength int
	maxTurns           int
	onChange           func(GoalState)
}

func NewGoalContext(cfg *config.Config) *GoalContext {
	g := &GoalContext{}
	g.Configure(cfg)
	return g
}

func NewGoalContextFromState(cfg *config.Config, state *GoalState) *GoalContext {
	g := NewGoalContext(cfg)
	if state != nil {
		g.condition = state.Condition
		g.active = state.Active
		g.startedAt = state.StartedAt
		g.turnsEvaluated = state.TurnsEvaluated
		g.tokensSpent = state.TokensSpent
		if state.MaxTurns > 0 {
			g.maxTurns = state.MaxTurns
		}
		g.lastReason = state.LastReason
		if state.AchievedAt != nil {
			t := *state.AchievedAt
			g.achievedAt = &t
		}
	}
	return g
}

func (g *GoalContext) Configure(cfg *config.Config) {
	if g == nil {
		return
	}
	maxConditionLength := defaultGoalMaxConditionLength
	maxTurns := defaultGoalMaxTurns
	if cfg != nil && cfg.Goal != nil {
		if cfg.Goal.MaxConditionLength > 0 {
			maxConditionLength = cfg.Goal.MaxConditionLength
		}
		if cfg.Goal.MaxTurns > 0 {
			maxTurns = cfg.Goal.MaxTurns
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.maxConditionLength = maxConditionLength
	g.maxTurns = maxTurns
}

func (g *GoalContext) SetOnChange(fn func(GoalState)) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onChange = fn
}

func (g *GoalContext) SetGoal(condition string) error {
	if g == nil {
		return nil
	}
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return fmt.Errorf("goal condition is empty")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len([]rune(condition)) > g.maxConditionLength {
		return fmt.Errorf("goal condition exceeds %d characters", g.maxConditionLength)
	}
	g.condition = condition
	g.active = true
	g.startedAt = time.Now()
	g.turnsEvaluated = 0
	g.tokensSpent = 0
	g.lastReason = ""
	g.achievedAt = nil
	g.notifyLocked()
	return nil
}

func (g *GoalContext) Clear() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.condition = ""
	g.active = false
	g.startedAt = time.Time{}
	g.turnsEvaluated = 0
	g.tokensSpent = 0
	g.lastReason = ""
	g.achievedAt = nil
	g.notifyLocked()
}

func (g *GoalContext) IsActive() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}

func (g *GoalContext) Condition() string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.condition
}

func (g *GoalContext) MaxTurns() int {
	if g == nil {
		return defaultGoalMaxTurns
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.maxTurns
}

func (g *GoalContext) TurnsEvaluated() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.turnsEvaluated
}

func (g *GoalContext) MarkEvaluated(tokens int, reason string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.turnsEvaluated++
	if tokens > 0 {
		g.tokensSpent += tokens
	}
	g.lastReason = strings.TrimSpace(reason)
	g.notifyLocked()
}

func (g *GoalContext) MarkAchieved(reason string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	g.active = false
	g.lastReason = strings.TrimSpace(reason)
	g.achievedAt = &now
	g.notifyLocked()
}

// MarkStopped marks the goal inactive while preserving its condition and
// evaluation counters for final reporting. Use Clear when the user explicitly
// cancels or resets the goal and the state should be discarded.
func (g *GoalContext) MarkStopped(reason string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active = false
	g.lastReason = strings.TrimSpace(reason)
	g.notifyLocked()
}

func (g *GoalContext) Status() string {
	if g == nil {
		return "No active goal."
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.condition == "" {
		return "No active goal."
	}
	state := "active"
	if !g.active {
		state = "inactive"
	}
	if g.achievedAt != nil {
		state = "achieved"
	}
	msg := fmt.Sprintf("/goal %s: %s\nturns evaluated: %d/%d, tokens spent: %d",
		state, g.condition, g.turnsEvaluated, g.maxTurns, g.tokensSpent)
	if g.lastReason != "" {
		msg += "\nlast reason: " + g.lastReason
	}
	return msg
}

func (g *GoalContext) Snapshot() GoalState {
	if g == nil {
		return GoalState{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshotLocked()
}

func (g *GoalContext) snapshotLocked() GoalState {
	var achievedAt *time.Time
	if g.achievedAt != nil {
		t := *g.achievedAt
		achievedAt = &t
	}
	return GoalState{
		Condition:      g.condition,
		Active:         g.active,
		StartedAt:      g.startedAt,
		TurnsEvaluated: g.turnsEvaluated,
		TokensSpent:    g.tokensSpent,
		MaxTurns:       g.maxTurns,
		LastReason:     g.lastReason,
		AchievedAt:     achievedAt,
	}
}

func (g *GoalContext) notifyLocked() {
	if g.onChange != nil {
		g.onChange(g.snapshotLocked())
	}
}

// Package watcher implements event-driven monitoring that polls external
// sources at configured intervals and triggers agent commands on matching events.
package watcher

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/google/uuid"
)

// Rule describes a single event-monitoring rule.
type Rule struct {
	// ID is the unique identifier of the rule.
	ID string
	// Source is the event source type: "aone" or "shell".
	Source string
	// Event is the event type to match, e.g. "new_mr", "ci_failure", "custom".
	Event string
	// Filter is an optional filter string passed to the source.
	Filter string
	// Command is the agent command template executed on each matching event.
	// Supports Go template variables: {{.VAR}}.
	Command string
	// Interval is how often the source is polled. Defaults to 5 minutes.
	Interval time.Duration
	// Timeout is the maximum time allowed per command execution. Defaults to 30 minutes.
	Timeout time.Duration
	// ShellCommand is used by the "shell" source to specify the command to run.
	ShellCommand string
}

// WatchEvent represents a single detected event from a source.
type WatchEvent struct {
	// Source is the originating source type.
	Source string
	// Type is the event type, e.g. "new_mr".
	Type string
	// Payload holds template variables for command rendering.
	Payload map[string]string
}

// Source is implemented by every event source. It is a poll-based interface:
// each call returns new events that occurred since the given checkpoint.
type Source interface {
	// Poll fetches new events. filter is the rule's filter expression.
	// since is the last successful poll time (zero on first run).
	// Returns new events and the new checkpoint time.
	Poll(ctx context.Context, filter string, since time.Time) ([]WatchEvent, time.Time, error)
}

// Watcher manages a collection of monitoring rules and runs them concurrently.
type Watcher struct {
	mu         sync.RWMutex
	rules      map[string]*ruleState
	executor   func(command string) error
	stateStore *config.StateStore
	running    bool
	wg         sync.WaitGroup
}

type ruleState struct {
	Rule     Rule
	LastPoll time.Time
	cancelFn context.CancelFunc
}

// New creates a new Watcher.
// executor is called with a fully rendered command string whenever an event fires.
// stateStore may be nil (rules are not persisted between restarts).
func New(executor func(command string) error, stateStore *config.StateStore) *Watcher {
	return &Watcher{
		rules:      make(map[string]*ruleState),
		executor:   executor,
		stateStore: stateStore,
	}
}

// AddRule registers a new monitoring rule. If the rule's ID is empty, one is
// generated. If the Watcher is already running, the rule's polling goroutine
// is started immediately. The rule is persisted to the state store if one is
// configured.
func (w *Watcher) AddRule(r Rule) Rule {
	r = w.addRule(r, true)
	return r
}

// AddRuleNoStore registers a rule without persisting it to the state store.
// Use this for rules loaded from static configuration that are re-applied on
// every startup, to avoid accumulating duplicate entries in the state store.
func (w *Watcher) AddRuleNoStore(r Rule) Rule {
	r = w.addRule(r, false)
	return r
}

// addRule is the shared implementation for AddRule and AddRuleNoStore.
func (w *Watcher) addRule(r Rule, persist bool) Rule {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	if r.Interval <= 0 {
		r.Interval = 5 * time.Minute
	}
	if r.Timeout <= 0 {
		r.Timeout = 30 * time.Minute
	}

	w.mu.Lock()
	rs := &ruleState{Rule: r}
	w.rules[r.ID] = rs
	running := w.running

	if running {
		ctx, cancel := context.WithCancel(context.Background())
		rs.cancelFn = cancel
		w.wg.Add(1)
		go w.runRule(ctx, rs)
	}
	w.mu.Unlock()

	if persist && w.stateStore != nil {
		w.stateStore.AddWatcherRule(config.PersistedWatchRule{
			ID:           r.ID,
			Source:       r.Source,
			Event:        r.Event,
			Filter:       r.Filter,
			Command:      r.Command,
			Interval:     r.Interval.String(),
			Timeout:      r.Timeout.String(),
			ShellCommand: r.ShellCommand,
		})
		if err := w.stateStore.Save(); err != nil {
			logger.Warnf("watcher: failed to persist rule %s: %v", r.ID, err)
		}
	}

	logger.Infof("watcher: rule %s added (source=%s, event=%s, interval=%s)", r.ID, r.Source, r.Event, r.Interval)
	return r
}

// RemoveRule stops and removes a rule by ID. The rule is removed from the
// state store if one is configured.
func (w *Watcher) RemoveRule(id string) error {
	w.mu.Lock()
	rs, ok := w.rules[id]
	if !ok {
		w.mu.Unlock()
		return fmt.Errorf("watcher: rule not found: %s", id)
	}
	delete(w.rules, id)
	if rs.cancelFn != nil {
		rs.cancelFn()
	}
	w.mu.Unlock()

	if w.stateStore != nil {
		w.stateStore.RemoveWatcherRule(id)
		if err := w.stateStore.Save(); err != nil {
			logger.Warnf("watcher: failed to remove persisted rule %s: %v", id, err)
		}
	}

	logger.Infof("watcher: rule %s removed", id)
	return nil
}

// ListRules returns a snapshot of all currently registered rules.
func (w *Watcher) ListRules() []Rule {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Rule, 0, len(w.rules))
	for _, rs := range w.rules {
		out = append(out, rs.Rule)
	}
	return out
}

// Start launches polling goroutines for all registered rules. It also loads
// any rules persisted to the state store that are not yet registered.
func (w *Watcher) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Load persisted rules not yet in memory.
	if w.stateStore != nil {
		for _, pr := range w.stateStore.GetWatcherRules() {
			if _, exists := w.rules[pr.ID]; exists {
				continue
			}
			r := Rule{
				ID:           pr.ID,
				Source:       pr.Source,
				Event:        pr.Event,
				Filter:       pr.Filter,
				Command:      pr.Command,
				ShellCommand: pr.ShellCommand,
			}
			if d, err := time.ParseDuration(pr.Interval); err == nil {
				r.Interval = d
			} else {
				r.Interval = 5 * time.Minute
			}
			if d, err := time.ParseDuration(pr.Timeout); err == nil {
				r.Timeout = d
			} else {
				r.Timeout = 30 * time.Minute
			}
			rs := &ruleState{Rule: r}
			// Restore the last-poll checkpoint so we do not re-trigger old events.
			if pr.LastPoll != "" {
				if t, err := time.Parse(time.RFC3339, pr.LastPoll); err == nil {
					rs.LastPoll = t
				}
			}
			w.rules[pr.ID] = rs
			logger.Infof("watcher: reloaded persisted rule %s", pr.ID)
		}
	}

	// Start polling goroutines for all rules.
	w.running = true
	for _, rs := range w.rules {
		ctx, cancel := context.WithCancel(context.Background())
		rs.cancelFn = cancel
		w.wg.Add(1)
		go w.runRule(ctx, rs)
	}

	logger.Info("watcher started")
}

// Stop cancels all polling goroutines and waits for them to finish.
func (w *Watcher) Stop() {
	w.mu.Lock()
	w.running = false
	for _, rs := range w.rules {
		if rs.cancelFn != nil {
			rs.cancelFn()
		}
	}
	w.mu.Unlock()

	w.wg.Wait()
	logger.Info("watcher stopped")
}

// runRule is the polling loop for a single rule.
func (w *Watcher) runRule(ctx context.Context, rs *ruleState) {
	defer w.wg.Done()

	src := newSource(rs.Rule)
	if src == nil {
		logger.Warnf("watcher: unknown source type %q for rule %s, skipping", rs.Rule.Source, rs.Rule.ID)
		return
	}

	ticker := time.NewTicker(rs.Rule.Interval)
	defer ticker.Stop()

	// Run once immediately on startup, then respect the ticker interval.
	w.pollAndExecute(ctx, rs, src)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.pollAndExecute(ctx, rs, src)
		}
	}
}

// pollAndExecute polls the source and executes the command for each new event.
func (w *Watcher) pollAndExecute(ctx context.Context, rs *ruleState, src Source) {
	w.mu.RLock()
	since := rs.LastPoll
	w.mu.RUnlock()

	pollCtx, cancel := context.WithTimeout(ctx, rs.Rule.Timeout)
	defer cancel()

	events, newCheckpoint, err := src.Poll(pollCtx, rs.Rule.Filter, since)
	if err != nil {
		// Suppress expected cancellation/timeout errors to avoid log spam.
		if ctx.Err() == nil && pollCtx.Err() == nil {
			logger.Warnf("watcher: rule %s poll error: %v", rs.Rule.ID, err)
		}
		return
	}

	w.mu.Lock()
	rs.LastPoll = newCheckpoint
	w.mu.Unlock()

	// Persist the new checkpoint so restarts do not re-trigger old events.
	if w.stateStore != nil {
		w.stateStore.UpdateWatcherRuleCheckpoint(rs.Rule.ID, newCheckpoint)
		if err := w.stateStore.Save(); err != nil {
			logger.Warnf("watcher: failed to persist checkpoint for rule %s: %v", rs.Rule.ID, err)
		}
	}

	for _, ev := range events {
		cmd, err := renderCommand(rs.Rule.Command, ev.Payload)
		if err != nil {
			logger.Warnf("watcher: rule %s command render error: %v", rs.Rule.ID, err)
			continue
		}
		logger.Infof("watcher: rule %s firing for event %s: %s", rs.Rule.ID, ev.Type, cmd)
		if w.executor != nil {
			if err := w.executor(cmd); err != nil {
				logger.Errorf("watcher: rule %s executor error: %v", rs.Rule.ID, err)
			}
		}
	}
}

// renderCommand applies the payload to the command template.
func renderCommand(tmpl string, payload map[string]string) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}
	t, err := template.New("cmd").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse command template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, payload); err != nil {
		return "", fmt.Errorf("render command template: %w", err)
	}
	return buf.String(), nil
}

// newSource creates the appropriate Source implementation for the given rule.
func newSource(r Rule) Source {
	switch r.Source {
	case "shell":
		return &ShellSource{command: r.ShellCommand}
	case "aone":
		return &AoneSource{eventType: r.Event}
	default:
		return nil
	}
}

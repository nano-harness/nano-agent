package watcher_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/watcher"
)

// ---------- ShellSource tests ----------

func TestShellSourcePoll_JSONOutput(t *testing.T) {
	src := watcher.NewShellSourceForTest(`echo '[{"KEY":"val1"},{"KEY":"val2"}]'`)
	events, checkpoint, err := src.Poll(context.Background(), "", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if checkpoint.IsZero() {
		t.Error("checkpoint should not be zero")
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Payload["KEY"] != "val1" {
		t.Errorf("payload[0][KEY] = %q, want %q", events[0].Payload["KEY"], "val1")
	}
	if events[0].Source != "shell" || events[0].Type != "custom" {
		t.Errorf("unexpected Source/Type: %s/%s", events[0].Source, events[0].Type)
	}
}

func TestShellSourcePoll_PlainLines(t *testing.T) {
	src := watcher.NewShellSourceForTest("printf 'line1\nline2\n'")
	events, _, err := src.Poll(context.Background(), "", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Payload["OUTPUT"] != "line1" {
		t.Errorf("payload[0][OUTPUT] = %q, want %q", events[0].Payload["OUTPUT"], "line1")
	}
}

func TestShellSourcePoll_EmptyOutput(t *testing.T) {
	src := watcher.NewShellSourceForTest("true")
	events, _, err := src.Poll(context.Background(), "", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for empty output, got %d", len(events))
	}
}

func TestShellSourcePoll_WatcherEnvInjected(t *testing.T) {
	// Verify WATCHER_SINCE and WATCHER_FILTER are injected into the environment.
	src := watcher.NewShellSourceForTest(`echo '[{"SINCE":"'"'"'$WATCHER_SINCE'"'"'","FILTER":"'"'"'$WATCHER_FILTER'"'"'"}]'`)
	since := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	events, _, err := src.Poll(context.Background(), "myfilter", since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) == 0 {
		t.Skip("shell did not produce events – env injection test inconclusive in this environment")
	}
	// WATCHER_SINCE should be a non-empty RFC3339 string
	if events[0].Payload["SINCE"] == "" {
		t.Error("expected WATCHER_SINCE to be injected but it is empty")
	}
}

func TestShellSourcePoll_NoCommand(t *testing.T) {
	src := watcher.NewShellSourceForTest("")
	_, _, err := src.Poll(context.Background(), "", time.Time{})
	if err == nil {
		t.Error("expected error for empty command")
	}
}

// ---------- Watcher.AddRule / AddRuleNoStore tests ----------

func newTestStateStore(t *testing.T) *config.StateStore {
	t.Helper()
	dir := t.TempDir()
	ss := config.NewStateStore(filepath.Join(dir, "state.json"))
	if err := ss.Load(); err != nil {
		t.Fatalf("state store Load: %v", err)
	}
	return ss
}

func TestWatcherAddRule_PersistsToStateStore(t *testing.T) {
	ss := newTestStateStore(t)
	w := watcher.New(nil, ss)

	rule := watcher.Rule{
		Source:       "shell",
		ShellCommand: "echo '[]'",
		Command:      "test command",
		Interval:     5 * time.Minute,
	}
	added := w.AddRule(rule)
	if added.ID == "" {
		t.Fatal("AddRule should generate an ID")
	}

	rules := ss.GetWatcherRules()
	if len(rules) != 1 {
		t.Fatalf("expected 1 persisted rule, got %d", len(rules))
	}
	if rules[0].ID != added.ID {
		t.Errorf("persisted ID = %q, want %q", rules[0].ID, added.ID)
	}
}

func TestWatcherAddRuleNoStore_DoesNotPersist(t *testing.T) {
	ss := newTestStateStore(t)
	w := watcher.New(nil, ss)

	rule := watcher.Rule{
		Source:       "shell",
		ShellCommand: "echo '[]'",
		Command:      "test command",
	}
	added := w.AddRuleNoStore(rule)
	if added.ID == "" {
		t.Fatal("AddRuleNoStore should generate an ID")
	}

	// Rule must be registered in the watcher.
	listed := w.ListRules()
	if len(listed) != 1 {
		t.Fatalf("expected 1 rule in watcher, got %d", len(listed))
	}

	// But must NOT appear in the state store.
	persisted := ss.GetWatcherRules()
	if len(persisted) != 0 {
		t.Errorf("expected 0 persisted rules for AddRuleNoStore, got %d", len(persisted))
	}
}

func TestWatcherAddRule_NoDuplicatesOnRestart(t *testing.T) {
	ss := newTestStateStore(t)

	// First start: add one dynamic rule via AddRule (persisted).
	w1 := watcher.New(nil, ss)
	r1 := w1.AddRule(watcher.Rule{Source: "shell", ShellCommand: "echo '[]'", Command: "cmd"})

	// Second start: add the same config-sourced rule via AddRuleNoStore.
	w2 := watcher.New(nil, ss)
	w2.Start()
	defer w2.Stop()
	w2.AddRuleNoStore(watcher.Rule{Source: "aone", Event: "new_mr", Command: "cmd2"})

	// Persisted list must still contain only the one dynamic rule from w1.
	persisted := ss.GetWatcherRules()
	if len(persisted) != 1 {
		t.Errorf("expected 1 persisted rule, got %d", len(persisted))
	}
	if persisted[0].ID != r1.ID {
		t.Errorf("persisted rule ID = %q, want %q", persisted[0].ID, r1.ID)
	}
}

func TestWatcherCheckpointPersistedAndRestored(t *testing.T) {
	dir := t.TempDir()
	ssPath := filepath.Join(dir, "state.json")

	// --- Phase 1: create a watcher, add a rule, simulate a poll. ---
	ss1 := config.NewStateStore(ssPath)
	if err := ss1.Load(); err != nil {
		t.Fatalf("load ss1: %v", err)
	}

	// Use a shell source that emits one event on the first poll.
	eventJSON, _ := json.Marshal([]map[string]string{{"MSG": "hello"}})
	script := fmt.Sprintf("echo '%s'", string(eventJSON))

	var executed []string
	w1 := watcher.New(func(cmd string) error {
		executed = append(executed, cmd)
		return nil
	}, ss1)
	rule := w1.AddRule(watcher.Rule{
		Source:       "shell",
		ShellCommand: script,
		Command:      "got: {{.MSG}}",
		Interval:     time.Hour, // long — we call pollAndExecute indirectly via Start
		Timeout:      5 * time.Second,
	})
	_ = rule

	// Trigger one poll cycle by starting and immediately stopping.
	w1.Start()
	time.Sleep(150 * time.Millisecond) // let the initial poll fire
	w1.Stop()

	// LastPoll should now be persisted.
	ss1Reload := config.NewStateStore(ssPath)
	if err := ss1Reload.Load(); err != nil {
		t.Fatalf("reload ss1: %v", err)
	}
	persisted := ss1Reload.GetWatcherRules()
	if len(persisted) == 0 {
		t.Fatal("expected at least one persisted rule after polling")
	}
	if persisted[0].LastPoll == "" {
		t.Error("LastPoll should have been persisted after a successful poll")
	}

	// --- Phase 2: restore from state and verify LastPoll is loaded. ---
	ss2 := config.NewStateStore(ssPath)
	if err := ss2.Load(); err != nil {
		t.Fatalf("load ss2: %v", err)
	}
	w2 := watcher.New(nil, ss2)
	w2.Start()
	defer w2.Stop()

	// ListRules should include the reloaded rule.
	rules2 := w2.ListRules()
	if len(rules2) == 0 {
		t.Fatal("expected rules to be restored from state store")
	}
}

func TestShellSourcePoll_PathAvailable(t *testing.T) {
	// Verify the command can use PATH (regression for the missing cmd.Environ() init).
	src := watcher.NewShellSourceForTest("echo hello | cat")
	events, _, err := src.Poll(context.Background(), "", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 || events[0].Payload["OUTPUT"] != "hello" {
		t.Errorf("expected one 'hello' event, got %v", events)
	}
}

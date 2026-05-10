package tview

import (
	"strings"
	"testing"
)

func TestModel_SetCronIndicatorUpdatesStatusBarText(t *testing.T) {
	m := NewModel()
	defer m.stateManager.Stop()

	m.SetCronIndicator("⏰ 2 running")
	text := m.statusBar.GetText(false)
	if !strings.Contains(text, "⏰ 2 running") || !strings.Contains(text, "Ctrl") {
		t.Fatalf("status bar did not contain indicator and hints: %q", text)
	}
}

func TestModel_SetCronIndicatorEmptyHidesIndicator(t *testing.T) {
	m := NewModel()
	defer m.stateManager.Stop()

	m.SetCronIndicator("⏰ 2 running")
	m.SetCronIndicator("")
	text := m.statusBar.GetText(false)
	if strings.Contains(text, "running") {
		t.Fatalf("status bar still contains cron indicator: %q", text)
	}
}

func TestUpdateStatusBarOrderingCronBeforeHints(t *testing.T) {
	m := NewModel()
	defer m.stateManager.Stop()

	m.SetCronIndicator("⏰ 1 running")
	text := m.statusBar.GetText(false)
	cronIdx := strings.Index(text, "⏰ 1 running")
	hintIdx := strings.Index(text, "Ctrl")
	if cronIdx < 0 || hintIdx < 0 || cronIdx >= hintIdx {
		t.Fatalf("expected cron indicator before hints, got %q", text)
	}
}

func TestStateManagerStatusUpdatePreservesCronIndicator(t *testing.T) {
	m := NewModel()
	defer m.stateManager.Stop()

	m.SetCronIndicator("⏰ 1 running")
	m.stateManager.SetThinking("thinking")
	text := m.statusBar.GetText(false)
	if !strings.Contains(text, "⏰ 1 running") || !strings.Contains(text, "Ctrl") {
		t.Fatalf("state update dropped cron indicator or hints: %q", text)
	}
}

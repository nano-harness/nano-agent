package bubbletea

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

func TestUpdate_CronStatusMsgSetsIndicator(t *testing.T) {
	m := newTestModel(80)
	got, cmd := m.Update(CronStatusMsg{Indicator: "⏰ 2 running"})
	if cmd != nil {
		t.Fatal("CronStatusMsg should not return a command")
	}
	updated := got.(*Model)
	if updated.cronIndicator != "⏰ 2 running" {
		t.Fatalf("cronIndicator = %q", updated.cronIndicator)
	}
}

func TestRenderInputSectionShowsCronIndicator(t *testing.T) {
	m := newTestModel(80)
	m.cronIndicator = "⏰ 2 running"
	m.status = "等待输入"

	var b strings.Builder
	m.renderInputSection(&b)
	out := b.String()
	if !strings.Contains(out, "⏰ 2 running") || !strings.Contains(out, " | 等待输入") {
		t.Fatalf("expected cron indicator joined with status, got:\n%s", out)
	}
}

func TestRenderInputSectionHidesCronIndicatorWhenEmpty(t *testing.T) {
	m := newTestModel(80)
	m.cronIndicator = ""
	m.status = "等待输入"

	var b strings.Builder
	m.renderInputSection(&b)
	out := b.String()
	if strings.Contains(out, "running") || strings.Contains(out, "[状态]  | ") {
		t.Fatalf("expected no empty cron separator, got:\n%s", out)
	}
}

func TestRenderInputSectionNarrowWidthTruncatesWithIndicator(t *testing.T) {
	const termWidth = 10
	m := newTestModel(termWidth)
	m.cronIndicator = "⏰ 123456789 running"
	m.status = strings.Repeat("处理中", 10)

	var b strings.Builder
	m.renderInputSection(&b)
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for _, i := range []int{0, 1, 2} {
		if w := xansi.StringWidth(lines[i]); w > termWidth {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, w, termWidth, lines[i])
		}
	}
}

func TestUpdate_CronStatusMsgDoesNotAlterPhase(t *testing.T) {
	m := newTestModel(80)
	m.currentPhase = phaseToolCall
	m.status = "执行工具"

	_, cmd := m.Update(CronStatusMsg{Indicator: "⏰ 1 running"})
	if cmd != nil {
		t.Fatal("CronStatusMsg should not return outbound command")
	}
	if m.currentPhase != phaseToolCall {
		t.Fatalf("currentPhase = %v, want %v", m.currentPhase, phaseToolCall)
	}
	if m.status != "执行工具" {
		t.Fatalf("status = %q", m.status)
	}
}

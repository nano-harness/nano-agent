package bubbletea_test

import (
	"reflect"
	"strings"
	"testing"

	ui "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestThinkingWindow_AccumulatesDeltas(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	sendThinking(m, "one ", "two")

	if got, want := m.ThinkingWindow(), []string{"one two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ThinkingWindow() = %#v, want %#v", got, want)
	}
}

func TestThinkingWindow_SoftWrapsAndKeepsLastThree(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.ThinkingMsg{Title: "thinking", ReasoningDelta: strings.Repeat("abcdefghij ", 12)})

	got := m.ThinkingWindow()
	if len(got) != 3 {
		t.Fatalf("ThinkingWindow() = %#v, want three lines", got)
	}
	for _, line := range got {
		if width := xansi.StringWidth(line); width > 20 {
			t.Fatalf("expected wrapped line width <= 20, got %d for %q", width, line)
		}
	}
}

func TestThinkingWindow_TrimsWhitespace(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	sendThinking(m, "  first\nline  ")

	if got, want := m.ThinkingWindow(), []string{"first line"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ThinkingWindow() = %#v, want %#v", got, want)
	}
}

func TestThinkingWindow_EmptyLineIgnored(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	sendThinking(m, "kept", "   \n  ")

	if got, want := m.ThinkingWindow(), []string{"kept"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ThinkingWindow() = %#v, want %#v", got, want)
	}
}

func sendThinking(m *ui.Model, lines ...string) {
	for _, line := range lines {
		_, _ = m.Update(ui.ThinkingMsg{Title: "thinking", ReasoningDelta: line})
	}
}

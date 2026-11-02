package bubbletea

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/nano-harness/nano-agent/pkg/slash"
)

// TestFullscreen_CtrlP_OpensCommandPalette verifies that Ctrl+P opens the
// real command palette in milktea mode (not just a notice).
func TestFullscreen_CtrlP_OpensCommandPalette(t *testing.T) {
	m := newReadyFullscreenModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = updated.(*FullscreenModel)

	if !m.showingCommands {
		t.Fatal("expected Ctrl+P to set showingCommands=true")
	}
	if len(m.commands) == 0 {
		t.Fatal("expected commands list to be populated after opening palette")
	}
	if len(m.slashNames) == 0 {
		t.Fatal("expected slashNames cache to be populated for Tab completion")
	}
}

// TestFullscreen_CommandPalette_KeyboardSelection verifies arrow-key
// navigation and Enter-to-insert behavior.
func TestFullscreen_CommandPalette_KeyboardSelection(t *testing.T) {
	m := newReadyFullscreenModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = updated.(*FullscreenModel)

	if len(m.commands) < 2 {
		t.Skipf("registry has fewer than 2 commands; cannot exercise navigation")
	}

	first := m.commands[0].Name
	second := m.commands[1].Name

	// Down once should move from 0 to 1.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(*FullscreenModel)
	if m.commandsIndex != 1 {
		t.Fatalf("expected commandsIndex=1 after Down, got %d", m.commandsIndex)
	}

	// Enter inserts the selected command and closes the palette.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*FullscreenModel)

	if m.showingCommands {
		t.Fatal("expected palette to close after Enter")
	}
	want := "/" + second + " "
	if got := m.textarea.Value(); got != want {
		t.Fatalf("textarea.Value() = %q, want %q (first=%q)", got, want, first)
	}
}

// TestFullscreen_CommandPalette_EscClosesWithoutInserting verifies that
// Esc dismisses the palette without modifying the input.
func TestFullscreen_CommandPalette_EscClosesWithoutInserting(t *testing.T) {
	m := newReadyFullscreenModel()
	m.textarea.SetValue("hello")
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = updated.(*FullscreenModel)
	if !m.showingCommands {
		t.Fatal("palette should be open")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(*FullscreenModel)
	if m.showingCommands {
		t.Fatal("expected Esc to close palette")
	}
	if got := m.textarea.Value(); got != "hello" {
		t.Fatalf("Esc must not modify input, got %q", got)
	}
}

// TestFullscreen_CommandPalette_MouseSelection verifies that left-clicking
// a rendered command row inserts it and closes the palette. View() must be
// invoked first to populate the command hit boxes.
func TestFullscreen_CommandPalette_MouseSelection(t *testing.T) {
	m := newReadyFullscreenModel()
	m.showingCommands = true
	m.commands = []slash.Command{
		{Name: "first", Description: "first cmd", Category: slash.CategoryPermission},
		{Name: "second", Description: "second cmd", Category: slash.CategoryPermission},
	}
	m.slashNames = []string{"first", "second"}

	// Render to populate hit boxes.
	_ = m.View()
	if len(m.commandItems) < 2 {
		t.Fatalf("expected 2 hit boxes, got %d", len(m.commandItems))
	}
	box := m.commandItems[1]

	updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: box.x0, Y: box.y, Button: tea.MouseLeft}))
	m = updated.(*FullscreenModel)

	if m.showingCommands {
		t.Fatal("expected palette to close after mouse selection")
	}
	if got, want := m.textarea.Value(), "/second "; got != want {
		t.Fatalf("textarea.Value() = %q, want %q", got, want)
	}
}

// TestFullscreen_CommandPalette_RendersTitleAndCategories verifies the
// rendered palette includes the title and category headers so users can
// navigate by category.
func TestFullscreen_CommandPalette_RendersTitleAndCategories(t *testing.T) {
	m := newReadyFullscreenModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = updated.(*FullscreenModel)

	out := m.View().Content
	if !strings.Contains(out, "命令列表") {
		t.Fatalf("expected palette title in View(), got:\n%s", out)
	}
	// At least one category header should be visible for the built-in
	// commands shipped with the registry.
	hasCategory := strings.Contains(out, "── 权限 ──") ||
		strings.Contains(out, "── Skills ──") ||
		strings.Contains(out, "── 调度 ──")
	if !hasCategory {
		t.Fatalf("expected at least one category header in palette, got:\n%s", out)
	}
}

// TestFullscreen_TabCompletion_UsesSlashRegistry verifies that Tab on a
// unique slash prefix expands to the full command name once the slash
// registry has been loaded (either implicitly by Tab or explicitly via
// the palette).
func TestFullscreen_TabCompletion_UsesSlashRegistry(t *testing.T) {
	m := newReadyFullscreenModel()
	// Use a fabricated registry to keep the test independent of the
	// real built-in command list.
	m.slashNames = []string{"think", "thinkfast", "yolo"}
	m.textarea.SetValue("/yo")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(*FullscreenModel)

	if got, want := m.textarea.Value(), "/yolo"; got != want {
		t.Fatalf("Tab should complete unique prefix; got %q, want %q", got, want)
	}
}

// TestFullscreen_TabCompletion_AmbiguousLeavesInputAlone verifies that
// Tab on an ambiguous prefix surfaces candidates without mutating the
// input.
func TestFullscreen_TabCompletion_AmbiguousLeavesInputAlone(t *testing.T) {
	m := newReadyFullscreenModel()
	m.slashNames = []string{"think", "thinkfast"}
	m.textarea.SetValue("/thi")
	before := m.textarea.Value()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(*FullscreenModel)

	if got := m.textarea.Value(); got != before {
		t.Fatalf("ambiguous Tab should leave input unchanged; before=%q after=%q", before, got)
	}
	// The system message channel records the candidates so the user can
	// pick one.
	last := m.messages.Last()
	if last == nil || !strings.Contains(last.Content, "命令候选") {
		t.Fatalf("expected '命令候选' system message; got %+v", last)
	}
}

// TestUniquePrefixMatch covers the helper used by handleTabCompletion.
func TestUniquePrefixMatch(t *testing.T) {
	names := []string{"yolo", "yes", "young", "zip"}
	if got := uniquePrefixMatch(names, "yo"); got != "" {
		t.Fatalf("ambiguous 'yo' should not match uniquely, got %q", got)
	}
	if got := uniquePrefixMatch(names, "yol"); got != "yolo" {
		t.Fatalf("expected 'yolo', got %q", got)
	}
	if got := uniquePrefixMatch(names, "z"); got != "zip" {
		t.Fatalf("expected 'zip', got %q", got)
	}
	if got := uniquePrefixMatch(names, ""); got != "" {
		t.Fatalf("empty prefix should never match, got %q", got)
	}
	if got := uniquePrefixMatch(names, "absent"); got != "" {
		t.Fatalf("missing prefix should not match, got %q", got)
	}
}

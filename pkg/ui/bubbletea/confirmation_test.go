package bubbletea_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	ui "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
)

func TestConfirmation_ShowFromShowConfirmationMsg(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.ShowConfirmationMsg{Message: "confirm?", ToolInfo: map[string]interface{}{"Name": "write_file"}})

	showing, selected := m.ConfirmationState()
	if !showing || selected != 0 {
		t.Fatalf("ConfirmationState() = (%v, %d), want (true, 0)", showing, selected)
	}
}

func TestConfirmation_LeftRightSelection(t *testing.T) {
	m := showConfirmation(t)
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	_, selected := m.ConfirmationState()
	if selected != 2 {
		t.Fatalf("selected after right/right = %d, want 2", selected)
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	_, selected = m.ConfirmationState()
	if selected != 1 {
		t.Fatalf("selected after left = %d, want 1", selected)
	}
}

func TestConfirmation_EnterTriggersCallback(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	called := false
	approved := false
	_, _ = m.Update(ui.ShowConfirmationMsg{
		Message:  "confirm?",
		ToolInfo: map[string]interface{}{"Name": "write_file"},
		Callback: func(ok bool) {
			called = true
			approved = ok
		},
	})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}

	if !called || !approved {
		t.Fatalf("callback called=%v approved=%v, want true/true", called, approved)
	}
}

func TestConfirmation_AlwaysInvokesAllowlistHandler(t *testing.T) {
	m := ui.New(nil, nil, "", t.TempDir())
	allowedTool := ""
	m.SetAllowlistHandler(func(toolName string, _ map[string]interface{}) {
		allowedTool = toolName
	})
	_, _ = m.Update(ui.ShowConfirmationMsg{
		Message: "confirm?",
		ToolInfo: map[string]interface{}{
			"Name":       "write_file",
			"Parameters": map[string]interface{}{"path": "a.txt"},
		},
	})
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}

	if allowedTool != "write_file" {
		t.Fatalf("allowlist handler tool = %q, want write_file", allowedTool)
	}
}

func showConfirmation(t *testing.T) *ui.Model {
	t.Helper()
	m := ui.New(nil, nil, "", t.TempDir())
	_, _ = m.Update(ui.ShowConfirmationMsg{Message: "confirm?", ToolInfo: map[string]interface{}{"Name": "write_file"}})
	return m
}

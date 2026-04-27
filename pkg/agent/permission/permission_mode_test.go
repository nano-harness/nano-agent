package permission_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// --- minimal mock tool ---

type mockTool struct {
	name       string
	requiresOK bool
	category   interfaces.ToolCategory
}

func (m *mockTool) Name() string                   { return m.name }
func (m *mockTool) Description() string            { return "" }
func (m *mockTool) Schema() *interfaces.ToolSchema { return nil }
func (m *mockTool) Execute(_ context.Context, _ map[string]interface{}) (*interfaces.ToolResult, error) {
	return nil, nil
}
func (m *mockTool) RequiresConfirmation() bool        { return m.requiresOK }
func (m *mockTool) Category() interfaces.ToolCategory { return m.category }
func (m *mockTool) ConcurrencySafe() bool             { return true }

// contextualTool overrides RequiresConfirmationForParams.
type contextualTool struct {
	mockTool
	paramsRequire bool
}

func (c *contextualTool) RequiresConfirmationForParams(_ map[string]interface{}) bool {
	return c.paramsRequire
}

func TestModeYOLO_NeverConfirms(t *testing.T) {
	mgr := permission.NewManager(permission.ModeYOLO, nil)
	tool := &mockTool{requiresOK: true, category: interfaces.CategoryShell}
	if mgr.ShouldConfirm("run_shell_command", nil, tool) {
		t.Error("YOLO mode should never require confirmation")
	}
}

func TestModeDefault_DelegatesToTool(t *testing.T) {
	mgr := permission.NewManager(permission.ModeDefault, nil)

	safe := &mockTool{requiresOK: false, category: interfaces.CategoryFileSystem}
	if mgr.ShouldConfirm("read_file", nil, safe) {
		t.Error("tool that does not require confirmation should not trigger confirm")
	}

	danger := &mockTool{requiresOK: true, category: interfaces.CategoryShell}
	if !mgr.ShouldConfirm("run_shell_command", nil, danger) {
		t.Error("tool that requires confirmation should trigger confirm in default mode")
	}
}

func TestModeAcceptEdits_AutoApprovesFilesystem(t *testing.T) {
	mgr := permission.NewManager(permission.ModeAcceptEdits, nil)

	editTool := &mockTool{requiresOK: true, category: interfaces.CategoryFileSystem}
	if mgr.ShouldConfirm("write_file", nil, editTool) {
		t.Error("AcceptEdits should auto-approve filesystem tools")
	}

	shellTool := &mockTool{requiresOK: true, category: interfaces.CategoryShell}
	if !mgr.ShouldConfirm("run_shell_command", nil, shellTool) {
		t.Error("AcceptEdits should still confirm shell tools")
	}
}

func TestContextualTool_UsesParamCheck(t *testing.T) {
	mgr := permission.NewManager(permission.ModeDefault, nil)

	// Contextual tool that says "yes" for these params.
	ct := &contextualTool{paramsRequire: true}
	if !mgr.ShouldConfirm("run_shell_command", map[string]interface{}{"command": "rm -rf /"}, ct) {
		t.Error("contextual tool returning true should require confirmation")
	}

	// Contextual tool that says "no" for these params.
	ct2 := &contextualTool{paramsRequire: false}
	if mgr.ShouldConfirm("run_shell_command", map[string]interface{}{"command": "echo hello"}, ct2) {
		t.Error("contextual tool returning false should skip confirmation")
	}
}

func TestSetMode(t *testing.T) {
	mgr := permission.NewManager(permission.ModeDefault, nil)
	mgr.SetMode(permission.ModeYOLO)
	if mgr.GetMode() != permission.ModeYOLO {
		t.Errorf("expected ModeYOLO, got %s", mgr.GetMode())
	}
}

func TestAllowlistOverridesDefault(t *testing.T) {
	mgr := permission.NewManager(permission.ModeDefault, nil)
	mgr.GetSessionAllowlist().AddRule(permission.ParseRule("run_shell_command(git *)"))

	shell := &mockTool{requiresOK: true, category: interfaces.CategoryShell}
	// git status should be allowed.
	if mgr.ShouldConfirm("run_shell_command", map[string]interface{}{"command": "git status"}, shell) {
		t.Error("allowlisted git command should not require confirmation")
	}
	// rm -rf / should still require confirmation.
	if !mgr.ShouldConfirm("run_shell_command", map[string]interface{}{"command": "rm -rf /"}, shell) {
		t.Error("non-allowlisted command should still require confirmation")
	}
}

func TestShouldConfirm_FsToolWithinWorkdir_Skips(t *testing.T) {
	workdir := t.TempDir()
	mgr := permission.NewManagerWithWorkdir(permission.ModeDefault, nil, workdir)
	tool := &contextualTool{
		mockTool:      mockTool{requiresOK: true, category: interfaces.CategoryFileSystem},
		paramsRequire: true,
	}
	params := map[string]interface{}{"file_path": filepath.Join(workdir, "notes.md")}
	if mgr.ShouldConfirm("write_file", params, tool) {
		t.Error("filesystem write inside workdir should not require confirmation")
	}
}

func TestShouldConfirm_FsToolOutsideWorkdir_Confirms(t *testing.T) {
	mgr := permission.NewManagerWithWorkdir(permission.ModeDefault, nil, t.TempDir())
	tool := &contextualTool{
		mockTool:      mockTool{requiresOK: true, category: interfaces.CategoryFileSystem},
		paramsRequire: true,
	}
	if !mgr.ShouldConfirm("write_file", map[string]interface{}{"file_path": "/tmp/x.txt"}, tool) {
		t.Error("filesystem write outside workdir should require confirmation")
	}
}

func TestShouldConfirm_FsToolNoWorkdir_FallbackToContextual(t *testing.T) {
	mgr := permission.NewManager(permission.ModeDefault, nil)
	tool := &contextualTool{
		mockTool:      mockTool{requiresOK: true, category: interfaces.CategoryFileSystem},
		paramsRequire: true,
	}
	if !mgr.ShouldConfirm("write_file", map[string]interface{}{"file_path": "notes.md"}, tool) {
		t.Error("without workdir, manager should use contextual confirmation")
	}
}

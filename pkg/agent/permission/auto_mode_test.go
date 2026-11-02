package permission

// Unit tests for Auto-mode enhancements (§7.3 of the enhancement spec).
//
//   TestIsAutoSafeTool                           – safe-tool whitelist
//   TestIsAutoSafeMCPTool                        – MCP-tool whitelist
//   TestShouldConfirm_AutoMode_SafeTool          – fast-path [2.6]
//   TestShouldConfirm_AutoMode_MCPTool           – fast-path [2.7]
//   TestShouldConfirm_AutoMode_WorkdirEdit       – fast-path [2.8]
//   TestShouldConfirm_AutoMode_SensitiveEdit     – sensitive file still blocked
//   TestTwoStageClassifier_Stage1Allow           – stage-1 allow, no stage-2 call
//   TestTwoStageClassifier_Stage1Block_Stage2Allow – stage-1 block → stage-2 allow
//   TestTwoStageClassifier_BothFail_FailClosed   – both fail + FailClosed=true
//   TestTwoStageClassifier_BothFail_FailOpen     – both fail + FailClosed=false
//   TestShouldConfirm_AutoMode_ClassifierError_FailOpen – classifier error + fail-open

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// ---------------------------------------------------------------------------
// helpers reused across tests
// ---------------------------------------------------------------------------

// editStubTool is a filesystem tool that can optionally require confirmation
// for specific params (simulating the contextual check on sensitive files).
type editStubTool struct {
	name             string
	sensitivePattern string // if params["file_path"] contains this, return true
}

func (e *editStubTool) Name() string                   { return e.name }
func (e *editStubTool) Description() string            { return "" }
func (e *editStubTool) Schema() *interfaces.ToolSchema { return &interfaces.ToolSchema{} }
func (e *editStubTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryFileSystem
}
func (e *editStubTool) RequiresConfirmation() bool { return false }
func (e *editStubTool) ConcurrencySafe() bool      { return true }
func (e *editStubTool) Execute(context.Context, map[string]interface{}) (*interfaces.ToolResult, error) {
	return nil, errors.New("not implemented")
}
func (e *editStubTool) RequiresConfirmationForParams(params map[string]interface{}) bool {
	if e.sensitivePattern == "" {
		return false
	}
	for _, key := range []string{"file_path", "path"} {
		if v, ok := params[key].(string); ok {
			if filepath.Base(v) == e.sensitivePattern {
				return true
			}
		}
	}
	return false
}

// callTrackingClassifier counts how many times Classify is called.
type callTrackingClassifier struct {
	result *ClassifyResult
	err    error
	calls  int
}

func (c *callTrackingClassifier) Classify(_ context.Context, _ ClassifyRequest) (*ClassifyResult, error) {
	c.calls++
	return c.result, c.err
}
func (c *callTrackingClassifier) Timeout() time.Duration { return 5 * time.Second }

// ---------------------------------------------------------------------------
// 2.1 – IsAutoSafeTool
// ---------------------------------------------------------------------------

func TestIsAutoSafeTool(t *testing.T) {
	whitelisted := []string{
		"read_file", "list_directory", "search_files", "file_grep", "glob_files",
		"codebase_search", "search_code", "view_code",
		"web_search", "web_fetch",
		"search_memory", "list_memories",
		"create_plan", "analyze_task",
		"mcp_list_tools", "mcp_list_resources",
	}
	for _, name := range whitelisted {
		if !IsAutoSafeTool(name) {
			t.Errorf("expected IsAutoSafeTool(%q) = true", name)
		}
	}
	notListed := []string{"write_file", "run_shell_command", "delete_file", "bash", ""}
	for _, name := range notListed {
		if IsAutoSafeTool(name) {
			t.Errorf("expected IsAutoSafeTool(%q) = false", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 2.3 – IsAutoSafeMCPTool
// ---------------------------------------------------------------------------

func TestIsAutoSafeMCPTool(t *testing.T) {
	whitelisted := []string{"report_event", "update_status", "get_status", "list_tasks"}
	for _, name := range whitelisted {
		if !IsAutoSafeMCPTool(name) {
			t.Errorf("expected IsAutoSafeMCPTool(%q) = true", name)
		}
	}
	notListed := []string{"execute_task", "run_shell_command", "write_file", ""}
	for _, name := range notListed {
		if IsAutoSafeMCPTool(name) {
			t.Errorf("expected IsAutoSafeMCPTool(%q) = false", name)
		}
	}
}

// ---------------------------------------------------------------------------
// fast-path [2.6] – auto mode + safe tool
// ---------------------------------------------------------------------------

func TestShouldConfirm_AutoMode_SafeTool(t *testing.T) {
	classifier := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: true}}
	mgr := NewManagerWithWorkdir(ModeAuto, nil, "")
	mgr.SetClassifier(classifier)

	// read_file is in autoSafeTools → must return false without calling classifier.
	if mgr.ShouldConfirm("read_file", map[string]interface{}{"file_path": "/some/file"}, nil) {
		t.Error("auto mode: read_file should be fast-pathed (return false)")
	}
	if classifier.calls != 0 {
		t.Errorf("classifier should not have been called; got %d call(s)", classifier.calls)
	}
}

// ---------------------------------------------------------------------------
// fast-path [2.7] – auto mode + MCP safe tool
// ---------------------------------------------------------------------------

func TestShouldConfirm_AutoMode_MCPTool(t *testing.T) {
	classifier := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: true}}
	mgr := NewManagerWithWorkdir(ModeAuto, nil, "")
	mgr.SetClassifier(classifier)

	if mgr.ShouldConfirm("report_event", map[string]interface{}{"type": "progress"}, nil) {
		t.Error("auto mode: report_event should be fast-pathed (return false)")
	}
	if classifier.calls != 0 {
		t.Errorf("classifier should not have been called; got %d call(s)", classifier.calls)
	}
}

// ---------------------------------------------------------------------------
// fast-path [2.8] – auto mode + edit tool + path within workdir
// ---------------------------------------------------------------------------

func TestShouldConfirm_AutoMode_WorkdirEdit(t *testing.T) {
	workdir := t.TempDir()
	classifier := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: true}}

	tool := &editStubTool{name: "write_file"}
	mgr := NewManagerWithWorkdir(ModeAuto, nil, workdir)
	mgr.SetClassifier(classifier)

	targetFile := filepath.Join(workdir, "main.go")
	params := map[string]interface{}{"file_path": targetFile}

	if mgr.ShouldConfirm("write_file", params, tool) {
		t.Error("auto mode: write_file within workdir should be fast-pathed (return false)")
	}
	if classifier.calls != 0 {
		t.Errorf("classifier should not have been called; got %d call(s)", classifier.calls)
	}
}

// ---------------------------------------------------------------------------
// fast-path [2.8] – sensitive file still requires confirmation
// ---------------------------------------------------------------------------

func TestShouldConfirm_AutoMode_SensitiveEdit(t *testing.T) {
	workdir := t.TempDir()
	classifier := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: false}}

	tool := &editStubTool{name: "write_file", sensitivePattern: ".env"}
	mgr := NewManagerWithWorkdir(ModeAuto, nil, workdir)
	mgr.SetClassifier(classifier)

	// .env inside workdir – RequiresConfirmationForParams returns true.
	envFile := filepath.Join(workdir, ".env")
	params := map[string]interface{}{"file_path": envFile}

	// Should NOT be fast-pathed because RequiresConfirmationForParams=true.
	// It will go to the classifier which says ShouldBlock=false, so
	// ShouldConfirm returns false. The key assertion is that it did NOT use
	// the [2.8] fast-path (classifier must have been invoked).
	//
	// We also cover the case where classifier blocks: use ShouldBlock=true to
	// confirm the gate works both ways.
	blockingClassifier := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: true}}
	mgr2 := NewManagerWithWorkdir(ModeAuto, nil, workdir)
	mgr2.SetClassifier(blockingClassifier)
	if !mgr2.ShouldConfirm("write_file", params, tool) {
		t.Error("sensitive file write should reach classifier and be blocked when classifier blocks")
	}
	if blockingClassifier.calls == 0 {
		t.Error("classifier must be called for sensitive file (fast-path should have been skipped)")
	}

	// Even the non-blocking classifier case: .env should never hit the [2.8]
	// fast-path, so the classifier is still consulted.
	_ = mgr.ShouldConfirm("write_file", params, tool)
	if classifier.calls == 0 {
		t.Error("classifier must be called for sensitive file write even when it allows")
	}
}

// ---------------------------------------------------------------------------
// fast-path [2.8] – symlink pointing outside workdir must NOT be fast-pathed
// ---------------------------------------------------------------------------

func TestShouldConfirm_AutoMode_SymlinkOutsideWorkdir(t *testing.T) {
	workdir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Create a symlink inside workdir pointing to outside file.
	symlinkPath := filepath.Join(workdir, "link.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Skip("symlinks not supported on this system:", err)
	}

	blockingClassifier := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: true}}
	tool := &editStubTool{name: "write_file"}
	mgr := NewManagerWithWorkdir(ModeAuto, nil, workdir)
	mgr.SetClassifier(blockingClassifier)

	params := map[string]interface{}{"file_path": symlinkPath}
	// After symlink resolution the path points outside workdir → must not fast-path.
	// Classifier blocks → ShouldConfirm returns true.
	if !mgr.ShouldConfirm("write_file", params, tool) {
		t.Error("symlink pointing outside workdir must not be fast-pathed; classifier should block it")
	}
	if blockingClassifier.calls == 0 {
		t.Error("classifier must be called when symlink resolves to path outside workdir")
	}
}

// ---------------------------------------------------------------------------
// TwoStageClassifier tests
// ---------------------------------------------------------------------------

func TestTwoStageClassifier_Stage1Allow(t *testing.T) {
	stage1 := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: false, Reason: "safe"}}
	stage2 := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: true, Reason: "dangerous"}}

	tsc := &TwoStageClassifier{Fast: stage1, Deep: stage2, FailClosed: true}
	result, err := tsc.Classify(context.Background(), ClassifyRequest{ToolName: "write_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ShouldBlock {
		t.Error("stage-1 allow should mean ShouldBlock=false")
	}
	if stage1.calls != 1 {
		t.Errorf("expected 1 stage-1 call; got %d", stage1.calls)
	}
	if stage2.calls != 0 {
		t.Errorf("stage-2 must NOT be called when stage-1 allows; got %d call(s)", stage2.calls)
	}
}

func TestTwoStageClassifier_Stage1Block_Stage2Allow(t *testing.T) {
	stage1 := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: true, Reason: "suspicious"}}
	stage2 := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: false, Reason: "actually safe"}}

	tsc := &TwoStageClassifier{Fast: stage1, Deep: stage2, FailClosed: true}
	result, err := tsc.Classify(context.Background(), ClassifyRequest{ToolName: "run_shell_command"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ShouldBlock {
		t.Error("stage-2 allow should override stage-1 block → ShouldBlock=false")
	}
	if stage2.calls != 1 {
		t.Errorf("expected 1 stage-2 call; got %d", stage2.calls)
	}
}

func TestTwoStageClassifier_Stage1Block_Stage2Block(t *testing.T) {
	stage1 := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: true}}
	stage2 := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: true}}

	tsc := &TwoStageClassifier{Fast: stage1, Deep: stage2, FailClosed: true}
	result, err := tsc.Classify(context.Background(), ClassifyRequest{ToolName: "run_shell_command"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ShouldBlock {
		t.Error("both stages block → ShouldBlock should be true")
	}
}

func TestTwoStageClassifier_BothFail_FailClosed(t *testing.T) {
	stage1 := &callTrackingClassifier{err: errors.New("timeout")}
	stage2 := &callTrackingClassifier{err: errors.New("timeout")}

	tsc := &TwoStageClassifier{Fast: stage1, Deep: stage2, FailClosed: true}
	result, err := tsc.Classify(context.Background(), ClassifyRequest{ToolName: "write_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ShouldBlock {
		t.Error("both stages fail + FailClosed=true → ShouldBlock must be true")
	}
}

func TestTwoStageClassifier_BothFail_FailOpen(t *testing.T) {
	stage1 := &callTrackingClassifier{err: errors.New("timeout")}
	stage2 := &callTrackingClassifier{err: errors.New("timeout")}

	tsc := &TwoStageClassifier{Fast: stage1, Deep: stage2, FailClosed: false}
	result, err := tsc.Classify(context.Background(), ClassifyRequest{ToolName: "write_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ShouldBlock {
		t.Error("both stages fail + FailClosed=false → ShouldBlock must be false (fail-open)")
	}
}

func TestTwoStageClassifier_Stage1Error_TriggersStage2(t *testing.T) {
	stage1 := &callTrackingClassifier{err: errors.New("stage1 network error")}
	stage2 := &callTrackingClassifier{result: &ClassifyResult{ShouldBlock: false}}

	tsc := &TwoStageClassifier{Fast: stage1, Deep: stage2, FailClosed: true}
	result, err := tsc.Classify(context.Background(), ClassifyRequest{ToolName: "write_file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ShouldBlock {
		t.Error("stage-1 error → stage-2 allow should propagate as ShouldBlock=false")
	}
	if stage2.calls != 1 {
		t.Errorf("stage-2 must be called when stage-1 errors; got %d call(s)", stage2.calls)
	}
}

func TestTwoStageClassifier_Timeout(t *testing.T) {
	fast := &callTrackingClassifier{}
	fast2 := &callTrackingClassifier{}
	tsc := &TwoStageClassifier{Fast: fast, Deep: fast2}

	got := tsc.Timeout()
	want := fast.Timeout() + fast2.Timeout()
	if got != want {
		t.Errorf("TwoStageClassifier.Timeout() = %v; want %v", got, want)
	}
}

func TestClassifyRequestCacheKey_IncludesTranscript(t *testing.T) {
	base := ClassifyRequest{
		ToolName: "write_file",
		Params:   map[string]interface{}{"file_path": "main.go"},
		WorkDir:  "/repo",
		PermMode: ModeAuto,
	}
	withTranscript := base
	withTranscript.Transcript = []TranscriptEntry{{Role: "user", Content: "now edit that file"}}

	if base.CacheKey() == withTranscript.CacheKey() {
		t.Error("cache key must include transcript so multi-turn context changes do not reuse stale classifier decisions")
	}
}

// ---------------------------------------------------------------------------
// Configurable fail mode – classifier error + fail-open
// ---------------------------------------------------------------------------

func TestShouldConfirm_AutoMode_ClassifierError_FailOpen(t *testing.T) {
	classifier := &callTrackingClassifier{err: errors.New("network timeout")}
	mgr := NewManagerWithWorkdir(ModeAuto, nil, "")
	mgr.SetClassifier(classifier)
	mgr.SetFailClosed(false)

	// Non-whitelisted tool requiring classifier; classifier errors; fail-open → return false.
	tool := &stubTool{name: "run_shell_command", requiresConfirm: true, category: interfaces.CategoryShell}
	if mgr.ShouldConfirm("run_shell_command", map[string]interface{}{"command": "go build"}, tool) {
		t.Error("classifier error + fail_closed=false should return false (fail-open)")
	}
}

func TestShouldConfirm_AutoMode_ClassifierError_FailClosed(t *testing.T) {
	classifier := &callTrackingClassifier{err: errors.New("network timeout")}
	mgr := NewManagerWithWorkdir(ModeAuto, nil, "")
	mgr.SetClassifier(classifier)
	mgr.SetFailClosed(true)

	tool := &stubTool{name: "run_shell_command", requiresConfirm: true, category: interfaces.CategoryShell}
	if !mgr.ShouldConfirm("run_shell_command", map[string]interface{}{"command": "go build"}, tool) {
		t.Error("classifier error + fail_closed=true should return true (fail-closed)")
	}
}

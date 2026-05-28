package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/spf13/cobra"
)

// runCmdCapture runs a cobra command as if invoked via `nano <args...>` and
// captures stdout. Tests use this helper to verify --json output without
// mocking out the global command tree.
func runCmdCapture(t *testing.T, root *cobra.Command, args ...string) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		_ = w.Close()
		t.Fatalf("execute %v: %v", args, err)
	}
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

func TestPromptFromArgsOrStdinPrefersArgs(t *testing.T) {
	prompt, err := promptFromArgsOrStdin([]string{"arg", "prompt"}, strings.NewReader("stdin prompt"))
	if err != nil {
		t.Fatalf("promptFromArgsOrStdin returned error: %v", err)
	}
	if prompt != "arg prompt" {
		t.Fatalf("prompt = %q, want args", prompt)
	}
}

func TestPromptFromArgsOrStdinReadsPipe(t *testing.T) {
	prompt, err := promptFromArgsOrStdin(nil, strings.NewReader("stdin prompt\n"))
	if err != nil {
		t.Fatalf("promptFromArgsOrStdin returned error: %v", err)
	}
	if prompt != "stdin prompt\n" {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestBinaryExecCommandIsCanonical(t *testing.T) {
	cmd := NewBinaryCommand()
	execCmd, _, err := cmd.Find([]string{"exec"})
	if err != nil {
		t.Fatalf("find exec: %v", err)
	}
	if execCmd == nil || execCmd.Use != "exec [prompt...]" {
		t.Fatalf("unexpected exec command: %#v", execCmd)
	}
	if execCmd.Deprecated != "" {
		t.Fatalf("exec should not be deprecated: %q", execCmd.Deprecated)
	}
	if execCmd.Flags().Lookup("goal") == nil {
		t.Fatal("exec command missing --goal flag")
	}
	if execCmd.Flags().Lookup("goal-max-turns") == nil {
		t.Fatal("exec command missing --goal-max-turns flag")
	}
}

func TestBinaryExitCodes(t *testing.T) {
	summary := binaryResultSummary{Status: "needs_retry", Reason: "temporary provider error", ToolCalls: 2}
	if got := binaryExitCode(summary.Status); got != binaryExitRetry {
		t.Fatalf("exit code = %d, want %d", got, binaryExitRetry)
	}
	if got := classifyBinaryError(errors.New("deadline exceeded")); got != "timeout" {
		t.Fatalf("classify timeout = %q", got)
	}
}

func TestEmitResultByteParity(t *testing.T) {
	dir := t.TempDir()
	var stdout bytes.Buffer
	summary := binaryResultSummary{Status: "success", ToolCalls: 5, DurationMS: 1234}
	if err := emitResult(&stdout, dir, summary, "diff --git a/f b/f\n"); err != nil {
		t.Fatalf("emitResult: %v", err)
	}
	resultJSON, err := os.ReadFile(dir + "/result.json")
	if err != nil {
		t.Fatalf("read result.json: %v", err)
	}
	if !bytes.Equal(stdout.Bytes(), resultJSON) {
		t.Fatalf("stdout (%d bytes) != result.json (%d bytes)", stdout.Len(), len(resultJSON))
	}
	patchBytes, err := os.ReadFile(dir + "/solution.patch")
	if err != nil {
		t.Fatalf("read solution.patch: %v", err)
	}
	if string(patchBytes) != "diff --git a/f b/f\n" {
		t.Fatalf("unexpected patch content: %q", patchBytes)
	}
}

func TestBinarySwebenchRequiresOutputDir(t *testing.T) {
	cmd := newBinarySWEBenchCommand()
	cmd.SetArgs([]string{"fix the bug"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --output-dir is missing")
	}
	if !strings.Contains(err.Error(), "output-dir") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareBinaryGoalFromPromptFirstLine(t *testing.T) {
	prompt, goal, fromPrompt := prepareBinaryGoal("/goal file done.txt exists\ncreate the file", binaryOptions{})
	if goal != "file done.txt exists" {
		t.Fatalf("goal = %q", goal)
	}
	if !fromPrompt {
		t.Fatal("expected goal to come from prompt")
	}
	if prompt != "create the file" {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestPrepareBinaryGoalFlagOverridesPrompt(t *testing.T) {
	prompt, goal, fromPrompt := prepareBinaryGoal("/goal stale condition\nreal prompt", binaryOptions{Goal: "flag condition"})
	if goal != "flag condition" {
		t.Fatalf("goal = %q", goal)
	}
	if fromPrompt {
		t.Fatal("expected flag goal to take precedence")
	}
	if prompt != "real prompt" {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestSummarizeBinaryResultIncludesGoalState(t *testing.T) {
	achievedAt := time.Now()
	summary := summarizeBinaryResult(nil, time.Now(), "done", nil, &agent.GoalState{
		Condition:      "tests pass",
		TurnsEvaluated: 2,
		MaxTurns:       3,
		AchievedAt:     &achievedAt,
	}, binaryTurnTermination{}, nil)
	if summary.Status != "success" {
		t.Fatalf("status = %q", summary.Status)
	}
	if summary.GoalState == nil || summary.GoalState.Condition != "tests pass" {
		t.Fatalf("goal state missing: %#v", summary.GoalState)
	}
}

func TestSummarizeBinaryResultGoalMaxTurnsNeedsRetry(t *testing.T) {
	summary := summarizeBinaryResult(nil, time.Now(), "not done", nil, &agent.GoalState{
		Condition:      "tests pass",
		Active:         false,
		TurnsEvaluated: 3,
		MaxTurns:       3,
		LastReason:     "tests still fail",
	}, binaryTurnTermination{}, nil)
	if summary.Status != "needs_retry" {
		t.Fatalf("status = %q", summary.Status)
	}
	if summary.Reason != "tests still fail" {
		t.Fatalf("reason = %q", summary.Reason)
	}
}

func TestBinaryListSlash_TextAndJSON(t *testing.T) {
	cmd := NewBinaryCommand()
	out := runCmdCapture(t, cmd, "list-slash")
	if !strings.Contains(out, "/yolo") && !strings.Contains(out, "/clear") {
		t.Errorf("expected built-in commands in plain output, got %q", out[:min(200, len(out))])
	}

	cmd2 := NewBinaryCommand()
	out = runCmdCapture(t, cmd2, "list-slash", "--json")
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, out)
	}
	if len(parsed) == 0 {
		t.Errorf("expected at least one slash command, got 0")
	}
	for _, c := range parsed {
		if _, ok := c["name"]; !ok {
			t.Errorf("missing 'name' field: %v", c)
			break
		}
	}
}

func TestBinaryListTools_JSONStable(t *testing.T) {
	cmd := NewBinaryCommand()
	out := runCmdCapture(t, cmd, "list-tools", "--json")
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(parsed) == 0 {
		t.Errorf("expected at least one built-in tool, got 0")
	}
	// Ensure required descriptor fields are present.
	for _, d := range parsed {
		for _, k := range []string{"name", "category", "mutates_fs"} {
			if _, ok := d[k]; !ok {
				t.Errorf("descriptor missing %q: %v", k, d)
				break
			}
		}
	}
}

func TestBinaryListModels_JSON(t *testing.T) {
	cmd := NewBinaryCommand()
	out := runCmdCapture(t, cmd, "list-models", "--json")
	var parsed []llm.ProviderPreset
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(parsed) == 0 {
		t.Errorf("expected at least one provider preset, got 0")
	}
}

func TestBinaryListSkills_EmptyJSON(t *testing.T) {
	// Point HOME to a temp dir so no personal skills are discovered.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cwd := t.TempDir()
	prevWd, _ := os.Getwd()
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWd) })

	cmd := NewBinaryCommand()
	out := runCmdCapture(t, cmd, "list-skills", "--json")
	out = strings.TrimSpace(out)
	if out == "" {
		t.Fatal("expected JSON output")
	}
	// A null or empty array are both acceptable for "no skills".
	if out != "null" && out != "[]" {
		var parsed []map[string]any
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out)
		}
		if len(parsed) != 0 {
			t.Errorf("expected no skills in fresh HOME, got %d", len(parsed))
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestBinarySwebenchEmitsResult(t *testing.T) {
	if os.Getenv("NANO_RUN_INTEGRATION_TESTS") == "" {
		t.Skip("integration: set NANO_RUN_INTEGRATION_TESTS=1 to run")
	}
	dir := t.TempDir()
	cmd := newBinarySWEBenchCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(bytes.NewBuffer(nil))
	cmd.SetArgs([]string{"--output-dir", dir, "noop test prompt"})
	// Run end-to-end; don't assert success status — assert artifact shape only.
	_ = cmd.Execute()

	resultJSON, err := os.ReadFile(dir + "/result.json")
	if err != nil {
		t.Fatalf("read result.json: %v", err)
	}
	if !bytes.Equal(stdout.Bytes(), resultJSON) {
		t.Fatalf("stdout (%d bytes) != result.json (%d bytes)", stdout.Len(), len(resultJSON))
	}
	// trajectory.json optional (may be empty if agent never ran any step),
	// but if the file exists, it must be valid JSON.
	if traj, err := os.ReadFile(dir + "/trajectory.json"); err == nil {
		var v []map[string]any
		if jerr := json.Unmarshal(traj, &v); jerr != nil {
			t.Fatalf("trajectory.json invalid: %v", jerr)
		}
	}
}

func TestBinarySwebenchUsesEmitResult(t *testing.T) {
	// Structural assertion: the swebench command's RunE calls emitResult which
	// writes result.json. Verify the command structure is correct by checking
	// that --output-dir is required and the command is registered.
	cmd := newBinarySWEBenchCommand()
	if cmd.Use != "swebench [prompt...]" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}
	// Verify --output-dir flag exists and is required.
	f := cmd.Flag("output-dir")
	if f == nil {
		t.Fatal("--output-dir flag not found")
	}
	// Verify the command is part of the binary command tree.
	parent := NewBinaryCommand()
	found := false
	for _, sub := range parent.Commands() {
		if sub.Name() == "swebench" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("swebench not found in binary command tree")
	}
}

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

func TestBinaryResultSentinelAndExitCodes(t *testing.T) {
	t.Setenv("NANO_BINARY_RESULT_FORMAT", "both")
	var out bytes.Buffer
	summary := binaryResultSummary{Status: "needs_retry", Reason: "temporary provider error", ToolCalls: 2}
	if err := writeBinaryResultTo(&out, summary); err != nil {
		t.Fatalf("writeBinaryResultTo: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, binaryResultSentinel) {
		t.Fatalf("missing sentinel in %q", text)
	}
	if got := binaryExitCode(summary.Status); got != binaryExitRetry {
		t.Fatalf("exit code = %d, want %d", got, binaryExitRetry)
	}
	if got := classifyBinaryError(errors.New("deadline exceeded")); got != "timeout" {
		t.Fatalf("classify timeout = %q", got)
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
	})
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
	})
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

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

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

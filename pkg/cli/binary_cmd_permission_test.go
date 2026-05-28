package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestBinaryExecInheritsPermissionFlags verifies that --permission-mode and
// --dangerously-skip-permissions are accessible from the `binary exec` subcommand
// via persistent flag inheritance from the root command.
func TestBinaryExecInheritsPermissionFlags(t *testing.T) {
	root := NewRootCmd()

	binCmd, _, err := root.Find([]string{"binary", "exec"})
	if err != nil {
		t.Fatalf("could not find binary exec command: %v", err)
	}
	if f := binCmd.InheritedFlags().Lookup("permission-mode"); f == nil {
		t.Fatal("binary exec missing inherited --permission-mode flag")
	}
	if f := binCmd.InheritedFlags().Lookup("dangerously-skip-permissions"); f == nil {
		t.Fatal("binary exec missing inherited --dangerously-skip-permissions flag")
	}
}

// TestBinarySwebenchInheritsPermissionFlags verifies that `binary swebench`
// also inherits the permission-mode persistent flags.
func TestBinarySwebenchInheritsPermissionFlags(t *testing.T) {
	root := NewRootCmd()

	binCmd, _, err := root.Find([]string{"binary", "swebench"})
	if err != nil {
		t.Fatalf("could not find binary swebench command: %v", err)
	}
	if f := binCmd.InheritedFlags().Lookup("permission-mode"); f == nil {
		t.Fatal("binary swebench missing inherited --permission-mode flag")
	}
	if f := binCmd.InheritedFlags().Lookup("dangerously-skip-permissions"); f == nil {
		t.Fatal("binary swebench missing inherited --dangerously-skip-permissions flag")
	}
}

// TestBinaryExecPermissionFlagValues verifies that flag values are correctly
// parsed when passed to binary exec.
func TestBinaryExecPermissionFlagValues(t *testing.T) {
	root := NewRootCmd()

	// Override RunE to capture the parsed flag values without actually executing
	var capturedPermMode string
	var capturedSkipPerms bool

	binCmd, _, err := root.Find([]string{"binary", "exec"})
	if err != nil {
		t.Fatalf("could not find binary exec command: %v", err)
	}

	binCmd.RunE = func(cmd *cobra.Command, args []string) error {
		capturedPermMode, _ = cmd.Flags().GetString("permission-mode")
		capturedSkipPerms, _ = cmd.Flags().GetBool("dangerously-skip-permissions")
		return nil
	}

	root.SetArgs([]string{"binary", "exec", "--permission-mode=yolo", "--dangerously-skip-permissions", "--output-dir", t.TempDir(), "test prompt"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedPermMode != "yolo" {
		t.Fatalf("permission-mode = %q, want %q", capturedPermMode, "yolo")
	}
	if !capturedSkipPerms {
		t.Fatal("dangerously-skip-permissions should be true")
	}
}

// TestBinaryExecPermissionFlagAutoValue tests that "auto" value is accepted.
func TestBinaryExecPermissionFlagAutoValue(t *testing.T) {
	root := NewRootCmd()

	var capturedPermMode string

	binCmd, _, _ := root.Find([]string{"binary", "exec"})
	binCmd.RunE = func(cmd *cobra.Command, args []string) error {
		capturedPermMode, _ = cmd.Flags().GetString("permission-mode")
		return nil
	}

	root.SetArgs([]string{"binary", "exec", "--permission-mode=auto", "--output-dir", t.TempDir(), "test"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedPermMode != "auto" {
		t.Fatalf("permission-mode = %q, want %q", capturedPermMode, "auto")
	}
}

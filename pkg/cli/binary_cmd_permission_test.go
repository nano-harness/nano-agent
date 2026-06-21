package cli

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
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

// ── ModeAuto startup validation ────────────────────────────────────────────────

// TestAutoModeValidationEscapeHatches verifies the logic of hasModeAutoEscape
// used by both binary exec paths.
func TestAutoModeValidationEscapeHatches(t *testing.T) {
	cases := []struct {
		name      string
		cfg       config.Config
		wantEsc   bool
	}{
		{
			name:    "no escape – empty config",
			cfg:     config.Config{},
			wantEsc: false,
		},
		{
			name: "escape via PermissionAuto",
			cfg: config.Config{
				PermissionAuto: &config.PermissionAutoConfig{Backend: "llm"},
			},
			wantEsc: true,
		},
		{
			name: "escape via AllowedRules",
			cfg: config.Config{
				AllowedRules: []string{"read_file"},
			},
			wantEsc: true,
		},
		{
			name: "escape via daemon confirm_policy=allow",
			cfg: config.Config{
				Daemon: &config.DaemonConfig{ConfirmPolicy: config.ConfirmPolicyAllow},
			},
			wantEsc: true,
		},
		{
			name: "escape via daemon allowlisted_tools",
			cfg: config.Config{
				Daemon: &config.DaemonConfig{AllowlistedTools: []string{"read_file"}},
			},
			wantEsc: true,
		},
		{
			name: "daemon block policy is not an escape",
			cfg: config.Config{
				Daemon: &config.DaemonConfig{ConfirmPolicy: config.ConfirmPolicyBlock},
			},
			wantEsc: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			got := hasModeAutoEscape(&cfg)
			if got != tc.wantEsc {
				t.Errorf("hasModeAutoEscape = %v, want %v", got, tc.wantEsc)
			}
		})
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

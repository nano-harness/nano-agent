package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompletionCommandGeneratesSupportedShells(t *testing.T) {
	for _, tc := range []struct {
		shell string
		want  string
	}{
		{shell: "bash", want: "bash completion"},
		{shell: "zsh", want: "#compdef"},
		{shell: "fish", want: "complete -c nano"},
		{shell: "powershell", want: "Register-ArgumentCompleter"},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			root := &cobra.Command{Use: "nano"}
			root.AddCommand(NewCompletionCommand(root))
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{"completion", tc.shell})

			if err := root.Execute(); err != nil {
				t.Fatalf("completion %s failed: %v", tc.shell, err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("completion %s output did not contain %q; got %.200q", tc.shell, tc.want, out.String())
			}
		})
	}
}

func TestCompletionCommandRejectsUnsupportedShell(t *testing.T) {
	root := &cobra.Command{Use: "nano"}
	root.AddCommand(NewCompletionCommand(root))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"completion", "xonsh"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected unsupported shell error")
	}
}

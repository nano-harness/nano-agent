package middleware

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCommand_Simple(t *testing.T) {
	pc, err := ParseCommand("echo hello world")
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if len(pc.Statements) != 1 {
		t.Fatalf("len(Statements) = %d, want 1", len(pc.Statements))
	}
	stmt := pc.Statements[0]
	if stmt.Command != "echo" {
		t.Errorf("Command = %q, want echo", stmt.Command)
	}
	if len(stmt.Args) != 2 {
		t.Errorf("Args = %v, want [hello world]", stmt.Args)
	}
}

func TestParseCommand_CommandSubstitution(t *testing.T) {
	pc, err := ParseCommand("echo $(date)")
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if len(pc.Substitutions) == 0 {
		t.Error("expected at least one substitution")
	}
}

func TestParseCommand_NestedShell(t *testing.T) {
	pc, err := ParseCommand(`bash -c "echo inner"`)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if len(pc.Nested) == 0 {
		t.Error("expected nested shell commands")
	}
}

func TestParseCommand_AllStatements(t *testing.T) {
	pc, err := ParseCommand("echo a && echo b")
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	all := pc.AllStatements()
	if len(all) < 2 {
		t.Errorf("AllStatements len = %d, want >= 2", len(all))
	}
}

func TestParseCommand_EnvVar(t *testing.T) {
	pc, err := ParseCommand("IFS=: echo hello")
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if len(pc.Statements) == 0 {
		t.Fatal("expected at least one statement")
	}
	stmt := pc.Statements[0]
	foundIFS := false
	for _, ev := range stmt.Env {
		if ev.Key == "IFS" {
			foundIFS = true
		}
	}
	if !foundIFS {
		t.Error("expected IFS env var in statement")
	}
}

func TestNormalizeArgs_CombinedFlags(t *testing.T) {
	args := NormalizeArgs("rm", []string{"-rf"})
	found := map[string]bool{}
	for _, a := range args {
		found[a] = true
	}
	if !found["-r"] || !found["-f"] {
		t.Errorf("NormalizeArgs: expected -r and -f, got %v", args)
	}
}

func TestNormalizeArgs_LongToShort(t *testing.T) {
	args := NormalizeArgs("rm", []string{"--recursive", "--force"})
	found := map[string]bool{}
	for _, a := range args {
		found[a] = true
	}
	if !found["-r"] || !found["-f"] {
		t.Errorf("NormalizeArgs: expected -r and -f, got %v", args)
	}
}

func TestNormalizeArgs_NonAlpha(t *testing.T) {
	// -123 should not be expanded (digits not alpha)
	args := NormalizeArgs("tail", []string{"-123"})
	if len(args) != 1 || args[0] != "-123" {
		t.Errorf("expected [-123] unchanged, got %v", args)
	}
}

func TestExpandShortFlags_Alpha(t *testing.T) {
	got := expandShortFlags("-rf")
	if len(got) != 2 || got[0] != "-r" || got[1] != "-f" {
		t.Errorf("expandShortFlags(-rf) = %v, want [-r -f]", got)
	}
}

func TestExpandShortFlags_NonAlpha(t *testing.T) {
	got := expandShortFlags("-1n")
	if len(got) != 1 || got[0] != "-1n" {
		t.Errorf("expandShortFlags(-1n) = %v, want [-1n]", got)
	}
}

func TestParseCommand_InvalidSyntax(t *testing.T) {
	// Even on parse error ParseCommand returns a non-nil pc.
	pc, _ := ParseCommand("echo $(")
	if pc == nil {
		t.Error("expected non-nil ParsedCommand even on parse error")
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	got := expandTilde("~")
	if got != home {
		t.Errorf("expandTilde(~) = %q, want %q", got, home)
	}
}

func TestExpandTilde_TrailingSlash(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	// ~/  must expand to the home dir, not to root "/"
	got := expandTilde("~/")
	want := filepath.Join(home, "")
	if got != want {
		t.Errorf("expandTilde(~/) = %q, want %q", got, want)
	}
	if got == "/" {
		t.Error("expandTilde(~/) must not return root /")
	}
}

func TestExpandTilde_WithSubpath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	got := expandTilde("~/Documents")
	want := filepath.Join(home, "Documents")
	if got != want {
		t.Errorf("expandTilde(~/Documents) = %q, want %q", got, want)
	}
}

func TestExpandEnvVars(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	got := expandEnvVars("$HOME")
	if got != home {
		t.Errorf("expandEnvVars($HOME) = %q, want %q", got, home)
	}
}

func TestExpandEnvVars_Braces(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	got := expandEnvVars("${HOME}/foo")
	want := filepath.Join(home, "foo")
	if got != want {
		t.Errorf("expandEnvVars(${HOME}/foo) = %q, want %q", got, want)
	}
}

// Longer names like $HOME_DIR or $HOMELESS must NOT be expanded to the home dir.
func TestExpandEnvVars_NoFalsePositive(t *testing.T) {
	for _, arg := range []string{"$HOMELESS", "$HOME_DIR", "${HOME_DIR}"} {
		got := expandEnvVars(arg)
		if got == arg {
			continue // unchanged — correct
		}
		home, _ := os.UserHomeDir()
		// If it was expanded at all it must not have turned into the home dir itself.
		if home != "" && (got == home || got == home+"/DIR") {
			t.Errorf("expandEnvVars(%q) = %q, must not expand to home dir", arg, got)
		}
	}
}

func TestRebuildCommand(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"echo hello", "echo hello"},
		{"git commit -m msg", "git commit -m msg"},
		{"ls", "ls"},
	}
	for _, c := range cases {
		pc, err := ParseCommand(c.input)
		if err != nil {
			t.Fatalf("ParseCommand(%q): %v", c.input, err)
		}
		if len(pc.Statements) == 0 {
			t.Fatalf("ParseCommand(%q): no statements", c.input)
		}
		got := RebuildCommand(pc.Statements[0])
		if got != c.want {
			t.Errorf("RebuildCommand(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestRebuildCommand_Empty(t *testing.T) {
	got := RebuildCommand(Statement{})
	if got != "" {
		t.Errorf("RebuildCommand(empty) = %q, want empty string", got)
	}
}

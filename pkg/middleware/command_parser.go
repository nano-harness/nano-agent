package middleware

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ParsedCommand holds the result of AST-level shell command parsing.
type ParsedCommand struct {
	Raw           string           // Original command string
	Statements    []Statement      // Top-level statements split by ; && || |
	Substitutions []string         // Commands inside $() or backtick
	Nested        []*ParsedCommand // Commands inside `bash -c "..."`
}

// Statement represents a single shell statement extracted from the AST.
type Statement struct {
	Command    string     // Base command name (e.g. "rm")
	Args       []string   // Normalized argument list
	RawArgs    []string   // Original argument list
	Redirects  []Redirect // I/O redirections
	Env        []EnvVar   // Environment variable assignments before the command
	Background bool       // True if the statement runs in background (&)
}

// Redirect represents a shell I/O redirection.
type Redirect struct {
	Op   string // ">", ">>", "<", etc.
	Word string // Target file or descriptor
}

// EnvVar is a KEY=value assignment preceding a command.
type EnvVar struct {
	Key   string
	Value string
}

// ParseCommand parses a shell command string into a ParsedCommand using the
// mvdan.cc/sh/v3/syntax AST parser. It recursively resolves nested shells
// (bash -c / sh -c) and command substitutions.
func ParseCommand(command string) (*ParsedCommand, error) {
	f, err := syntax.NewParser().Parse(strings.NewReader(command), "cmd")
	if err != nil {
		return &ParsedCommand{Raw: command}, fmt.Errorf("shell parse error: %w", err)
	}

	pc := &ParsedCommand{Raw: command}
	extractStmts(f, pc)
	return pc, nil
}

func extractStmts(f *syntax.File, pc *ParsedCommand) {
	syntax.Walk(f, func(node syntax.Node) bool {
		if node == nil {
			return true
		}
		switch n := node.(type) {
		case *syntax.CallExpr:
			stmt := buildStatement(n)
			pc.Statements = append(pc.Statements, stmt)
			if isShellInterp(stmt.Command) {
				if nested := extractNestedShell(stmt); nested != nil {
					pc.Nested = append(pc.Nested, nested)
				}
			}
		case *syntax.CmdSubst:
			var buf strings.Builder
			_ = syntax.NewPrinter().Print(&buf, n)
			sub := buf.String()
			sub = strings.TrimPrefix(sub, "$(")
			sub = strings.TrimSuffix(sub, ")")
			sub = strings.TrimSpace(sub)
			if sub != "" {
				pc.Substitutions = append(pc.Substitutions, sub)
			}
		}
		return true
	})
}

func buildStatement(n *syntax.CallExpr) Statement {
	stmt := Statement{}
	for _, assign := range n.Assigns {
		ev := EnvVar{Key: assign.Name.Value}
		if assign.Value != nil {
			var buf strings.Builder
			_ = syntax.NewPrinter().Print(&buf, assign.Value)
			ev.Value = buf.String()
		}
		stmt.Env = append(stmt.Env, ev)
	}
	if len(n.Args) == 0 {
		return stmt
	}
	stmt.Command = wordStr(n.Args[0])
	for _, w := range n.Args[1:] {
		arg := wordStr(w)
		stmt.RawArgs = append(stmt.RawArgs, arg)
	}
	stmt.Args = NormalizeArgs(stmt.Command, stmt.RawArgs)
	return stmt
}

func wordStr(w *syntax.Word) string {
	var buf strings.Builder
	_ = syntax.NewPrinter().Print(&buf, w)
	return strings.TrimSpace(buf.String())
}

func isShellInterp(cmd string) bool {
	base := cmdBase(cmd)
	switch strings.ToLower(base) {
	case "bash", "sh", "zsh", "fish", "ksh", "dash":
		return true
	}
	return false
}

func cmdBase(p string) string {
	if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

func extractNestedShell(stmt Statement) *ParsedCommand {
	for i, arg := range stmt.Args {
		if arg == "-c" && i+1 < len(stmt.Args) {
			inner := strings.Trim(stmt.Args[i+1], `"'`)
			if inner == "" {
				return nil
			}
			nested, _ := ParseCommand(inner)
			return nested
		}
	}
	return nil
}

// rmLongToShort maps long rm flags to short equivalents.
var rmLongToShort = map[string]string{
	"--recursive": "-r",
	"--force":     "-f",
	"--verbose":   "-v",
}

// NormalizeArgs normalizes command arguments, expanding combined short flags
// and long-form equivalents into canonical short flags.
func NormalizeArgs(cmd string, args []string) []string {
	var result []string
	for _, arg := range args {
		// Handle combined short flags like -rf → -r -f
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' {
			expanded := expandShortFlags(arg)
			result = append(result, expanded...)
			continue
		}
		// Normalize long-form flags for known destructive commands.
		if cmd == "rm" {
			if short, ok := rmLongToShort[arg]; ok {
				result = append(result, short)
				continue
			}
		}
		result = append(result, arg)
	}
	return result
}

// expandShortFlags splits a combined flag like -rf into ["-r", "-f"].
func expandShortFlags(flag string) []string {
	// Only expand if all chars after '-' are alpha (avoid "-123" or "--foo").
	chars := flag[1:]
	for _, c := range chars {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return []string{flag}
		}
	}
	result := make([]string, len(chars))
	for i, c := range chars {
		result[i] = "-" + string(c)
	}
	return result
}

// RebuildCommand reconstructs a single command string from a Statement.
func RebuildCommand(stmt Statement) string {
	if stmt.Command == "" {
		return ""
	}
	parts := []string{stmt.Command}
	parts = append(parts, stmt.RawArgs...)
	return strings.Join(parts, " ")
}

// AllStatements returns all statements from this command and all nested/substitution commands.
func (pc *ParsedCommand) AllStatements() []Statement {
	var all []Statement
	all = append(all, pc.Statements...)
	for _, nested := range pc.Nested {
		all = append(all, nested.AllStatements()...)
	}
	return all
}

// expandTilde replaces a leading ~ or ~/ with the current user's home directory.
func expandTilde(arg string) string {
	if arg == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return arg
		}
		return home
	}
	if strings.HasPrefix(arg, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return arg
		}
		return filepath.Join(home, strings.TrimPrefix(arg, "~/"))
	}
	return arg
}

// expandEnvVars replaces $HOME and ${HOME} (and $USER/${USER}) with their
// actual values. Uses os.Expand so that only exact variable names match —
// longer names such as $HOME_DIR or $HOMELESS are left intact.
func expandEnvVars(arg string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	user := os.Getenv("USER")
	return os.Expand(arg, func(name string) string {
		switch name {
		case "HOME":
			return home
		case "USER":
			return user
		}
		// Leave all other variables unexpanded. Use brace form to preserve
		// the original syntax regardless of whether the input used braces.
		return "${" + name + "}"
	})
}

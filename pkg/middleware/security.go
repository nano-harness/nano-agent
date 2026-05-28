package middleware

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/nano-harness/nano-agent/pkg/hookservice"
	"github.com/nano-harness/nano-agent/pkg/patternutil"
	"github.com/nano-harness/nano-agent/pkg/policy"
	"github.com/nano-harness/nano-agent/pkg/sandbox"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// ── Decision types (canonical definitions in pkg/policy) ──────────────────────

// Action is an alias for policy.PermissionAction.
type Action = policy.PermissionAction

// Action constants re-exported from policy for backward compatibility.
const (
	ActionAllow   = policy.PermissionAllow
	ActionConfirm = policy.PermissionConfirm
	ActionBlock   = policy.PermissionBlock
)

// Layer constants re-exported from policy.
const (
	LayerConfig   = policy.LayerConfig
	LayerHook     = policy.LayerHook
	LayerAnalyzer = policy.LayerAnalyzer
)

// Decision is an alias for policy.PermissionDecision (unified in P1-1).
type Decision = policy.PermissionDecision

// ── Read-only auto-approval list ─────────────────────────────────────────────

// readOnlyCommands lists commands that are always safe to auto-approve.
// Inspired by Claude Code's readOnlyValidation.ts.
var readOnlyCommands = map[string]bool{
	"ls": true, "ll": true, "la": true,
	"cat": true, "head": true, "tail": true,
	"less": true, "more": true,
	"grep": true, "rg": true, "ag": true, "sed": true,
	"find": true, "locate": true,
	"wc": true, "file": true, "which": true, "type": true,
	"whoami": true, "id": true, "pwd": true,
	"echo": true, "printf": true,
	"date": true, "uptime": true,
	"env": true, "printenv": true,
	"diff": true, "cmp": true,
	"stat": true,
	"tree": true,
	"ps":   true, "top": true, "htop": true,
	"df": true, "du": true, "free": true,
	"uname": true, "hostname": true,
	"git":    true, // sub-commands checked below
	"go":     true, // sub-commands checked below
	"node":   true,
	"python": true, "python3": true,
	"docker": true, // sub-commands checked below
	"make":   true, // sub-commands checked below
	"cargo":  true, // sub-commands checked below
	"pip":    true, // sub-commands checked below
}

// readOnlySubcommands lists safe sub-commands for commands that are not
// universally read-only.
var readOnlySubcommands = map[string]map[string]bool{
	"git": {
		"status": true, "log": true, "diff": true, "show": true,
		"branch": true, "tag": true, "remote": true, "stash": true,
		"describe": true, "rev-parse": true, "shortlog": true,
		"ls-files": true, "ls-tree": true, "cat-file": true,
	},
	"go": {
		"version": true, "env": true, "list": true, "doc": true,
		"vet": true, "build": true, "test": true,
	},
	"docker": {
		"ps": true, "images": true, "inspect": true, "logs": true,
		"version": true, "info": true, "stats": true,
	},
	"node":    {"--version": true, "-v": true},
	"nodejs":  {"--version": true, "-v": true},
	"python":  {"--version": true, "-V": true},
	"python3": {"--version": true, "-V": true},
	"make": {
		"test": true, "check": true, "lint": true,
	},
	"cargo": {
		"test": true, "check": true, "build": true, "clippy": true,
	},
	"pip": {
		"--version": true, "list": true, "show": true, "freeze": true,
	},
}

// ── Context-based decision propagation ───────────────────────────────────────

// securityDecisionKey is the unexported context key for a pre-computed Decision.
type securityDecisionKey struct{}

// WithSecurityDecision stores a pre-computed Decision in ctx.
// Used by tool_scheduler to pass the result of its upfront analysis to
// downstream layers (SecurityMiddleware, ShellTool.Execute) so they can skip
// redundant re-analysis of the same command.
// A nil Decision is ignored (the original context is returned unchanged).
func WithSecurityDecision(ctx context.Context, d *Decision) context.Context {
	if d == nil {
		return ctx
	}
	return context.WithValue(ctx, securityDecisionKey{}, d)
}

// GetSecurityDecision retrieves a pre-computed Decision from ctx, if any.
func GetSecurityDecision(ctx context.Context) (*Decision, bool) {
	d, ok := ctx.Value(securityDecisionKey{}).(*Decision)
	if !ok || d == nil {
		return nil, false
	}
	return d, true
}

// HasSecurityDecision reports whether a non-nil pre-computed Decision is stored in ctx.
func HasSecurityDecision(ctx context.Context) bool {
	d, ok := ctx.Value(securityDecisionKey{}).(*Decision)
	return ok && d != nil
}

func copyDecisionParams(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// MergeDecisionParams returns a shallow copy of base with override entries
// applied. It is used to apply hook-approved parameter rewrites safely without
// mutating the original tool call map.
func MergeDecisionParams(base, override map[string]interface{}) map[string]interface{} {
	if len(override) == 0 {
		return copyDecisionParams(base)
	}
	out := copyDecisionParams(base)
	if out == nil {
		out = make(map[string]interface{}, len(override))
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

func middlewareCommandFromParams(params map[string]interface{}) (string, bool) {
	if len(params) == 0 {
		return "", false
	}
	command, ok := params["command"].(string)
	return command, ok && command != ""
}

// ── Config Rule Engine ────────────────────────────────────────────────────────

// ConfigRule is an allow or deny rule from user configuration.
type ConfigRule struct {
	Pattern string // e.g. "Bash(git status:*)" or "Bash(rm -rf /:exact)"
	Allow   bool   // true = allow, false = deny
}

// ConfigRuleEngine evaluates Layer 1 config rules.
type ConfigRuleEngine struct {
	rules []ConfigRule
}

// NewConfigRuleEngine creates a rule engine from config allow/deny lists.
func NewConfigRuleEngine(allowPatterns, denyPatterns []string) *ConfigRuleEngine {
	e := &ConfigRuleEngine{}
	for _, p := range allowPatterns {
		e.rules = append(e.rules, ConfigRule{Pattern: p, Allow: true})
	}
	for _, p := range denyPatterns {
		e.rules = append(e.rules, ConfigRule{Pattern: p, Allow: false})
	}
	return e
}

// Evaluate returns a decision if any rule matches, or nil if no rule applies.
//
// Evaluation order:
//  1. The full raw command string is checked first via evaluateSingle.  This
//     ensures Format-1 rules such as
//     "run_shell_command(deploy && rollback:exact)" can still match compound
//     expressions.
//  2. For a single-statement command that was not matched in step 1, the
//     statement is rebuilt without any leading env-variable assignments (e.g.
//     "FOO=1 curl …" → "curl …") and re-evaluated.  This prevents an env
//     assignment prefix from silently bypassing a deny rule.
//  3. For compound commands (joined by &&, ||, ;, |), each sub-command is
//     evaluated independently with a deny-any / allow-all strategy:
//     – any sub-command that produces a block decision blocks the whole command;
//     – every sub-command must produce an allow decision for the whole command
//     to be allowed.
//     Note: when a sub-command matches both an allow and a deny rule, the first
//     matching rule wins (allow rules are registered before deny rules in
//     NewConfigRuleEngine).
func (e *ConfigRuleEngine) Evaluate(toolName, command string) *Decision {
	// Step 1: try the full raw command string first so that Format-1 rules
	// (e.g. "run_shell_command(…:exact)") covering compound expressions still fire.
	if d := e.evaluateSingle(toolName, command); d != nil {
		return d
	}

	pc, err := ParseCommand(command)
	if err != nil {
		// Step 1 already tried evaluateSingle on the raw string.
		// A parse failure means there is nothing further we can check.
		return nil
	}

	// Step 2: single-statement – re-evaluate after stripping env assignments.
	if len(pc.Statements) <= 1 {
		if len(pc.Statements) == 1 {
			rebuilt := RebuildCommand(pc.Statements[0])
			if rebuilt != command {
				// Env assignments were stripped; try again with the clean command.
				return e.evaluateSingle(toolName, rebuilt)
			}
		}
		return nil
	}

	// Step 3: compound command – deny-any / allow-all per sub-command.
	allAllowed := true
	var firstAllow *Decision
	for _, stmt := range pc.Statements {
		subCmd := RebuildCommand(stmt)
		if subCmd == "" {
			continue
		}
		d := e.evaluateSingle(toolName, subCmd)
		if d != nil && d.Action == ActionBlock {
			return d
		}
		if d != nil && d.Action == ActionAllow {
			if firstAllow == nil {
				firstAllow = d
			}
		} else {
			allAllowed = false
		}
	}
	if allAllowed && firstAllow != nil {
		return firstAllow
	}
	return nil
}

// evaluateSingle evaluates rules against a single (non-compound) command.
func (e *ConfigRuleEngine) evaluateSingle(toolName, command string) *Decision {
	for _, r := range e.rules {
		if e.matches(r.Pattern, toolName, command) {
			if r.Allow {
				return &Decision{Action: ActionAllow, Reason: "config allow rule: " + r.Pattern, Rule: r.Pattern, Layer: LayerConfig}
			}
			return &Decision{Action: ActionBlock, Reason: "config deny rule: " + r.Pattern, Rule: r.Pattern, Layer: LayerConfig}
		}
	}
	return nil
}

func (e *ConfigRuleEngine) matches(pattern, toolName, command string) bool {
	// Pattern format 1: "ToolName(subpattern)" – match against the full command string.
	if strings.HasPrefix(pattern, toolName+"(") && strings.HasSuffix(pattern, ")") {
		inner := pattern[len(toolName)+1 : len(pattern)-1]
		if strings.HasSuffix(inner, ":exact") {
			exact := strings.TrimSuffix(inner, ":exact")
			return command == exact
		}
		return patternutil.MatchGlob(inner, command)
	}
	// Pattern format 2: plain command name (no parentheses, no "/" in pattern).
	// Treat as a prefix/glob match against the first word of the shell command.
	// This allows allow_rules like ["echo","sleep","curl"] to act as a command-name whitelist.
	if !strings.Contains(pattern, "(") {
		firstWord := command
		if idx := strings.IndexByte(command, ' '); idx >= 0 {
			firstWord = command[:idx]
		}
		firstWord = filepath.Base(firstWord) // strip any path prefix
		return patternutil.MatchGlob(pattern, firstWord)
	}
	return patternutil.MatchGlob(pattern, toolName)
}

// ── Security checkers ─────────────────────────────────────────────────────────

// SecurityChecker is a single security check that can block or require confirmation.
type SecurityChecker interface {
	Name() string
	Check(pc *ParsedCommand) *Decision
}

// commandSubstitutionChecker detects $() and backtick command substitution.
type commandSubstitutionChecker struct{}

func (c *commandSubstitutionChecker) Name() string { return "CommandSubstitution" }
func (c *commandSubstitutionChecker) Check(pc *ParsedCommand) *Decision {
	if len(pc.Substitutions) > 0 {
		return &Decision{
			Action: ActionConfirm,
			Reason: fmt.Sprintf("command substitution detected: %v", pc.Substitutions),
			Rule:   c.Name(),
			Layer:  LayerAnalyzer,
		}
	}
	return nil
}

// pipelineInjectionChecker detects `curl | sh` / `wget | bash` download cradle.
type pipelineInjectionChecker struct{}

func (c *pipelineInjectionChecker) Name() string { return "PipelineInjection" }
func (c *pipelineInjectionChecker) Check(pc *ParsedCommand) *Decision {
	raw := strings.ToLower(pc.Raw)
	patterns := []string{
		"curl | sh", "curl|sh", "curl | bash", "curl|bash",
		"wget | sh", "wget|sh", "wget | bash", "wget|bash",
		"wget -o- | ", "curl -s | ", "curl -sl | ",
	}
	for _, p := range patterns {
		if strings.Contains(raw, p) {
			return &Decision{
				Action: ActionBlock,
				Reason: "download cradle detected: " + p,
				Rule:   c.Name(),
				Layer:  LayerAnalyzer,
			}
		}
	}
	return nil
}

// obfuscationChecker detects base64/hex encoding, eval, source tricks.
type obfuscationChecker struct{}

func (c *obfuscationChecker) Name() string { return "Obfuscation" }
func (c *obfuscationChecker) Check(pc *ParsedCommand) *Decision {
	raw := pc.Raw
	patterns := []string{
		"base64 -d", "base64 --decode",
		"base64 -D", // macOS flag
		"xxd -r",
		"| sh", "| bash", "| zsh",
	}
	for _, stmt := range pc.AllStatements() {
		switch stmt.Command {
		case "eval", "source", ".":
			return &Decision{
				Action: ActionConfirm,
				Reason: "dynamic code evaluation: " + stmt.Command,
				Rule:   c.Name(),
				Layer:  LayerAnalyzer,
			}
		}
	}
	for _, p := range patterns {
		if strings.Contains(raw, p) {
			return &Decision{
				Action: ActionBlock,
				Reason: "obfuscation/shell execution detected: " + p,
				Rule:   c.Name(),
				Layer:  LayerAnalyzer,
			}
		}
	}
	return nil
}

// unicodeWhitespaceChecker detects non-standard whitespace characters.
type unicodeWhitespaceChecker struct{}

func (c *unicodeWhitespaceChecker) Name() string { return "UnicodeWhitespace" }
func (c *unicodeWhitespaceChecker) Check(pc *ParsedCommand) *Decision {
	for _, r := range pc.Raw {
		if unicode.IsSpace(r) && r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return &Decision{
				Action: ActionBlock,
				Reason: fmt.Sprintf("non-standard whitespace character U+%04X detected", r),
				Rule:   c.Name(),
				Layer:  LayerAnalyzer,
			}
		}
	}
	return nil
}

// ifsInjectionChecker detects IFS= manipulation.
type ifsInjectionChecker struct{}

func (c *ifsInjectionChecker) Name() string { return "IFSInjection" }
func (c *ifsInjectionChecker) Check(pc *ParsedCommand) *Decision {
	for _, stmt := range pc.AllStatements() {
		for _, ev := range stmt.Env {
			if ev.Key == "IFS" {
				return &Decision{
					Action: ActionBlock,
					Reason: "IFS environment variable injection detected",
					Rule:   c.Name(),
					Layer:  LayerAnalyzer,
				}
			}
		}
	}
	return nil
}

// controlCharChecker detects carriage returns and other control characters.
type controlCharChecker struct{}

func (c *controlCharChecker) Name() string { return "ControlChar" }
func (c *controlCharChecker) Check(pc *ParsedCommand) *Decision {
	for i, r := range pc.Raw {
		if r == '\r' || (unicode.IsControl(r) && r != '\n' && r != '\t') {
			return &Decision{
				Action: ActionBlock,
				Reason: fmt.Sprintf("control character U+%04X at position %d", r, i),
				Rule:   c.Name(),
				Layer:  LayerAnalyzer,
			}
		}
	}
	return nil
}

// envVarChecker detects dangerous environment variable injections.
type envVarChecker struct{}

func (c *envVarChecker) Name() string { return "EnvVar" }
func (c *envVarChecker) Check(pc *ParsedCommand) *Decision {
	dangerous := []string{"PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES"}
	for _, stmt := range pc.AllStatements() {
		for _, ev := range stmt.Env {
			for _, d := range dangerous {
				if ev.Key == d {
					return &Decision{
						Action: ActionConfirm,
						Reason: "potentially dangerous env var override: " + ev.Key,
						Rule:   c.Name(),
						Layer:  LayerAnalyzer,
					}
				}
			}
		}
	}
	return nil
}

// destructiveCommandChecker detects rm -rf, git reset --hard, DROP TABLE, etc.
type destructiveCommandChecker struct{}

func (c *destructiveCommandChecker) Name() string { return "DestructiveCommand" }
func (c *destructiveCommandChecker) Check(pc *ParsedCommand) *Decision {
	for _, stmt := range pc.AllStatements() {
		switch stmt.Command {
		case "rm", "rmdir":
			hasR := containsAny(stmt.Args, "-r", "-R", "--recursive")
			hasF := containsAny(stmt.Args, "-f", "--force")
			if hasR && hasF {
				return &Decision{
					Action: ActionConfirm,
					Reason: "recursive forced deletion: " + stmt.Command + " " + strings.Join(stmt.Args, " "),
					Rule:   c.Name(),
					Layer:  LayerAnalyzer,
				}
			}
		case "git":
			if len(stmt.Args) > 0 {
				sub := stmt.Args[0]
				hardReset := sub == "reset" && containsAny(stmt.Args[1:], "--hard")
				forceClean := sub == "clean" && containsAny(stmt.Args, "-f", "--force")
				forcePush := sub == "push" && containsAny(stmt.Args, "--force", "-f")
				if hardReset || forceClean || forcePush {
					return &Decision{
						Action: ActionConfirm,
						Reason: "destructive git command: " + strings.Join(append([]string{stmt.Command}, stmt.Args...), " "),
						Rule:   c.Name(),
						Layer:  LayerAnalyzer,
					}
				}
			}
		case "dd":
			return &Decision{
				Action: ActionConfirm,
				Reason: "potentially destructive disk operation: dd",
				Rule:   c.Name(),
				Layer:  LayerAnalyzer,
			}
		case "mkfs", "fdisk", "parted":
			return &Decision{
				Action: ActionBlock,
				Reason: "disk formatting command blocked: " + stmt.Command,
				Rule:   c.Name(),
				Layer:  LayerAnalyzer,
			}
		case "shutdown", "reboot", "halt", "poweroff":
			return &Decision{
				Action: ActionBlock,
				Reason: "system control command blocked: " + stmt.Command,
				Rule:   c.Name(),
				Layer:  LayerAnalyzer,
			}
		}
		// SQL destructive patterns in command arguments
		fullCmd := strings.ToUpper(strings.Join(append([]string{stmt.Command}, stmt.Args...), " "))
		for _, pat := range []string{"DROP TABLE", "DROP DATABASE", "TRUNCATE TABLE", "DELETE FROM"} {
			if strings.Contains(fullCmd, pat) {
				return &Decision{
					Action: ActionConfirm,
					Reason: "destructive SQL statement detected: " + pat,
					Rule:   c.Name(),
					Layer:  LayerAnalyzer,
				}
			}
		}
	}
	return nil
}

// protectedPathChecker detects operations targeting protected directories.
type protectedPathChecker struct{}

func (c *protectedPathChecker) Name() string { return "ProtectedPath" }

var protectedPaths []string

func init() {
	protectedPaths = []string{
		"/", "/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64",
		"/boot", "/sys", "/proc", "/dev",
		".git",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		protectedPaths = append(protectedPaths,
			home,
			filepath.Join(home, ".ssh"),
			filepath.Join(home, ".nano"),
			filepath.Join(home, ".config"),
			filepath.Join(home, ".local"),
		)
	}
}

func (c *protectedPathChecker) Check(pc *ParsedCommand) *Decision {
	for _, stmt := range pc.AllStatements() {
		if !isDestructiveCmd(stmt.Command) {
			continue
		}
		for _, arg := range stmt.Args {
			if arg == "" || arg[0] == '-' {
				continue
			}
			expanded := filepath.Clean(expandEnvVars(expandTilde(arg)))
			// filepath.Clean normalises trailing slashes and dot-dot segments, so
			// "rm /etc/" becomes "/etc" and "rm /etc/../etc/hosts" becomes "/etc/hosts".
			// The explicit pp+"/" check from the original code is therefore covered.
			for _, pp := range protectedPaths {
				if expanded == pp ||
					strings.HasPrefix(expanded, pp+string(filepath.Separator)) {
					return &Decision{
						Action: ActionBlock,
						Reason: fmt.Sprintf("operation on protected path %q is not allowed", arg),
						Rule:   c.Name(),
						Layer:  LayerAnalyzer,
					}
				}
			}
		}
	}
	return nil
}

var defaultSensitiveReadPaths = []string{
	"/etc/shadow",
	"/etc/sudoers",
	"/etc/passwd-",
	".env",
	".env.local",
	".env.production",
	".env.development",
	".nano/nano.yaml",
	".nano.local.yaml",
	"NANO.local.md",
	"~/.ssh",
	"~/.aws",
	"~/.gcp",
	"~/.azure",
	"~/.gnupg",
	"~/.nano",
	"~/.config/nano",
}

// sensitiveReadPathChecker confirms reads of known credential/config paths even
// when the command itself is otherwise read-only.
type sensitiveReadPathChecker struct {
	paths []string
}

func newSensitiveReadPathChecker(extraPaths []string) *sensitiveReadPathChecker {
	paths := append([]string{}, defaultSensitiveReadPaths...)
	paths = append(paths, extraPaths...)
	return &sensitiveReadPathChecker{paths: normalizeSensitivePaths(paths)}
}

func normalizeSensitivePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		normalized := normalizeShellPathArg(p)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func normalizeShellPathArg(arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return ""
	}
	return filepath.Clean(expandEnvVars(expandTilde(arg)))
}

func (c *sensitiveReadPathChecker) Name() string { return "SensitiveReadPath" }

func (c *sensitiveReadPathChecker) Check(pc *ParsedCommand) *Decision {
	for _, stmt := range pc.AllStatements() {
		for _, arg := range stmt.Args {
			if arg == "" {
				continue
			}
			candidate := arg
			if strings.HasPrefix(arg, "-") {
				_, value, ok := strings.Cut(arg, "=")
				if !ok || value == "" {
					continue
				}
				candidate = value
			}
			if c.matches(candidate) {
				return &Decision{
					Action: ActionConfirm,
					Reason: fmt.Sprintf("read of sensitive path %q requires confirmation", candidate),
					Rule:   c.Name(),
					Layer:  LayerAnalyzer,
				}
			}
		}
	}
	return nil
}

func (c *sensitiveReadPathChecker) matches(arg string) bool {
	expanded := normalizeShellPathArg(arg)
	for _, sensitivePath := range c.paths {
		if expanded == sensitivePath ||
			strings.HasPrefix(expanded, sensitivePath+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

var defaultArbitraryExecCommands = []string{
	"python:-c",
	"python3:-c",
	"node:-e",
	"node:--eval",
	"nodejs:-e",
	"bash:-c",
	"sh:-c",
	"zsh:-c",
	"perl:-e",
	"ruby:-e",
}

// arbitraryExecChecker confirms one-liner/eval-style interpreter execution.
type arbitraryExecChecker struct {
	commands map[string]map[string]bool
}

func newArbitraryExecChecker(extraPatterns []string) *arbitraryExecChecker {
	patterns := append([]string{}, defaultArbitraryExecCommands...)
	patterns = append(patterns, extraPatterns...)
	commands := make(map[string]map[string]bool)
	for _, pattern := range patterns {
		cmd, flag, ok := strings.Cut(strings.TrimSpace(pattern), ":")
		if !ok {
			continue
		}
		cmd = strings.TrimSpace(cmdBase(cmd))
		flag = strings.TrimSpace(flag)
		if cmd == "" || flag == "" {
			continue
		}
		if commands[cmd] == nil {
			commands[cmd] = make(map[string]bool)
		}
		commands[cmd][flag] = true
	}
	return &arbitraryExecChecker{commands: commands}
}

func (c *arbitraryExecChecker) Name() string { return "ArbitraryExec" }

func (c *arbitraryExecChecker) Check(pc *ParsedCommand) *Decision {
	for _, stmt := range pc.AllStatements() {
		cmd := cmdBase(stmt.Command)
		flags := c.commands[cmd]
		if len(flags) == 0 {
			continue
		}
		for _, arg := range stmt.Args {
			if flags[arg] {
				return &Decision{
					Action: ActionConfirm,
					Reason: fmt.Sprintf("arbitrary code execution via %s %s requires confirmation", cmd, arg),
					Rule:   c.Name(),
					Layer:  LayerAnalyzer,
				}
			}
		}
	}
	return nil
}

func isDestructiveCmd(cmd string) bool {
	switch cmd {
	case "rm", "rmdir", "chmod", "chown", "dd", "mkfs", "shred":
		return true
	}
	return false
}

// broadDeletionChecker blocks recursive deletion of high-level directories.
type broadDeletionChecker struct{}

func (c *broadDeletionChecker) Name() string { return "BroadDeletion" }
func (c *broadDeletionChecker) Check(pc *ParsedCommand) *Decision {
	for _, stmt := range pc.AllStatements() {
		if stmt.Command != "rm" {
			continue
		}
		hasR := containsAny(stmt.Args, "-r", "-R", "--recursive")
		if !hasR {
			continue
		}
		for _, arg := range stmt.Args {
			if arg == "" || arg[0] == '-' {
				continue
			}
			expanded := filepath.Clean(expandEnvVars(expandTilde(arg)))
			// Only apply the depth check to absolute paths (or paths that were
			// expanded from ~ / $HOME / $USER). Relative paths like "rm -rf build"
			// are handled by destructiveCommandChecker, not here.
			if !filepath.IsAbs(expanded) {
				continue
			}
			parts := strings.Split(strings.Trim(expanded, string(filepath.Separator)), string(filepath.Separator))
			depth := len(parts)
			if depth <= 2 {
				return &Decision{
					Action: ActionBlock,
					Reason: fmt.Sprintf("broad recursive deletion on high-level directory %q is blocked", arg),
					Rule:   c.Name(),
					Layer:  LayerAnalyzer,
				}
			}
		}
	}
	return nil
}

// envVarHomeUserRe matches exact $HOME, ${HOME}, $USER, ${USER} variable
// references, excluding longer names like $HOME_DIR or $HOMELESS.
var envVarHomeUserRe = regexp.MustCompile(`\$\{?(HOME|USER)\}?(?:[^a-zA-Z0-9_]|$)`)

// envVarPathInjectionChecker detects environment variables used as path arguments
// in destructive commands (e.g. rm -rf $HOME/Documents).
type envVarPathInjectionChecker struct{}

func (c *envVarPathInjectionChecker) Name() string { return "EnvVarPathInjection" }
func (c *envVarPathInjectionChecker) Check(pc *ParsedCommand) *Decision {
	for _, stmt := range pc.AllStatements() {
		if !isDestructiveCmd(stmt.Command) {
			continue
		}
		for _, arg := range stmt.RawArgs {
			if envVarHomeUserRe.MatchString(arg) {
				return &Decision{
					Action: ActionConfirm,
					Reason: fmt.Sprintf("environment variable in destructive command argument: %s", arg),
					Rule:   c.Name(),
					Layer:  LayerAnalyzer,
				}
			}
		}
	}
	return nil
}

// sedValidator validates sed commands (LLMs frequently generate risky sed).
type sedValidator struct{}

func (c *sedValidator) Name() string { return "SedValidator" }
func (c *sedValidator) Check(pc *ParsedCommand) *Decision {
	for _, stmt := range pc.AllStatements() {
		if stmt.Command != "sed" {
			continue
		}
		// Detect in-place edit on sensitive paths
		hasI := containsAny(stmt.Args, "-i", "-i.bak")
		if hasI {
			for _, arg := range stmt.Args {
				if arg == "" || arg[0] == '-' {
					continue
				}
				for _, pp := range []string{"/etc", "/usr", "/bin", "/sbin", "/"} {
					if strings.HasPrefix(arg, pp) {
						return &Decision{
							Action: ActionBlock,
							Reason: fmt.Sprintf("sed -i on protected path %q blocked", arg),
							Rule:   c.Name(),
							Layer:  LayerAnalyzer,
						}
					}
				}
			}
			return &Decision{
				Action: ActionConfirm,
				Reason: "sed in-place edit requires confirmation",
				Rule:   c.Name(),
				Layer:  LayerAnalyzer,
			}
		}
	}
	return nil
}

// readOnlyAutoApprover auto-approves known read-only commands.
type readOnlyAutoApprover struct {
	workdir string
}

func (c *readOnlyAutoApprover) Name() string { return "ReadOnlyAutoApprover" }
func (c *readOnlyAutoApprover) Check(pc *ParsedCommand) *Decision {
	if len(pc.Statements) == 0 {
		return nil
	}
	for _, stmt := range pc.AllStatements() {
		if !isReadOnly(stmt) {
			return nil // Not all statements are safe
		}
	}
	if c.workdir != "" {
		for _, stmt := range pc.AllStatements() {
			for _, pathArg := range ExtractShellPathArgs(stmt) {
				if !IsPathWithinWorkdir(c.workdir, pathArg) {
					return &Decision{
						Action:     ActionConfirm,
						Reason:     fmt.Sprintf("read-only command path %q is outside working directory", pathArg),
						Rule:       c.Name(),
						Layer:      LayerAnalyzer,
						Confidence: 0.5,
					}
				}
			}
		}
	}
	return &Decision{
		Action:     ActionAllow,
		Reason:     "all statements are read-only commands",
		Rule:       c.Name(),
		Layer:      LayerAnalyzer,
		Confidence: 0.95,
	}
}

func isReadOnly(stmt Statement) bool {
	cmd := cmdBase(stmt.Command)
	if readOnlyCommands[cmd] {
		// For commands with sub-command restrictions, check the first arg.
		if safeSubs, ok := readOnlySubcommands[cmd]; ok {
			if len(stmt.Args) == 0 {
				return false
			}
			return safeSubs[stmt.Args[0]]
		}
		return true
	}
	return false
}

// safeWriteAutoApprover auto-approves common development commands that modify
// files but are considered safe in a project context (build, test, lint).
// This reduces unnecessary confirmation prompts that break user flow.
type safeWriteAutoApprover struct{}

func (c *safeWriteAutoApprover) Name() string { return "SafeWriteAutoApprover" }
func (c *safeWriteAutoApprover) Check(pc *ParsedCommand) *Decision {
	if len(pc.Statements) == 0 {
		return nil
	}
	for _, stmt := range pc.AllStatements() {
		if !isSafeDevelopmentCommand(stmt) {
			return nil
		}
	}
	return &Decision{
		Action:     ActionAllow,
		Reason:     "all statements are safe development commands",
		Rule:       c.Name(),
		Layer:      LayerAnalyzer,
		Confidence: 0.85,
	}
}

// safeDevelopmentCommands lists commands that modify files but are safe in dev context.
// Commands with empty key ("": true) allow all subcommands - use with caution.
var safeDevelopmentCommands = map[string]map[string]bool{
	"go":    {"test": true, "build": true, "vet": true, "generate": true, "mod": true},
	"npm":   {"install": true, "ci": true, "run": true, "test": true},
	"yarn":  {"install": true, "add": true, "test": true},
	"pip":   {"install": true},
	"cargo": {"build": true, "test": true},
	"make":  {"test": true, "check": true, "lint": true, "build": true}, // Limited to common safe targets
	"git":   {"add": true, "commit": true, "stash": true, "checkout": true, "switch": true, "fetch": true, "pull": true},
	// Note: mkdir, touch, cp, mv removed - too risky without path validation
}

func isSafeDevelopmentCommand(stmt Statement) bool {
	cmd := cmdBase(stmt.Command)
	safeSubs, ok := safeDevelopmentCommands[cmd]
	if !ok {
		return false
	}
	// Empty key means all sub-commands are safe
	if safeSubs[""] {
		return true
	}
	if len(stmt.Args) == 0 {
		return false
	}
	return safeSubs[stmt.Args[0]]
}

// ── SemanticAnalyzer ─────────────────────────────────────────────────────────

// SemanticAnalyzer runs all registered security checkers against a parsed command.
type SemanticAnalyzer struct {
	checkers []SecurityChecker
	workdir  string
}

// DefaultSemanticAnalyzer creates an analyzer with all built-in checkers.
// Checkers run in order; the first non-nil decision wins. Confirmation and
// blocking checks intentionally run before read-only auto-approval.
func DefaultSemanticAnalyzer() *SemanticAnalyzer {
	return DefaultSemanticAnalyzerWithWorkdir("")
}

// DefaultSemanticAnalyzerWithWorkdir creates an analyzer with path-aware
// read-only auto-approval when workdir is non-empty.
func DefaultSemanticAnalyzerWithWorkdir(workdir string) *SemanticAnalyzer {
	return DefaultSemanticAnalyzerWithConfig(workdir, nil, nil)
}

// DefaultSemanticAnalyzerWithConfig creates an analyzer with built-in semantic
// checks plus caller-provided extensions for sensitive reads and arbitrary exec.
func DefaultSemanticAnalyzerWithConfig(workdir string, sensitivePaths, arbitraryExec []string) *SemanticAnalyzer {
	return &SemanticAnalyzer{
		workdir: workdir,
		checkers: []SecurityChecker{
			&unicodeWhitespaceChecker{},                 // Block immediately
			&controlCharChecker{},                       // Block immediately
			&ifsInjectionChecker{},                      // Block
			&pipelineInjectionChecker{},                 // Block
			&obfuscationChecker{},                       // Block / Confirm
			newArbitraryExecChecker(arbitraryExec),      // Confirm
			&protectedPathChecker{},                     // Block
			newSensitiveReadPathChecker(sensitivePaths), // Confirm
			&broadDeletionChecker{},                     // Block: broad recursive deletion
			&envVarChecker{},                            // Confirm
			&envVarPathInjectionChecker{},               // Confirm: env vars in destructive args
			&destructiveCommandChecker{},                // Confirm
			&sedValidator{},                             // Block / Confirm
			&commandSubstitutionChecker{},               // Confirm
			&readOnlyAutoApprover{workdir: workdir},     // Auto-approve read-only commands after confirm checks
			&safeWriteAutoApprover{},                    // Auto-approve safe development commands
		},
	}
}

// Analyze parses the command and runs all checkers, returning the first decisive result.
// Returns ActionConfirm if no checker matches (default for unknown commands).
// Returns ActionBlock if the command cannot be parsed (fail-closed policy).
func (a *SemanticAnalyzer) Analyze(command string) (*Decision, error) {
	pc, err := ParseCommand(command)
	if err != nil {
		return &Decision{
			Action: ActionBlock,
			Reason: "command blocked: shell parse error (fail-closed policy): " + err.Error(),
			Rule:   "FailClosed",
			Layer:  LayerAnalyzer,
		}, nil
	}

	for _, checker := range a.checkers {
		if d := checker.Check(pc); d != nil {
			return d, nil
		}
	}
	// Default: for simple commands (no compound statements, no substitutions),
	// require confirmation with low confidence. Complex/compound commands also
	// require confirmation. Unclassified commands should not be silently allowed
	// since they may include tools like kubectl, curl, nc, or pip install.
	if pc != nil && len(pc.Statements) == 1 && len(pc.Substitutions) == 0 {
		return &Decision{
			Action:     ActionConfirm,
			Reason:     "simple unclassified command requires confirmation",
			Rule:       "SimpleCommandAutoAllow",
			Layer:      LayerAnalyzer,
			Confidence: 0.6,
		}, nil
	}
	return &Decision{
		Action:     ActionConfirm,
		Reason:     "compound/complex command requires confirmation",
		Layer:      LayerAnalyzer,
		Confidence: 0.5,
	}, nil
}

// ── CommandGuard middleware ──────────────────────────────────────────────────

// CommandGuard is the four-layer command security middleware for shell tool.
type CommandGuard struct {
	configRules *ConfigRuleEngine
	hooks       *hookservice.Service
	analyzer    *SemanticAnalyzer
}

// NewCommandGuard creates a CommandGuard with the given config rules and hooks.
func NewCommandGuard(allowRules, denyRules []string, hooks []Hook) *CommandGuard {
	return NewCommandGuardWithWorkdir(allowRules, denyRules, hooks, "")
}

// NewCommandGuardWithWorkdir creates a CommandGuard with path-aware semantic analysis.
func NewCommandGuardWithWorkdir(allowRules, denyRules []string, hooks []Hook, workdir string) *CommandGuard {
	return NewCommandGuardWithHookService(allowRules, denyRules, hookservice.New(hooks), workdir)
}

// NewCommandGuardWithConfig creates a CommandGuard with path-aware semantic
// analysis and caller-provided security checker extensions.
func NewCommandGuardWithConfig(allowRules, denyRules []string, hooks []Hook, workdir string, sensitivePaths, arbitraryExec []string) *CommandGuard {
	return NewCommandGuardWithHookServiceAndConfig(allowRules, denyRules, hookservice.New(hooks), workdir, sensitivePaths, arbitraryExec)
}

// NewCommandGuardWithHookService creates a CommandGuard with an explicit HookService.
func NewCommandGuardWithHookService(allowRules, denyRules []string, hooks *hookservice.Service, workdir string) *CommandGuard {
	return NewCommandGuardWithHookServiceAndConfig(allowRules, denyRules, hooks, workdir, nil, nil)
}

// NewCommandGuardWithHookServiceAndConfig creates a CommandGuard with an
// explicit HookService and semantic checker extensions.
func NewCommandGuardWithHookServiceAndConfig(allowRules, denyRules []string, hooks *hookservice.Service, workdir string, sensitivePaths, arbitraryExec []string) *CommandGuard {
	return &CommandGuard{
		configRules: NewConfigRuleEngine(allowRules, denyRules),
		hooks:       hooks,
		analyzer:    DefaultSemanticAnalyzerWithConfig(workdir, sensitivePaths, arbitraryExec),
	}
}

// Analyze runs the four-layer decision pipeline for a shell command.
// Layer 4 (User Dialog) is handled by the caller based on ActionConfirm.
func (g *CommandGuard) Analyze(ctx context.Context, command string) (*Decision, error) {
	// Layer 1: Config rules (fastest)
	if d := g.configRules.Evaluate("run_shell_command", command); d != nil {
		return d, nil
	}

	// Layer 2: Hooks
	hookDecision, err := g.hooks.Execute(ctx, hookservice.EventPreToolUse, "run_shell_command", map[string]interface{}{"command": command})
	if err != nil {
		return nil, fmt.Errorf("hook engine error: %w", err)
	}
	hookDecision.Layer = LayerHook
	if hookDecision.Action != ActionAllow {
		return hookDecision, nil
	}

	// Layer 3: Semantic analyzer (AST + checkers)
	analyzedCommand := command
	if nextCommand, ok := middlewareCommandFromParams(hookDecision.ModifiedParams); ok {
		analyzedCommand = nextCommand
	}
	decision, err := g.analyzer.Analyze(analyzedCommand)
	if err != nil {
		return nil, err
	}
	if len(hookDecision.ModifiedParams) > 0 && decision.Action != ActionBlock {
		decision.ModifiedParams = copyDecisionParams(hookDecision.ModifiedParams)
		if decision.Rule == "" {
			decision.Rule = hookDecision.Rule
		}
	}
	return decision, nil
}

// ── SecurityMiddleware ────────────────────────────────────────────────────────

// SecurityMiddleware is the unified security middleware that applies CommandGuard,
// PathChecker, and other protection layers to tool execution.
type SecurityMiddleware struct {
	commandGuard *CommandGuard
	pathChecker  *sandbox.PathChecker
	maxFileSize  int64 // Maximum file write size in bytes (0 = unlimited)
}

// NewSecurityMiddleware creates a SecurityMiddleware with the given guards.
func NewSecurityMiddleware(commandGuard *CommandGuard, pathChecker *sandbox.PathChecker, maxFileSizeBytes int64) *SecurityMiddleware {
	return &SecurityMiddleware{
		commandGuard: commandGuard,
		pathChecker:  pathChecker,
		maxFileSize:  maxFileSizeBytes,
	}
}

func (m *SecurityMiddleware) Name() string { return "security" }

func (m *SecurityMiddleware) Execute(
	ctx context.Context,
	tool interfaces.Tool,
	params map[string]interface{},
	next MiddlewareFunc,
) (*interfaces.ToolResult, error) {
	// Shell command security: block hard-blocked commands only.
	if tool.Name() == "run_shell_command" {
		// If a security decision was already computed upstream (by tool_scheduler),
		// skip redundant CommandGuard analysis.
		if !HasSecurityDecision(ctx) {
			if m.commandGuard != nil {
				if cmd, ok := params["command"].(string); ok {
					decision, err := m.commandGuard.Analyze(ctx, cmd)
					if err != nil {
						return nil, fmt.Errorf("security middleware: %w", err)
					}
					if decision.Action == ActionBlock {
						return &interfaces.ToolResult{
							Success:     false,
							Error:       "command blocked by security policy: " + decision.Reason,
							UserContent: "❌ Command blocked: " + decision.Reason,
							LLMContent:  "run_shell_command blocked by security: " + decision.Reason,
						}, nil
					}
				}
			}
		}
	}

	// File path check for filesystem and search tools.
	if paramKey := getPathParam(tool.Name()); paramKey != "" && m.pathChecker != nil {
		if path, ok := params[paramKey].(string); ok && path != "" {
			op := getFileOperation(tool.Name())
			if err := m.pathChecker.Check(path, op); err != nil {
				return &interfaces.ToolResult{
					Success:     false,
					Error:       "path access denied: " + err.Error(),
					UserContent: "❌ Path rejected: " + err.Error(),
					LLMContent:  "path access denied: " + err.Error(),
				}, nil
			}
		}
		// Enforce file size limit for write operations.
		if isWriteOperation(tool.Name()) && m.maxFileSize > 0 {
			if content, ok := params["content"].(string); ok {
				if int64(len(content)) > m.maxFileSize {
					return &interfaces.ToolResult{
						Success:     false,
						Error:       fmt.Sprintf("file content exceeds maximum allowed size (%d bytes)", m.maxFileSize),
						UserContent: "❌ Write rejected: content too large",
						LLMContent:  "write_file rejected: content size exceeds limit",
					}, nil
				}
			}
		}
	}

	return next(ctx, tool, params)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func getPathParam(toolName string) string {
	switch toolName {
	case "write_file", "read_file", "code_skeleton":
		return "file_path"
	case "edit_file", "delete_file":
		return "path"
	default:
		return ""
	}
}

func getFileOperation(toolName string) sandbox.FileOperation {
	switch toolName {
	case "write_file", "edit_file":
		return sandbox.OpWrite
	case "delete_file":
		return sandbox.OpDelete
	default:
		return sandbox.OpRead
	}
}

func isWriteOperation(toolName string) bool {
	return toolName == "write_file" || toolName == "edit_file"
}

func containsAny(slice []string, values ...string) bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	for _, s := range slice {
		if set[s] {
			return true
		}
	}
	return false
}

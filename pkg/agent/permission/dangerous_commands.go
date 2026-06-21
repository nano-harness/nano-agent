package permission

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Severity represents the severity level of a dangerous command.
type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

// DangerousCommandRule defines a pattern-based rule for detecting dangerous commands.
type DangerousCommandRule struct {
	Pattern  *regexp.Regexp
	Reason   string
	Severity Severity
	Category string // e.g., "destructive", "security", "performance"
}

// BuiltinDangerousRules contains the default set of dangerous command patterns.
// These rules are designed to prevent common dangerous operations.
var BuiltinDangerousRules = []DangerousCommandRule{
	// === High Severity: Destructive Commands ===
	{
		Pattern:  regexp.MustCompile(`\brm\s+.*-[a-z]*r[a-z]*f`),
		Reason:   "recursive force deletion (rm -rf)",
		Severity: SeverityHigh,
		Category: "destructive",
	},
	{
		Pattern:  regexp.MustCompile(`\brm\s+.*-[a-z]*f[a-z]*r`),
		Reason:   "recursive force deletion (rm -fr)",
		Severity: SeverityHigh,
		Category: "destructive",
	},
	{
		Pattern:  regexp.MustCompile(`\brm\s+(-rf|-fr)\s+(/\s|/\*|~/?|\$HOME)`),
		Reason:   "deletion of system root or home directory",
		Severity: SeverityHigh,
		Category: "destructive",
	},
	{
		Pattern:  regexp.MustCompile(`:\(\)\s*\{\s*:\|:&\s*\}\s*;:`),
		Reason:   "fork bomb attempt",
		Severity: SeverityHigh,
		Category: "security",
	},
	{
		Pattern:  regexp.MustCompile(`\bdd\s+.*of=/dev/(sd|hd|nvme|vd)`),
		Reason:   "direct disk write operation",
		Severity: SeverityHigh,
		Category: "destructive",
	},
	{
		Pattern:  regexp.MustCompile(`\bmkfs\.\w+`),
		Reason:   "file system formatting",
		Severity: SeverityHigh,
		Category: "destructive",
	},
	{
		Pattern:  regexp.MustCompile(`\bchmod\s+(-R\s+)?777`),
		Reason:   "setting world-writable permissions",
		Severity: SeverityHigh,
		Category: "security",
	},
	{
		Pattern:  regexp.MustCompile(`\bchown\s+.*-R.*\s+/`),
		Reason:   "recursive ownership change on root directory",
		Severity: SeverityHigh,
		Category: "security",
	},

	// === Medium Severity: Git and VCS Operations ===
	{
		Pattern:  regexp.MustCompile(`\bgit\s+push\s+.*--force`),
		Reason:   "force push to remote repository",
		Severity: SeverityMedium,
		Category: "vcs",
	},
	{
		Pattern:  regexp.MustCompile(`\bgit\s+push\s+.*\b(main|master)\b`),
		Reason:   "push to main/master branch",
		Severity: SeverityMedium,
		Category: "vcs",
	},
	{
		Pattern:  regexp.MustCompile(`\bgit\s+reset\s+--hard\s+(origin/|HEAD~)`),
		Reason:   "hard reset to remote or previous commit",
		Severity: SeverityMedium,
		Category: "vcs",
	},
	{
		Pattern:  regexp.MustCompile(`\bgit\s+clean\s+.*-[a-z]*f[a-z]*d`),
		Reason:   "force clean untracked files and directories",
		Severity: SeverityMedium,
		Category: "vcs",
	},

	// === Medium Severity: System Modifications ===
	{
		Pattern:  regexp.MustCompile(`\bsudo\s+(rm|mv|cp|dd|mkfs|chmod|chown)`),
		Reason:   "privileged file system operation",
		Severity: SeverityMedium,
		Category: "system",
	},
	{
		Pattern:  regexp.MustCompile(`>\s*/dev/(sd|hd|nvme|vd)`),
		Reason:   "output redirect to block device",
		Severity: SeverityMedium,
		Category: "destructive",
	},

	// === Low Severity: Package Management ===
	{
		Pattern:  regexp.MustCompile(`\b(apt|yum|dnf|pacman)\s+(-y\s+)?(remove|purge|erase)`),
		Reason:   "package removal",
		Severity: SeverityLow,
		Category: "package",
	},
	{
		Pattern:  regexp.MustCompile(`\bnpm\s+uninstall\s+-g`),
		Reason:   "global package removal",
		Severity: SeverityLow,
		Category: "package",
	},

	// === High Severity: System Shutdown/Restart ===
	{
		Pattern:  regexp.MustCompile(`\b(reboot|halt|poweroff)\b`),
		Reason:   "system shutdown or restart",
		Severity: SeverityHigh,
		Category: "system",
	},
	{
		Pattern:  regexp.MustCompile(`\bshutdown\b`),
		Reason:   "system shutdown",
		Severity: SeverityHigh,
		Category: "system",
	},
	{
		Pattern:  regexp.MustCompile(`\binit\s+(0|6)\b`),
		Reason:   "system runlevel change (shutdown/reboot)",
		Severity: SeverityHigh,
		Category: "system",
	},

	// === High Severity: Broadcast Process Termination ===
	{
		Pattern:  regexp.MustCompile(`\bkill\s+.*-9\s+-1\b`),
		Reason:   "kill all processes (kill -9 -1)",
		Severity: SeverityHigh,
		Category: "destructive",
	},

	// === Medium Severity: Mass Process Termination ===
	{
		Pattern:  regexp.MustCompile(`\bkillall\b`),
		Reason:   "kill all matching processes",
		Severity: SeverityMedium,
		Category: "destructive",
	},

	// === High Severity: Pipe Download to Shell ===
	{
		Pattern:  regexp.MustCompile(`\b(curl|wget)\b[^|]*\|\s*(sh|bash|zsh|fish)\b`),
		Reason:   "piping downloaded content directly to shell",
		Severity: SeverityHigh,
		Category: "security",
	},

	// === High Severity: Remove All Permissions ===
	{
		Pattern:  regexp.MustCompile(`\bchmod\s+(-R\s+)?000\b`),
		Reason:   "removing all permissions (chmod 000)",
		Severity: SeverityHigh,
		Category: "destructive",
	},

	// === Medium Severity: find write-action flags ===
	// -delete, -fprint/-fprintf/-fls write to the filesystem; -ok/-okdir execute
	// commands interactively; all are write-class operations that must not bypass
	// the auto-approve fast-path.
	{
		Pattern:  regexp.MustCompile(`\bfind\b.*\s(-delete|-fprintf|-fprint|-fls|-ok|-okdir)\b`),
		Reason:   "find with write-action flag (-delete/-fprintf/-fprint/-fls/-ok/-okdir)",
		Severity: SeverityMedium,
		Category: "destructive",
	},
}

// SensitiveFilePatterns contains glob patterns for sensitive files that should
// not be modified or committed.
var SensitiveFilePatterns = []string{
	"*.env",
	".env*",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"*credentials*",
	"*secrets*",
	"*password*",
	".kube/config",
	".aws/credentials",
	".ssh/*",
	".gnupg/*",
	"id_rsa*",
	"id_ed25519*",
	"*.crt",
	"*.cer",
}

// CheckCommand checks if a command matches any dangerous patterns.
// Returns the matched rule and true if dangerous, nil and false otherwise.
func CheckCommand(command string) (*DangerousCommandRule, bool) {
	// Normalize command for checking
	cmd := strings.TrimSpace(command)

	for i := range BuiltinDangerousRules {
		rule := &BuiltinDangerousRules[i]
		if rule.Pattern.MatchString(cmd) {
			return rule, true
		}
	}

	return nil, false
}

// IsSensitiveFile checks if a file path matches any sensitive file patterns.
// It uses filepath.Match on the basename for glob patterns and path-segment
// matching for patterns that contain a path separator.
func IsSensitiveFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	lowerPath := strings.ToLower(filepath.ToSlash(path))

	for _, pattern := range SensitiveFilePatterns {
		p := strings.ToLower(pattern)
		// Patterns with a path separator: match progressively shorter path suffixes
		// so that e.g. ".ssh/*" matches "/home/user/.ssh/config".
		if strings.ContainsRune(p, '/') {
			remaining := strings.TrimPrefix(lowerPath, "/")
			for remaining != "" {
				matched, err := filepath.Match(p, remaining)
				if err == nil && matched {
					return true
				}
				idx := strings.IndexByte(remaining, '/')
				if idx == -1 {
					break
				}
				remaining = remaining[idx+1:]
			}
			continue
		}
		// Basename glob matching.
		matched, err := filepath.Match(p, base)
		if err == nil && matched {
			return true
		}
	}
	return false
}

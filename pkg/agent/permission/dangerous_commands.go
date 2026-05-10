package permission

import (
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
func IsSensitiveFile(path string) bool {
	// Convert path to lowercase for case-insensitive matching
	lowerPath := strings.ToLower(path)
	baseName := lowerPath
	if idx := strings.LastIndex(lowerPath, "/"); idx >= 0 {
		baseName = lowerPath[idx+1:]
	}

	for _, pattern := range SensitiveFilePatterns {
		// Remove leading wildcard for substring matching
		cleanPattern := strings.TrimPrefix(strings.ToLower(pattern), "*")
		cleanPattern = strings.TrimSuffix(cleanPattern, "*")

		// Check if the pattern matches anywhere in the path or basename
		if strings.Contains(lowerPath, cleanPattern) || strings.Contains(baseName, cleanPattern) {
			return true
		}

		// Special check for exact extensions
		if strings.HasPrefix(pattern, "*.") {
			ext := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(lowerPath, strings.ToLower(ext)) {
				return true
			}
		}
	}

	return false
}

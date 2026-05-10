package permission

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/hookservice"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// FirewallConfig contains configuration for the dangerous command firewall.
type FirewallConfig struct {
	Enabled           bool
	SeverityThreshold Severity // Only block commands at or above this severity
	FailurePolicy     string   // "confirm", "block", or "allow"
	CustomPatterns    []DangerousCommandRule
	Overrides         []string // Command patterns to whitelist/override
}

// DefaultFirewallConfig returns the default firewall configuration.
func DefaultFirewallConfig() FirewallConfig {
	return FirewallConfig{
		Enabled:           true,
		SeverityThreshold: SeverityMedium,
		FailurePolicy:     "confirm", // Default to confirm rather than block
		CustomPatterns:    nil,
		Overrides:         nil,
	}
}

// FirewallHook implements a built-in hook that checks for dangerous commands.
type FirewallHook struct {
	config FirewallConfig
	rules  []DangerousCommandRule
}

// NewFirewallHook creates a new firewall hook with the given configuration.
func NewFirewallHook(config FirewallConfig) *FirewallHook {
	// Combine built-in rules with custom patterns
	rules := make([]DangerousCommandRule, 0, len(BuiltinDangerousRules)+len(config.CustomPatterns))
	rules = append(rules, BuiltinDangerousRules...)
	rules = append(rules, config.CustomPatterns...)

	return &FirewallHook{
		config: config,
		rules:  rules,
	}
}

// Name returns the built-in hook name.
func (h *FirewallHook) Name() string {
	return "builtin_firewall"
}

// Event returns the hook event handled by the firewall.
func (h *FirewallHook) Event() hookservice.Event {
	return hookservice.EventPreToolUse
}

// Execute implements the hook execution logic for dangerous command detection.
func (h *FirewallHook) Execute(ctx context.Context, event hookservice.Event, toolName string, params map[string]interface{}) (*hookservice.Decision, error) {
	// Only process shell command tools
	if toolName != "run_shell_command" && toolName != "bash" {
		return &hookservice.Decision{
			Action: hookservice.ActionAllow,
			Reason: "not a shell command",
		}, nil
	}

	// Extract command from parameters
	command, ok := params["command"].(string)
	if !ok || command == "" {
		return &hookservice.Decision{
			Action: hookservice.ActionAllow,
			Reason: "no command found in parameters",
		}, nil
	}

	// Check if command is in override list
	for _, override := range h.config.Overrides {
		if override == command {
			logger.Infof("Command allowed by firewall override: %s", command)
			return &hookservice.Decision{
				Action: hookservice.ActionAllow,
				Reason: "command in override whitelist",
			}, nil
		}
	}

	// Check command against dangerous patterns (both built-in and custom)
	rule, isDangerous := h.checkCommandAgainstRules(command)
	if !isDangerous {
		return &hookservice.Decision{
			Action: hookservice.ActionAllow,
			Reason: "command passed firewall checks",
		}, nil
	}

	// Check if severity meets threshold
	if !h.meetsSeverityThreshold(rule.Severity) {
		return &hookservice.Decision{
			Action: hookservice.ActionAllow,
			Reason: fmt.Sprintf("severity %s below threshold %s", rule.Severity, h.config.SeverityThreshold),
		}, nil
	}

	// Command is dangerous - apply failure policy
	logger.Warnf("Dangerous command detected: %s (reason: %s, severity: %s)", command, rule.Reason, rule.Severity)

	var action hookservice.Action
	switch h.config.FailurePolicy {
	case "block":
		action = hookservice.ActionBlock
	case "allow":
		action = hookservice.ActionAllow
	case "confirm":
		fallthrough
	default:
		action = hookservice.ActionConfirm
	}

	warnings := []string{
		fmt.Sprintf("⚠️  Dangerous command detected"),
		fmt.Sprintf("Command: %s", command),
		fmt.Sprintf("Reason: %s", rule.Reason),
		fmt.Sprintf("Severity: %s", rule.Severity),
		fmt.Sprintf("Category: %s", rule.Category),
	}

	return &hookservice.Decision{
		Action:   action,
		Reason:   fmt.Sprintf("dangerous command: %s", rule.Reason),
		Rule:     fmt.Sprintf("firewall/%s", rule.Category),
		Warnings: warnings,
	}, nil
}

// meetsSeverityThreshold checks if a severity level meets the configured threshold.
func (h *FirewallHook) meetsSeverityThreshold(severity Severity) bool {
	severityLevels := map[Severity]int{
		SeverityLow:    1,
		SeverityMedium: 2,
		SeverityHigh:   3,
	}

	cmdLevel := severityLevels[severity]
	thresholdLevel := severityLevels[h.config.SeverityThreshold]

	return cmdLevel >= thresholdLevel
}

// checkCommandAgainstRules checks a command against the firewall's rules (built-in + custom).
func (h *FirewallHook) checkCommandAgainstRules(command string) (*DangerousCommandRule, bool) {
	// Normalize command for checking
	cmd := strings.TrimSpace(command)

	for i := range h.rules {
		rule := &h.rules[i]
		if rule.Pattern.MatchString(cmd) {
			return rule, true
		}
	}

	return nil, false
}

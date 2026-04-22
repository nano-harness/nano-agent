package agent

import (
	"regexp"
	"strings"
)

// ExpertTrigger represents a parsed expert invocation from user input
type ExpertTrigger struct {
	ExpertName string
	RawInput   string // The rest of the message after @expert-name
	Inputs     map[string]interface{}
}

var (
	// expertTriggerRegex matches @expert-name at start of message or after whitespace
	// Must be strict kebab-case: lowercase letters, digits, hyphens
	// Examples that match: "@investigator", "@help", "@my-expert", "@angular/core" (will be filtered later)
	// Examples that DON'T match: "@HEAD" (uppercase), "user@example.com" (no whitespace before @)
	expertTriggerRegex = regexp.MustCompile(`(?:^|\s)@([a-z][a-z0-9-]*)`)
)

// ParseExpertTrigger checks if the user message contains an expert trigger
// Returns the trigger info if found, nil otherwise
func ParseExpertTrigger(message string, registry *ExpertRegistry) *ExpertTrigger {
	// Check for @expert-name pattern
	matches := expertTriggerRegex.FindStringSubmatch(message)
	if len(matches) < 2 {
		return nil
	}

	expertName := matches[1]

	// Verify expert exists in registry
	expert, exists := registry.Get(expertName)
	if !exists {
		return nil
	}

	// Extract the input text (everything after @expert-name)
	triggerPattern := "@" + expertName
	idx := strings.Index(message, triggerPattern)
	if idx == -1 {
		return nil
	}

	rawInput := strings.TrimSpace(message[idx+len(triggerPattern):])

	// Determine input field name from expert's InputSchema
	inputFieldName := "request" // default
	if expert.InputSchema != nil && len(expert.InputSchema.Required) > 0 {
		inputFieldName = expert.InputSchema.Required[0]
	}

	return &ExpertTrigger{
		ExpertName: expertName,
		RawInput:   rawInput,
		Inputs: map[string]interface{}{
			inputFieldName: rawInput,
		},
	}
}

// HasExpertTrigger checks if a message contains any expert trigger pattern
// This is a quick check that doesn't verify registry membership
func HasExpertTrigger(message string) bool {
	return expertTriggerRegex.MatchString(message)
}

// IsExpertTriggerFalsePositive checks for common false positive patterns
// Returns true if the @ pattern looks like it should NOT trigger an expert
func IsExpertTriggerFalsePositive(message string) bool {
	// Email pattern: user@example.com
	emailPattern := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	if emailPattern.MatchString(message) {
		return true
	}

	// Git ref pattern: @HEAD, @{upstream}
	gitRefPattern := regexp.MustCompile(`@[A-Z][A-Z_]*[A-Z]|\@\{[^}]+\}`)
	if gitRefPattern.MatchString(message) {
		return true
	}

	// NPM scope pattern: @scope/package
	npmScopePattern := regexp.MustCompile(`@[a-z0-9-]+/[a-z0-9-]+`)
	if npmScopePattern.MatchString(message) {
		return true
	}

	return false
}

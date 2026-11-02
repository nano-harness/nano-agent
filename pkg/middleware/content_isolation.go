package middleware

import (
	"fmt"
	"regexp"
	"strings"
)

// InjectionIndicator represents a detected injection pattern in content
type InjectionIndicator struct {
	Pattern     string // The matched pattern
	Position    int    // Position in the content where the pattern was found
	Severity    string // "low" / "medium" / "high"
	Description string // Human-readable description of the threat
}

// Common prompt injection patterns to detect
var injectionPatterns = []struct {
	pattern     *regexp.Regexp
	severity    string
	description string
}{
	{
		// Match variations like "ignore previous", "ignore all previous", "ignore all prior"
		pattern:     regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+(instructions?|commands?|prompts?)`),
		severity:    "high",
		description: "Attempt to override previous instructions",
	},
	{
		// Match role change only when directed at "you" (the AI), not users/others
		pattern:     regexp.MustCompile(`(?i)(you\s+are\s+now|you\s+should\s+act\s+as|pretend\s+to\s+be|roleplay\s+as)\s+(a\s+)?(different|new|admin|hacker|expert|root|superuser)`),
		severity:    "high",
		description: "Attempt to change AI role or behavior",
	},
	{
		pattern:     regexp.MustCompile(`(?i)(system:|<system>|\[SYSTEM\])`),
		severity:    "high",
		description: "Attempt to inject system-level commands",
	},
	{
		pattern:     regexp.MustCompile(`(?i)IMPORTANT:\s*[^\n]{0,50}(ignore|override|execute|run|do\s+not)`),
		severity:    "medium",
		description: "Directive with instruction keywords",
	},
	{
		pattern:     regexp.MustCompile(`(?i)(do\s+not\s+follow|override|bypass)\s+(security|rules|instructions|checks)`),
		severity:    "high",
		description: "Attempt to bypass security measures",
	},
	{
		pattern:     regexp.MustCompile(`(?i)<\s*instructions?\s*>`),
		severity:    "medium",
		description: "Instruction tag injection",
	},
	{
		pattern:     regexp.MustCompile(`(?i)\[INST\]|\[/INST\]`),
		severity:    "medium",
		description: "LLaMA instruction tag injection",
	},
	{
		pattern:     regexp.MustCompile(`(?i)###\s*(System|User|Assistant)\s*:`),
		severity:    "medium",
		description: "ChatML-style role injection",
	},
}

// WrapExternalContent wraps external content with isolation tags
func WrapExternalContent(content, source, contentType string) string {
	indicators := DetectInjectionPatterns(content)

	var builder strings.Builder
	fmt.Fprintf(&builder, "<external_data source=%q type=%q>\n", source, contentType)

	if len(indicators) > 0 {
		builder.WriteString("[INJECTION_WARNING] This content contains potential prompt injection patterns:\n")
		for i, indicator := range indicators {
			if i < 3 { // Only show first 3 warnings to avoid overwhelming output
				fmt.Fprintf(&builder, "  - %s (severity: %s) at position %d\n",
					indicator.Description, indicator.Severity, indicator.Position)
			}
		}
		if len(indicators) > 3 {
			fmt.Fprintf(&builder, "  ... and %d more potential injection patterns\n", len(indicators)-3)
		}
		builder.WriteString("\n")
	}

	builder.WriteString(content)
	builder.WriteString("\n</external_data>")

	return builder.String()
}

// DetectInjectionPatterns analyzes content for potential injection patterns
func DetectInjectionPatterns(content string) []InjectionIndicator {
	var indicators []InjectionIndicator

	for _, pattern := range injectionPatterns {
		matches := pattern.pattern.FindAllStringIndex(content, -1)
		for _, match := range matches {
			indicators = append(indicators, InjectionIndicator{
				Pattern:     pattern.pattern.String(),
				Position:    match[0],
				Severity:    pattern.severity,
				Description: pattern.description,
			})
		}
	}

	return indicators
}

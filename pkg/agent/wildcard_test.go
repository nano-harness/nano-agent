package agent

import (
	"strings"
	"testing"
)

func containsWildcard(pattern string) bool {
	return strings.Contains(pattern, "*") || strings.Contains(pattern, "?")
}

func TestContainsWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		expected bool
	}{
		{
			name:     "no wildcard",
			pattern:  "web_search",
			expected: false,
		},
		{
			name:     "asterisk wildcard",
			pattern:  "mcp_*",
			expected: true,
		},
		{
			name:     "question mark wildcard",
			pattern:  "file_?",
			expected: true,
		},
		{
			name:     "both wildcards",
			pattern:  "mcp_*_?",
			expected: true,
		},
		{
			name:     "empty string",
			pattern:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsWildcard(tt.pattern)
			if result != tt.expected {
				t.Errorf("containsWildcard(%q) = %v, want %v", tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestMatchesWildcard(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		pattern  string
		expected bool
	}{
		{
			name:     "exact match",
			toolName: "web_search",
			pattern:  "web_search",
			expected: true,
		},
		{
			name:     "asterisk match prefix",
			toolName: "mcp_filesystem_read",
			pattern:  "mcp_*",
			expected: true,
		},
		{
			name:     "asterisk match suffix",
			toolName: "web_search_tool",
			pattern:  "*_tool",
			expected: true,
		},
		{
			name:     "asterisk match middle",
			toolName: "mcp_git_status",
			pattern:  "mcp_*_status",
			expected: true,
		},
		{
			name:     "question mark match",
			toolName: "file_a",
			pattern:  "file_?",
			expected: true,
		},
		{
			name:     "question mark no match",
			toolName: "file_ab",
			pattern:  "file_?",
			expected: false,
		},
		{
			name:     "no match",
			toolName: "web_search",
			pattern:  "mcp_*",
			expected: false,
		},
		{
			name:     "complex pattern match",
			toolName: "mcp_filesystem_read_file",
			pattern:  "mcp_*_read_*",
			expected: true,
		},
		{
			name:     "all match",
			toolName: "any_tool_name",
			pattern:  "*",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesWildcard(tt.toolName, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchesWildcard(%q, %q) = %v, want %v", tt.toolName, tt.pattern, result, tt.expected)
			}
		})
	}
}

package swarm

import (
	"fmt"
	"strings"
)

// ResumeOptions configures resuming an interrupted agent.
type ResumeOptions struct {
	AgentID    string // Agent to resume
	OutputDir  string // Directory where transcript is stored
	MaxEntries int    // Max transcript entries to restore (0 = all)
}

// ResumeResult holds the restored state for a resumed agent.
type ResumeResult struct {
	Transcript []TranscriptEntry // Recovered transcript entries
	LastRole   string            // Last role in transcript
}

// ResumeAgent recovers state from a persisted transcript to resume an interrupted agent.
// Incomplete tool_call entries (those without a matching tool_result) are filtered out.
func ResumeAgent(opts ResumeOptions) (*ResumeResult, error) {
	if opts.AgentID == "" {
		return nil, fmt.Errorf("agent ID is required for resume")
	}

	path := TranscriptPath(opts.OutputDir, opts.AgentID)
	entries, err := ReadTranscript(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read transcript for %s: %w", opts.AgentID, err)
	}

	// Filter incomplete tool calls
	entries = filterIncompleteToolCalls(entries)

	// Apply max entries limit
	if opts.MaxEntries > 0 && len(entries) > opts.MaxEntries {
		entries = entries[len(entries)-opts.MaxEntries:]
	}

	lastRole := ""
	if len(entries) > 0 {
		lastRole = entries[len(entries)-1].Role
	}

	return &ResumeResult{
		Transcript: entries,
		LastRole:   lastRole,
	}, nil
}

// filterIncompleteToolCalls removes tool_use entries that don't have a
// corresponding tool_result entry following them.
func filterIncompleteToolCalls(entries []TranscriptEntry) []TranscriptEntry {
	if len(entries) == 0 {
		return entries
	}

	// Collect tool IDs that have results
	completedTools := make(map[string]bool)
	for _, e := range entries {
		if e.Role == "tool_result" && e.ToolID != "" {
			completedTools[e.ToolID] = true
		}
	}

	// Filter: keep everything except tool_use without matching result
	var result []TranscriptEntry
	for _, e := range entries {
		if e.Role == "tool_use" && e.ToolID != "" {
			if !completedTools[e.ToolID] {
				continue // Skip incomplete tool call
			}
		}
		result = append(result, e)
	}

	// Also trim any trailing incomplete state
	for len(result) > 0 {
		last := result[len(result)-1]
		if last.Role == "tool_use" && !completedTools[last.ToolID] {
			result = result[:len(result)-1]
			continue
		}
		break
	}

	return result
}

// BuildResumePrompt creates a context prompt that summarizes the resumed state.
func BuildResumePrompt(resumeResult *ResumeResult) string {
	if resumeResult == nil || len(resumeResult.Transcript) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Resumed from previous session\n\n")
	sb.WriteString(fmt.Sprintf("Resuming with %d transcript entries from previous execution.\n", len(resumeResult.Transcript)))
	sb.WriteString("Continue from where you left off.\n")

	return sb.String()
}

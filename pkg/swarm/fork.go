package swarm

import (
	"fmt"
)

// ForkOptions configures forking a subagent from a parent's state.
type ForkOptions struct {
	ParentAgentID    string            // Parent agent to fork from
	ParentSysPrompt  string            // Parent's rendered system prompt (for prompt-cache continuation)
	TranscriptPrefix []TranscriptEntry // Subset of parent transcript to inherit
}

// ForkResult holds the configuration derived from forking.
type ForkResult struct {
	SystemPrompt     string            // Inherited system prompt
	TranscriptPrefix []TranscriptEntry // Transcript entries to prepend
}

// ForkSubagent creates a fork configuration from a parent agent's state.
// The forked agent inherits the parent's rendered system prompt and optionally
// a prefix of the parent's transcript, enabling prompt-cache continuation.
func ForkSubagent(opts ForkOptions) (*ForkResult, error) {
	if opts.ParentAgentID == "" {
		return nil, fmt.Errorf("parent agent ID is required for fork")
	}
	if opts.ParentSysPrompt == "" {
		return nil, fmt.Errorf("parent system prompt is required for fork")
	}

	result := &ForkResult{
		SystemPrompt:     opts.ParentSysPrompt,
		TranscriptPrefix: opts.TranscriptPrefix,
	}

	return result, nil
}

// BuildForkPrompt constructs the full prompt for a forked agent by combining
// the inherited system prompt with the fork-specific task.
func BuildForkPrompt(forkResult *ForkResult, taskPrompt string) string {
	if forkResult == nil {
		return taskPrompt
	}
	// The system prompt is passed separately; the task prompt is what differs
	return taskPrompt
}

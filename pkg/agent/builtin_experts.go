package agent

// builtin_experts.go defines the three built-in experts aligned with Gemini CLI:
// - investigator (from codebase-investigator.ts)
// - help (from cli-help-agent.ts)
// - generalist (from generalist-agent.ts)

const (
	// investigatorSystemPrompt is directly translated from Gemini CLI's codebase-investigator.ts (lines 127-181)
	investigatorSystemPrompt = `You are a specialized codebase investigation agent. Your role is to explore and analyze codebases to help answer questions about their structure, functionality, and implementation details.

## RULES

1. **Read-only exploration**: You can only read files and search the codebase. You cannot modify files.

2. **Systematic investigation**:
   - Start broad, then narrow down
   - Use glob patterns to find relevant files
   - Search file content for specific patterns
   - Read files to understand implementation details

3. **Efficient exploration**:
   - Use search_file_content with appropriate patterns before reading entire files
   - Use glob to identify relevant file paths
   - Focus on files most likely to contain answers

4. **Comprehensive reporting**:
   - Provide clear, structured findings
   - Include specific file paths and line references
   - Explain architectural patterns discovered
   - Note any ambiguities or areas needing clarification

## Scratchpad Management

Use your working memory to track:
- Files already examined (avoid redundant reads)
- Search patterns tried (refine if unproductive)
- Key findings (build cumulative understanding)
- Hypotheses to test (guide next investigation steps)

## Termination

When you have sufficient information to answer the objective comprehensively, output your final report as a JSON object with this structure:

{
  "SummaryOfFindings": "A clear, comprehensive summary answering the original question",
  "ExplorationTrace": "Brief trace of your investigation path (which searches, which files, what you learned)",
  "RelevantLocations": [
    {
      "FilePath": "path/to/file.go",
      "LineRange": "10-50",
      "Description": "What this location contains/demonstrates"
    }
  ]
}

Provide the JSON output in a markdown code block tagged as 'json'.`

	// helpSystemPrompt is translated from cli-help-agent.ts, adapted for nano-agent
	helpSystemPrompt = `You are the nano-agent CLI help assistant. Your role is to answer questions about how to use nano-agent CLI by consulting the official documentation.

## Your Capabilities

You have access to the nano-agent documentation in the docs/ directory. Use the following tools to find answers:
- **list_directory**: Browse the docs/ directory structure
- **read_file**: Read specific documentation files
- **search_file_content**: Search for keywords across documentation files
- **glob**: Find documentation files matching patterns

## Guidelines

1. **Documentation-first**: Base your answers on actual documentation content, not assumptions
2. **Accurate citations**: Reference specific documentation files in your answer
3. **Practical examples**: Include examples from the docs when available
4. **Clarify ambiguity**: If a question is unclear, ask for clarification
5. **Admit limitations**: If the docs don't cover something, say so honestly

## Output Format

When you have found the answer, provide your response as a JSON object:

{
  "answer": "Clear, helpful answer to the user's question with examples if applicable",
  "sources": [
    {
      "file": "docs/GETTING_STARTED.md",
      "section": "Configuration",
      "relevance": "Explains how to set up MCP servers"
    }
  ]
}

Provide the JSON output in a markdown code block tagged as 'json'.`

	// generalistSystemPrompt is empty - signals to reuse parent agent's system prompt
	generalistSystemPrompt = ""
)

// RegisterBuiltinExperts registers the three built-in experts to the registry
func RegisterBuiltinExperts(registry *ExpertRegistry) error {
	experts := []*Expert{
		{
			Name:              "investigator",
			DisplayName:       "Codebase Investigator",
			Description:       "Read-only codebase exploration agent that systematically investigates code structure, finds implementations, and provides detailed reports with file references",
			Source:            "builtin",
			SystemPrompt:      investigatorSystemPrompt,
			QueryTemplate:     "${objective}",
			Model:             "", // Inherit from parent
			Temperature:       0.1,
			MaxTurns:          50,
			MaxTimeMinutes:    10,
			AllowedTools:      []string{"list_directory", "read_file", "glob", "search_file_content"},
			OutputName:        "report",
			OutputDescription: "Structured investigation report with findings, exploration trace, and relevant code locations",
			OutputSchemaJSON: `{
				"type": "object",
				"properties": {
					"SummaryOfFindings": {"type": "string"},
					"ExplorationTrace": {"type": "string"},
					"RelevantLocations": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"FilePath": {"type": "string"},
								"LineRange": {"type": "string"},
								"Description": {"type": "string"}
							}
						}
					}
				},
				"required": ["SummaryOfFindings"]
			}`,
			InputSchema: &ExpertInputSchema{
				Type: "object",
				Properties: map[string]*ExpertPropertySchema{
					"objective": {
						Type:        "string",
						Description: "The investigation objective or question about the codebase",
					},
				},
				Required: []string{"objective"},
			},
		},
		{
			Name:              "help",
			DisplayName:       "CLI Help Assistant",
			Description:       "Answers questions about nano-agent CLI usage by consulting official documentation",
			Source:            "builtin",
			SystemPrompt:      helpSystemPrompt,
			QueryTemplate:     "${question}",
			Model:             "", // Inherit from parent
			Temperature:       0.1,
			MaxTurns:          10,
			MaxTimeMinutes:    3,
			AllowedTools:      []string{"list_directory", "read_file", "search_file_content", "glob"},
			OutputName:        "report",
			OutputDescription: "Answer with documentation sources",
			OutputSchemaJSON: `{
				"type": "object",
				"properties": {
					"answer": {"type": "string"},
					"sources": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"file": {"type": "string"},
								"section": {"type": "string"},
								"relevance": {"type": "string"}
							}
						}
					}
				},
				"required": ["answer"]
			}`,
			InputSchema: &ExpertInputSchema{
				Type: "object",
				Properties: map[string]*ExpertPropertySchema{
					"question": {
						Type:        "string",
						Description: "Question about nano-agent CLI usage",
					},
				},
				Required: []string{"question"},
			},
		},
		{
			Name:              "generalist",
			DisplayName:       "General Purpose Agent",
			Description:       "Full-featured agent with access to all tools and the main agent's system prompt for general-purpose tasks",
			Source:            "builtin",
			SystemPrompt:      generalistSystemPrompt, // Empty = reuse parent's prompt
			QueryTemplate:     "${request}",
			Model:             "",      // Inherit from parent
			Temperature:       0,       // Not set
			MaxTurns:          20,
			MaxTimeMinutes:    10,
			AllowedTools:      []string{"*"}, // All tools
			OutputName:        "result",
			OutputDescription: "Task execution result",
			OutputSchemaJSON:  "", // No schema validation for generalist
			InputSchema: &ExpertInputSchema{
				Type: "object",
				Properties: map[string]*ExpertPropertySchema{
					"request": {
						Type:        "string",
						Description: "The task request",
					},
				},
				Required: []string{"request"},
			},
		},
	}

	for _, expert := range experts {
		if err := registry.Register(expert); err != nil {
			return err
		}
	}

	return nil
}

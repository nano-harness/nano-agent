package slash

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agent"
)

// HandleAgentsCommand processes /agents commands
// Supports: /agents (list), /agents:list, /agents:show <name>
func HandleAgentsCommand(input string, registry *agent.ExpertRegistry) string {
	input = strings.TrimSpace(input)

	// Remove leading /agents prefix
	if !strings.HasPrefix(input, "/agents") {
		return "Invalid command. Usage: /agents, /agents:list, or /agents:show <name>"
	}

	remainder := strings.TrimSpace(strings.TrimPrefix(input, "/agents"))

	// /agents or /agents:list - list all experts
	if remainder == "" || remainder == ":list" {
		return formatExpertList(registry)
	}

	// /agents:show <name> - show expert details
	if strings.HasPrefix(remainder, ":show") {
		expertName := strings.TrimSpace(strings.TrimPrefix(remainder, ":show"))
		if expertName == "" {
			return "Usage: /agents:show <expert-name>"
		}
		return formatExpertDetails(registry, expertName)
	}

	return fmt.Sprintf("Unknown agents command: %s\nUsage: /agents, /agents:list, or /agents:show <name>", remainder)
}

// formatExpertList formats a list of all available experts
func formatExpertList(registry *agent.ExpertRegistry) string {
	experts := registry.List()
	if len(experts) == 0 {
		return "No experts available."
	}

	// Group by source
	bySource := make(map[string][]*agent.Expert)
	for _, expert := range experts {
		bySource[expert.Source] = append(bySource[expert.Source], expert)
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("Available Experts (%d total):\n\n", len(experts)))

	// Display order: builtin, project, user, yaml
	sourceOrder := []string{"builtin", "project", "user", "yaml"}

	for _, source := range sourceOrder {
		expertList, exists := bySource[source]
		if !exists || len(expertList) == 0 {
			continue
		}

		// Sort by name within source
		sort.Slice(expertList, func(i, j int) bool {
			return expertList[i].Name < expertList[j].Name
		})

		buf.WriteString(fmt.Sprintf("## %s (%d)\n", strings.ToUpper(source), len(expertList)))
		for _, expert := range expertList {
			model := expert.Model
			if model == "" {
				model = "(inherit)"
			}
			buf.WriteString(fmt.Sprintf("  @%-20s | %-12s | %s\n",
				expert.Name,
				model,
				truncate(expert.Description, 60)))
		}
		buf.WriteString("\n")
	}

	buf.WriteString("Use `/agents:show <name>` for details about a specific expert.\n")
	buf.WriteString("Trigger an expert with: @expert-name <your request>\n")

	return buf.String()
}

// formatExpertDetails formats detailed information about a specific expert
func formatExpertDetails(registry *agent.ExpertRegistry, expertName string) string {
	expert, exists := registry.Get(expertName)
	if !exists {
		return fmt.Sprintf("Expert '%s' not found. Use `/agents` to see available experts.", expertName)
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("Expert: @%s\n", expert.Name))
	buf.WriteString(strings.Repeat("=", 80) + "\n\n")

	buf.WriteString(fmt.Sprintf("Display Name:  %s\n", expert.DisplayName))
	buf.WriteString(fmt.Sprintf("Source:        %s\n", expert.Source))
	buf.WriteString(fmt.Sprintf("Description:   %s\n\n", expert.Description))

	// Model info
	model := expert.Model
	if model == "" {
		model = "(inherit from parent)"
	}
	buf.WriteString(fmt.Sprintf("Model:         %s\n", model))
	if expert.Temperature > 0 {
		buf.WriteString(fmt.Sprintf("Temperature:   %.2f\n", expert.Temperature))
	}
	buf.WriteString(fmt.Sprintf("Max Turns:     %d\n", expert.MaxTurns))
	buf.WriteString(fmt.Sprintf("Max Time:      %d minutes\n\n", expert.MaxTimeMinutes))

	// Tools
	buf.WriteString("Allowed Tools:\n")
	if len(expert.AllowedTools) == 1 && expert.AllowedTools[0] == "*" {
		buf.WriteString("  (all tools)\n")
	} else {
		for _, tool := range expert.AllowedTools {
			buf.WriteString(fmt.Sprintf("  - %s\n", tool))
		}
	}
	buf.WriteString("\n")

	// Input/Output
	if expert.InputSchema != nil && len(expert.InputSchema.Required) > 0 {
		buf.WriteString(fmt.Sprintf("Input Field:   %s\n", expert.InputSchema.Required[0]))
	}
	buf.WriteString(fmt.Sprintf("Output Field:  %s\n", expert.OutputName))
	if expert.OutputDescription != "" {
		buf.WriteString(fmt.Sprintf("Output:        %s\n", expert.OutputDescription))
	}
	buf.WriteString("\n")

	// System prompt (truncated)
	if expert.SystemPrompt != "" {
		buf.WriteString("System Prompt (first 200 chars):\n")
		promptPreview := truncate(expert.SystemPrompt, 200)
		buf.WriteString(fmt.Sprintf("  %s...\n\n", promptPreview))
	} else if expert.Name == "generalist" {
		buf.WriteString("System Prompt: (reuses main agent's system prompt)\n\n")
	}

	buf.WriteString(fmt.Sprintf("Usage: @%s <your request>\n", expert.Name))

	return buf.String()
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

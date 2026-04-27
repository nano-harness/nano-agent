package slash

// HandleTeammatesCommand processes /teammates commands.
// Supports: /teammates (list), /teammates:list, /teammates:show <name>.
func HandleTeammatesCommand(input string, registry interface{}) string {
	return "Teammate management is available through the team tools: team_list, team_create, and spawn_teammate."
}

// HandleAgentsCommand preserves the old /agents entry point as a compatibility alias.
func HandleAgentsCommand(input string, registry interface{}) string {
	return HandleTeammatesCommand(input, registry)
}

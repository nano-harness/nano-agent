package skill

const builtinNanoSymphonyInstructions = `---
name: nano-symphony
description: Coordinate with a nano-symphony orchestrator through the injected MCP server.
triggers:
  - symphony
  - orchestrator
auto_invoke: true
priority: 100
---

When running under nano-symphony, use the symphony MCP tools to report meaningful progress, request orchestration context when needed, and ensure final session status is communicated before completion when tools are available.
`

func builtinSkills() []*Skill {
	sk, err := parseSkillContent(builtinNanoSymphonyInstructions, "builtin:nano-symphony")
	if err != nil {
		return nil
	}
	sk.Scope = ScopeBuiltin
	return []*Skill{sk}
}

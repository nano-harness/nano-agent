package slash

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/team"
)

// HandleTeammatesCommand processes /teammates commands.
//
// Supports:
//   - /teammates           – list teammates of the current team (or all teams when none specified)
//   - /teammates:list      – alias for /teammates
//   - /teammates:show NAME – show details of a single teammate
//
// teamName may be empty when no team is active in the current session; in
// that case the handler falls back to listing teammates across all known teams
// so the user still receives meaningful output instead of a placeholder.
func HandleTeammatesCommand(input, teamName string) string {
	cmd, arg := parseTeammatesInput(input)

	switch cmd {
	case "show":
		if arg == "" {
			return "用法：/teammates:show <name>"
		}
		return formatTeammateDetail(teamName, arg)
	default:
		// list (default)
		return formatTeammatesList(teamName)
	}
}

// HandleAgentsCommand preserves the old /agents entry point as a compatibility alias.
func HandleAgentsCommand(input, teamName string) string {
	// Translate /agents[:show] → /teammates[:show] before dispatching so the
	// underlying handler sees a consistent prefix.
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "/agents") {
		trimmed = "/teammates" + strings.TrimPrefix(trimmed, "/agents")
	}
	return HandleTeammatesCommand(trimmed, teamName)
}

// parseTeammatesInput extracts the sub-command and its argument from a
// /teammates[:sub] [arg] input string. Recognized sub-commands are "list"
// (default) and "show". Unknown sub-commands fall through to "list".
func parseTeammatesInput(input string) (sub, arg string) {
	trimmed := strings.TrimSpace(input)
	trimmed = strings.TrimPrefix(trimmed, "/")
	// Split off any argument.
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "list", ""
	}
	head := parts[0]
	if len(parts) > 1 {
		arg = strings.TrimSpace(strings.Join(parts[1:], " "))
	}
	// head is like "teammates", "teammates:list", "teammates:show".
	if i := strings.Index(head, ":"); i >= 0 {
		return strings.ToLower(head[i+1:]), arg
	}
	return "list", arg
}

// formatTeammatesList produces a human-readable summary of all teammates.
// When teamName is non-empty, only that team is queried; otherwise every
// team known under ~/.nano/teams is included.
func formatTeammatesList(teamName string) string {
	teams, err := loadTeams(teamName)
	if err != nil {
		return fmt.Sprintf("❌ 无法加载团队信息：%v", err)
	}
	if len(teams) == 0 {
		if teamName != "" {
			return fmt.Sprintf("ℹ️  团队 %q 中暂无 teammate。使用 spawn_teammate 工具创建。", teamName)
		}
		return "ℹ️  暂无可用团队。使用 spawn_teammate 工具创建第一个 teammate。"
	}

	var b strings.Builder
	for _, t := range teams {
		fmt.Fprintf(&b, "Team: %s", t.Name)
		if t.Description != "" {
			fmt.Fprintf(&b, " — %s", t.Description)
		}
		b.WriteString("\n")
		if len(t.Members) == 0 {
			b.WriteString("  (暂无 teammate)\n")
			continue
		}
		// Stable ordering by name.
		members := append([]team.TeamMember(nil), t.Members...)
		sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
		for _, m := range members {
			fmt.Fprintf(&b, "  - %-16s  kind=%-10s  mode=%-12s  %s\n",
				m.Name,
				orDefault(m.Kind, "unknown"),
				orDefault(m.Mode, "default"),
				memberStatus(m),
			)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatTeammateDetail produces a detailed view of a single teammate looked
// up by name across all teams (or the specified team only).
func formatTeammateDetail(teamName, name string) string {
	teams, err := loadTeams(teamName)
	if err != nil {
		return fmt.Sprintf("❌ 无法加载团队信息：%v", err)
	}
	for _, t := range teams {
		for _, m := range t.Members {
			if strings.EqualFold(m.Name, name) || strings.EqualFold(m.AgentID, name) {
				return formatMember(t, m)
			}
		}
	}
	return fmt.Sprintf("❌ 未找到 teammate：%s", name)
}

func formatMember(t *team.Team, m team.TeamMember) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Teammate: %s\n", m.Name)
	fmt.Fprintf(&b, "  team:        %s\n", t.Name)
	fmt.Fprintf(&b, "  agent_id:    %s\n", m.AgentID)
	fmt.Fprintf(&b, "  session_id:  %s\n", m.SessionID)
	fmt.Fprintf(&b, "  kind:        %s\n", orDefault(m.Kind, "unknown"))
	fmt.Fprintf(&b, "  mode:        %s\n", orDefault(m.Mode, "default"))
	fmt.Fprintf(&b, "  status:      %s\n", memberStatus(m))
	if m.Model != "" {
		fmt.Fprintf(&b, "  model:       %s\n", m.Model)
	}
	if len(m.ContextProviders) > 0 {
		fmt.Fprintf(&b, "  context:     %s\n", strings.Join(m.ContextProviders, ","))
	}
	if m.MaxRuntimeSec > 0 {
		fmt.Fprintf(&b, "  max_runtime: %ds\n", m.MaxRuntimeSec)
	}
	if m.Sandbox.Backend != "" || m.Sandbox.Lifecycle != "" {
		fmt.Fprintf(&b, "  sandbox:     backend=%s lifecycle=%s scope=%s\n",
			orDefault(m.Sandbox.Backend, "none"),
			orDefault(m.Sandbox.Lifecycle, "none"),
			orDefault(m.Sandbox.Scope, "team"))
	}
	if m.PID > 0 {
		fmt.Fprintf(&b, "  pid:         %d\n", m.PID)
	}
	return strings.TrimRight(b.String(), "\n")
}

func memberStatus(m team.TeamMember) string {
	if m.IsActive {
		return "● active"
	}
	return "○ idle"
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// teamLoader is overridable for tests.
var teamLoader = func(name string) (*team.Team, error) { return team.ReadTeam(name) }

// teamLister is overridable for tests.
var teamLister = team.ListAllTeams

func loadTeams(name string) ([]*team.Team, error) {
	if name != "" {
		t, err := teamLoader(name)
		if err != nil {
			return nil, err
		}
		return []*team.Team{t}, nil
	}
	return teamLister()
}

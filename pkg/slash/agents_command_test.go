package slash

import (
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/team"
)

func setFakeTeams(t *testing.T, teams []*team.Team) {
	t.Helper()
	prevList := teamLister
	prevLoad := teamLoader
	teamLister = func() ([]*team.Team, error) { return teams, nil }
	teamLoader = func(name string) (*team.Team, error) {
		for _, x := range teams {
			if x.Name == name {
				return x, nil
			}
		}
		return nil, &fakeMissingTeamErr{name: name}
	}
	t.Cleanup(func() { teamLister = prevList; teamLoader = prevLoad })
}

type fakeMissingTeamErr struct{ name string }

func (e *fakeMissingTeamErr) Error() string { return "team not found: " + e.name }

func TestHandleAgentsCommand_AliasesToTeammates(t *testing.T) {
	setFakeTeams(t, []*team.Team{{
		Name: "alpha",
		Members: []team.TeamMember{
			{Name: "alice", Kind: "in_process", IsActive: true, AgentID: "alice@alpha"},
		},
	}})
	out := HandleAgentsCommand("/agents", "")
	if !strings.Contains(out, "alice") || !strings.Contains(out, "alpha") {
		t.Errorf("alias should produce same listing, got %q", out)
	}

	out = HandleAgentsCommand("/agents:show alice", "")
	if !strings.Contains(out, "alice@alpha") {
		t.Errorf("expected detail view via alias, got %q", out)
	}
}

func TestHandleTeammatesCommand_EmptyState(t *testing.T) {
	setFakeTeams(t, nil)
	out := HandleTeammatesCommand("/teammates", "")
	if !strings.Contains(out, "暂无") {
		t.Errorf("expected empty-state message, got %q", out)
	}
}

func TestHandleTeammatesCommand_FilterByTeam(t *testing.T) {
	setFakeTeams(t, []*team.Team{
		{Name: "alpha", Members: []team.TeamMember{{Name: "a1", AgentID: "a1@alpha"}}},
		{Name: "beta", Members: []team.TeamMember{{Name: "b1", AgentID: "b1@beta"}}},
	})
	out := HandleTeammatesCommand("/teammates", "alpha")
	if !strings.Contains(out, "a1") || strings.Contains(out, "b1") {
		t.Errorf("expected only alpha team, got %q", out)
	}
}

func TestHandleTeammatesCommand_ShowMissing(t *testing.T) {
	setFakeTeams(t, []*team.Team{{Name: "alpha", Members: []team.TeamMember{{Name: "a1", AgentID: "a1@alpha"}}}})
	out := HandleTeammatesCommand("/teammates:show ghost", "")
	if !strings.Contains(out, "未找到") {
		t.Errorf("expected not-found message, got %q", out)
	}
}

func TestHandleTeammatesCommand_ShowUsage(t *testing.T) {
	out := HandleTeammatesCommand("/teammates:show", "")
	if !strings.Contains(out, "用法") {
		t.Errorf("expected usage hint, got %q", out)
	}
}

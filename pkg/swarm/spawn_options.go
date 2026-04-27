package swarm

import (
	"fmt"

	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/team"
)

type spawnIdentity struct {
	agentID   string
	sessionID string
	teammate  *TeammateIdentity
}

func (opts SpawnOptions) validate() error {
	if opts.TeamName == "" {
		return fmt.Errorf("team name cannot be empty")
	}
	if opts.Name == "" {
		return fmt.Errorf("teammate name cannot be empty")
	}
	return nil
}

func (opts SpawnOptions) newIdentity() spawnIdentity {
	sessionID := nanoruntime.BuildTeammateSessionID(opts.TeamName, opts.Name)
	agentID := opts.Name + "@" + opts.TeamName
	return spawnIdentity{
		agentID:   agentID,
		sessionID: sessionID,
		teammate: &TeammateIdentity{
			AgentID:         agentID,
			AgentName:       opts.Name,
			TeamName:        opts.TeamName,
			Color:           opts.Color,
			ParentSessionID: "",
		},
	}
}

func (opts SpawnOptions) newTeamMember(agentID, sessionID, memberKind string) team.TeamMember {
	return team.TeamMember{
		AgentID:   agentID,
		Name:      opts.Name,
		Color:     opts.Color,
		Mode:      opts.PermissionMode,
		IsActive:  true,
		SessionID: sessionID,
		Kind:      memberKind,
	}
}

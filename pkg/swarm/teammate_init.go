package swarm

import (
	"context"
	"fmt"
	"strings"
)

// InitializeTeammate performs initialization for a teammate agent
// This includes reading team config, applying permissions, etc.
func InitializeTeammate(ctx context.Context, identity *TeammateIdentity) error {
	if identity == nil {
		return fmt.Errorf("identity cannot be nil")
	}
	_ = ctx

	// Validate required identity fields.
	if identity.TeamName == "" {
		return fmt.Errorf("team name is required")
	}
	if identity.AgentName == "" {
		return fmt.Errorf("agent name is required")
	}
	if identity.AgentID == "" {
		identity.AgentID = identity.AgentName + "@" + identity.TeamName
	}

	// Apply teammate-specific constraints from the active config layer.
	// Per-teammate overrides (allowed tools/model/context providers/permission mode)
	// should already be present on identity (filled by SpawnOptions / agent profiles).

	// Normalize legacy modes to stable values.
	if identity.PermissionMode != "" {
		switch strings.ToLower(strings.TrimSpace(identity.PermissionMode)) {
		case "auto":
			identity.PermissionMode = "yolo"
		case "ask":
			identity.PermissionMode = "default"
		}
	}

	return nil
}

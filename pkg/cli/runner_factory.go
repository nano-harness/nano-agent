package cli

import (
	"context"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/swarm"
)

func init() {
	swarm.SetDefaultRunFunc(runDefaultTeammate)
}

func runDefaultTeammate(ctx context.Context, identity *swarm.TeammateIdentity, initialPrompt string, cfg *config.Config) error {
	if err := swarm.InitializeTeammate(ctx, identity); err != nil {
		return err
	}
	cfg = configForTeammate(cfg, identity)

	// Resolve permission mode using unified resolver
	res, warns := ResolvePermission(cfg, PermissionResolveOpts{
		EnvHintEnabled: true,
	})
	LogPermissionResolution("swarm.teammate", res, warns)

	// Match the hidden teammate CLI path: the lead-authorized spawn controls when
	// this runner starts, while teammate mode withholds lead-only swarm tools.
	eng, err := engine.NewTeammateEngine(cfg, identity)
	if err != nil {
		return err
	}
	// Auto-approve all tools for teammate autonomy.
	eng.Agent.SetApprovalHandlerV2(func(*agent.ToolCallInfo) agent.ApprovalDecision {
		return agent.ApprovalApproveOnce
	})
	defer eng.Shutdown()

	ctx = swarm.WithTeammate(ctx, identity)
	sessionID := nanoruntime.BuildTeammateSessionID(identity.TeamName, identity.AgentName)
	session := eng.Agent.GetSessionManager().GetOrCreateSession(sessionID)
	session.SetMetadata("swarm", agent.SessionMetadata{
		TeamName:   identity.TeamName,
		AgentName:  identity.AgentName,
		IsTeammate: true,
	})
	return eng.Agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, initialPrompt, nil, func(event.StreamEvent) {})
}

func configForTeammate(cfg *config.Config, identity *swarm.TeammateIdentity) *config.Config {
	if cfg != nil && identity != nil && identity.PermissionMode != "" {
		childCfg := cfg.DeepCopy()
		// Translate legacy teammate modes: auto→yolo (no confirmation),
		// ask→default (prompt for confirmation).
		switch identity.PermissionMode {
		case "auto":
			childCfg.PermissionMode = "yolo"
		case "ask":
			childCfg.PermissionMode = "default"
		default:
			childCfg.PermissionMode = identity.PermissionMode
		}
		cfg = childCfg
	}
	if cfg != nil && identity != nil && len(identity.AllowedTools) > 0 {
		childCfg := cfg.DeepCopy()
		childCfg.EnabledTools = append([]string(nil), identity.AllowedTools...)
		cfg = childCfg
	}
	if cfg != nil && identity != nil && identity.Model != "" {
		childCfg := cfg.DeepCopy()
		childCfg.Model = identity.Model
		cfg = childCfg
	}
	if cfg != nil && identity != nil && len(identity.Fallbacks) > 0 {
		childCfg := cfg.DeepCopy()
		childCfg.Fallbacks = append([]string(nil), identity.Fallbacks...)
		if childCfg.ModelRouting != nil {
			childCfg.ModelRouting.Fallbacks = nil
		}
		cfg = childCfg
	}
	if cfg != nil && identity != nil && len(identity.ContextProviders) > 0 {
		cfg = configWithContextProviders(cfg, identity.ContextProviders)
	}
	return cfg
}

func configWithContextProviders(cfg *config.Config, providers []string) *config.Config {
	allowed := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		normalized := strings.ToLower(strings.TrimSpace(provider))
		if normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	if _, ok := allowed["all"]; ok || len(allowed) == 0 {
		return cfg
	}
	childCfg := cfg.DeepCopy()
	if _, ok := allowed["memory"]; !ok {
		childCfg.Memory = nil
	}
	if _, ok := allowed["skills"]; !ok && childCfg.Skills != nil {
		childCfg.Skills.Enabled = false
	}
	if _, ok := allowed["openspec"]; !ok && childCfg.OpenSpec != nil {
		childCfg.OpenSpec.InjectContext = false
	}
	return childCfg
}

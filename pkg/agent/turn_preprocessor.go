package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/preprocessor"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

// preprocessInput runs all turn input enrichment through a single
// preprocessor.Pipeline so the execution loop does not need to know each
// feature-specific preprocessing branch.
func (t *Turn) preprocessInput(ctx context.Context, cfg *config.Config) {
	req := &preprocessor.Request{
		UserInput:  t.UserInput,
		WorkingDir: t.WorkingDir,
	}
	if t.agent != nil {
		req.Mailbox = t.agent.Mailbox()
	}

	pipeline := preprocessor.NewPipeline(
		preprocessor.MailboxStep(),
		preprocessor.OpenSpecStep(func() preprocessor.OpenSpecOptions {
			if cfg == nil || cfg.OpenSpec == nil {
				return preprocessor.OpenSpecOptions{}
			}
			return preprocessor.OpenSpecOptions{
				Enabled:         cfg.OpenSpec.Enabled,
				RootDir:         cfg.OpenSpec.RootDir,
				WorkingDir:      t.WorkingDir,
				DefaultSchema:   cfg.OpenSpec.DefaultSchema,
				MaxArtifactSize: cfg.OpenSpec.MaxArtifactSize,
			}
		}),
		t.skillPreprocessorStep(ctx, cfg),
		preprocessor.RoutinesStep(),
	)
	if err := pipeline.Run(ctx, req); err != nil {
		logger.Warnf("Turn preprocessing failed: %v", err)
		return
	}

	if cmd := req.Metadata["openspec.command"]; cmd != "" {
		logger.Infof("Detected OpenSpec command: /opsx:%s (change: %s)", cmd, req.Metadata["openspec.change"])
	}
	if req.SystemPromptAddition != "" {
		if t.systemPrompt == "" {
			t.systemPrompt = t.buildUnifiedSystemPrompt() + req.SystemPromptAddition
		} else {
			t.systemPrompt += req.SystemPromptAddition
		}
	}
	if req.UserInput != t.UserInput {
		if req.Metadata["mailbox.drained"] == "true" {
			logger.Debugf("Appended %d chars from mailbox/preprocessors to user input", len(req.UserInput)-len(t.UserInput))
		}
		t.UserInput = req.UserInput
	}
}

func (t *Turn) skillPreprocessorStep(ctx context.Context, cfg *config.Config) preprocessor.Step {
	return preprocessor.StepFunc{
		StepName: "skill",
		Fn: func(_ context.Context, req *preprocessor.Request) error {
			before := req.UserInput
			t.UserInput = req.UserInput
			t.preprocessSkillCommand(ctx, cfg)
			req.UserInput = t.UserInput
			if t.UserInput != before {
				req.SetMetadata("skill.rewritten", "true")
			}
			return nil
		},
	}
}

// preprocessOpenSpecCommand detects /opsx: commands in user input and enriches
// the turn with OpenSpec context, system prompt additions, and modified user messages.
func (t *Turn) preprocessOpenSpecCommand(cfg *config.Config) {
	if cfg == nil || cfg.OpenSpec == nil || !cfg.OpenSpec.Enabled {
		return
	}

	result := preprocessor.ProcessOpenSpecCommand(t.UserInput, preprocessor.OpenSpecOptions{
		Enabled:         cfg.OpenSpec.Enabled,
		RootDir:         cfg.OpenSpec.RootDir,
		WorkingDir:      t.WorkingDir,
		DefaultSchema:   cfg.OpenSpec.DefaultSchema,
		MaxArtifactSize: cfg.OpenSpec.MaxArtifactSize,
	})
	if !result.Handled {
		return
	}

	logger.Infof("Detected OpenSpec command: /opsx:%s (change: %s)", result.CommandType, result.ChangeName)
	if result.Err != nil {
		logger.Errorf("OpenSpec command failed: %v", result.Err)
		t.UserInput = result.UserInput
		return
	}

	t.UserInput = result.UserInput

	// Inject additional system prompt context
	if result.SystemPromptAddition != "" {
		if t.systemPrompt == "" {
			// Build the unified system prompt before appending the OpenSpec addition
			t.systemPrompt = t.buildUnifiedSystemPrompt() + result.SystemPromptAddition
		} else {
			t.systemPrompt += result.SystemPromptAddition
		}
	}
}

// preprocessSkillCommand detects /skill: commands in user input and handles
// skill activation/deactivation. It also performs auto-matching of skills
// based on user input when auto_invoke is enabled.
func (t *Turn) preprocessSkillCommand(ctx context.Context, cfg *config.Config) {
	if cfg == nil || cfg.Skills == nil || !cfg.Skills.Enabled {
		return
	}

	if t.SystemPromptBuilder == nil {
		return
	}
	sm := t.SystemPromptBuilder.skillManager
	if sm == nil {
		return
	}

	input := strings.TrimSpace(t.UserInput)

	// Handle /skill: commands
	if strings.HasPrefix(input, "/skill:") {
		t.handleSkillSlashCommand(ctx, sm, input)
		return
	}

	// Auto-match skills if global auto_invoke is enabled
	if sm.IsAutoInvokeEnabled() {
		matches := sm.Match(&skill.MatchContext{
			UserInput: input,
		}, false)

		activated := false
		for _, m := range matches {
			if err := sm.ActivateSkill(m.Skill.Name); err != nil {
				logger.Warnf("Failed to auto-activate skill %q: %v", m.Skill.Name, err)
				break // likely hit max active skills
			}
			activated = true
			logger.Infof("Auto-activated skill %q (reason: %s, score: %.2f)", m.Skill.Name, m.Reason, m.Score)
		}

		// If skills were activated, rebuild system prompt
		if activated {
			t.systemPrompt = "" // Force rebuild on next access
			t.SystemPromptBuilder.InvalidatePromptCache()
		}
	}
}

// handleSkillSlashCommand processes /skill: slash commands.
func (t *Turn) handleSkillSlashCommand(ctx context.Context, sm *skill.Manager, input string) {
	parts := strings.SplitN(input, " ", 2)
	cmd := strings.TrimPrefix(parts[0], "/skill:")
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case "list":
		listing := sm.ListSkillNames()
		t.UserInput = fmt.Sprintf("The user requested a list of available skills. Here is the current status:\n\n%s\nPresent this information to the user.", listing)

	case "use":
		if arg == "" {
			t.UserInput = "The user tried to activate a skill but didn't specify a name. Ask them which skill they want to use. Available skills:\n\n" + sm.ListSkillNames()
			return
		}
		if err := sm.ActivateSkill(arg); err != nil {
			t.UserInput = fmt.Sprintf("The user tried to activate skill %q but it failed: %v\nHelp them resolve the issue.", arg, err)
			return
		}
		logger.Infof("Manually activated skill %q", arg)
		s := sm.GetByName(arg)
		t.systemPrompt = "" // Force rebuild
		t.SystemPromptBuilder.InvalidatePromptCache()
		t.UserInput = fmt.Sprintf("The user activated skill '%s'. Acknowledge that the skill is now active and briefly describe what it does: %s", arg, s.Description)

	case "off":
		if arg == "" {
			t.UserInput = "The user tried to deactivate a skill but didn't specify a name. Ask them which skill to deactivate."
			return
		}
		sm.DeactivateSkill(arg) //nolint:errcheck // non-fatal: skill is deactivated in-memory even if persist fails
		logger.Infof("Deactivated skill %q", arg)
		t.systemPrompt = "" // Force rebuild
		t.SystemPromptBuilder.InvalidatePromptCache()
		t.UserInput = fmt.Sprintf("The user deactivated skill '%s'. Acknowledge that the skill has been deactivated.", arg)

	case "info":
		if arg == "" {
			t.UserInput = "The user tried to get skill info but didn't specify a name. Ask them which skill they want info about."
			return
		}
		s := sm.GetByName(arg)
		if s == nil {
			t.UserInput = fmt.Sprintf("The user requested info about skill %q but it was not found. Available skills:\n\n%s", arg, sm.ListSkillNames())
			return
		}
		t.UserInput = fmt.Sprintf("The user requested info about skill '%s'. Present the following:\n\nName: %s\nDescription: %s\nScope: %s\nTriggers: %s\nGlobs: %s\nAuto-invoke: %t\nPriority: %d\nActive: %t\n\nFull Instructions:\n%s",
			arg, s.Name, s.Description, s.Scope,
			strings.Join(s.Triggers, ", "), strings.Join(s.Globs, ", "),
			s.IsAutoInvoke(), s.Priority, sm.IsActive(s.Name), s.Instructions)

	case "install":
		if arg == "" {
			t.UserInput = "The user tried to install a skill but didn't provide a URL. Ask them for the URL of the SKILL.md file to install."
			return
		}
		ctx, cancel := context.WithTimeout(ctx, skill.InstallHTTPTimeout)
		defer cancel()
		installed, err := sm.InstallSkill(ctx, arg)
		if err != nil {
			t.UserInput = fmt.Sprintf("The user tried to install a skill from %q but it failed: %v\nHelp them resolve the issue.", arg, err)
			return
		}
		logger.Infof("Installed skill %q from %s", installed.Name, arg)
		t.systemPrompt = "" // Force rebuild
		t.SystemPromptBuilder.InvalidatePromptCache()
		t.UserInput = fmt.Sprintf("The user installed skill '%s' from %s. The skill is now available. Briefly describe what it does: %s\n\nAvailable commands: /skill:use %s (to activate), /skill:info %s (for details)",
			installed.Name, arg, installed.Description, installed.Name, installed.Name)

	default:
		t.UserInput = fmt.Sprintf("Unknown skill command '/skill:%s'. Available commands: /skill:list, /skill:use <name>, /skill:off <name>, /skill:info <name>, /skill:install <url>", cmd)
	}
}

// preprocessRoutinesCommand detects /routines slash commands and converts them
// into LLM prompts that invoke the manage_routine tool.
//
// The implementation is now a single-step preprocessor.Pipeline so future
// command-style preprocessors can be added by appending steps instead of
// branching here.
func (t *Turn) preprocessRoutinesCommand() {
	pipeline := preprocessor.NewPipeline(preprocessor.RoutinesStep())
	req := &preprocessor.Request{UserInput: t.UserInput, WorkingDir: t.WorkingDir}
	if err := pipeline.Run(context.Background(), req); err != nil {
		logger.Warnf("Routines preprocessing failed: %v", err)
		return
	}
	t.UserInput = req.UserInput
}

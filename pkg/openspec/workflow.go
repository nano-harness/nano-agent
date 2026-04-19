package openspec

import (
	"fmt"
	"strings"
)

// WorkflowEngine orchestrates the execution of /opsx: commands.
// It delegates to the ArtifactManager for file operations and uses
// the LLM (via callback) for artifact content generation.
type WorkflowEngine struct {
	manager       *ArtifactManager
	defaultSchema string
}

// NewWorkflowEngine creates a new WorkflowEngine.
func NewWorkflowEngine(manager *ArtifactManager, defaultSchema string) *WorkflowEngine {
	if defaultSchema == "" {
		defaultSchema = "spec-driven"
	}
	return &WorkflowEngine{
		manager:       manager,
		defaultSchema: defaultSchema,
	}
}

// Manager returns the underlying ArtifactManager.
func (we *WorkflowEngine) Manager() *ArtifactManager {
	return we.manager
}

// HandleCommand processes an /opsx: command and returns instructions
// for the LLM to follow. This doesn't call the LLM directly but
// prepares the context and instructions that will be injected into
// the agent's turn execution.
func (we *WorkflowEngine) HandleCommand(cmd *Command) (*WorkflowResult, error) {
	switch cmd.Type {
	case CommandPropose:
		return we.handlePropose(cmd)
	case CommandNew:
		return we.handleNew(cmd)
	case CommandApply:
		return we.handleApply(cmd)
	case CommandStatus:
		return we.handleStatus(cmd)
	case CommandContinue:
		return we.handleContinue(cmd)
	case CommandFastForward:
		return we.handleFastForward(cmd)
	case CommandVerify:
		return we.handleVerify(cmd)
	case CommandArchive:
		return we.handleArchive(cmd)
	case CommandExplore:
		return we.handleExplore(cmd)
	case CommandSync:
		return we.handleSync(cmd)
	default:
		return nil, fmt.Errorf("unsupported command: %s", cmd.Type)
	}
}

// WorkflowResult contains the output of a workflow command execution.
// It provides context and instructions to inject into the agent's turn.
type WorkflowResult struct {
	// SystemPromptAddition is additional context to inject into the system prompt
	SystemPromptAddition string
	// UserMessageOverride replaces the original user message with enriched context
	UserMessageOverride string
	// StatusMessage is a human-readable summary of what happened
	StatusMessage string
	// Change is the relevant change (if any)
	Change *Change
}

func (we *WorkflowEngine) handlePropose(cmd *Command) (*WorkflowResult, error) {
	changeName := cmd.ChangeName
	if changeName == "" {
		return &WorkflowResult{
			UserMessageOverride: "The user wants to create a new OpenSpec change proposal but didn't specify a name. " +
				"Ask them for a kebab-case name for the change (e.g., 'add-dark-mode', 'fix-auth-bug').",
		}, nil
	}

	change, err := we.manager.CreateChange(changeName, we.defaultSchema)
	if err != nil {
		// If change already exists, load it instead
		if strings.Contains(err.Error(), "already exists") {
			change, err = we.manager.GetChange(changeName)
			if err != nil {
				return nil, fmt.Errorf("failed to load existing change: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to create change: %w", err)
		}
	}

	// Load project config for context
	projConfig, _ := we.manager.ReadProjectConfig()

	// Build instructions for LLM
	instructions := we.buildProposeInstructions(changeName, change, projConfig, cmd)

	return &WorkflowResult{
		SystemPromptAddition: we.buildOpenSpecSystemContext(projConfig),
		UserMessageOverride:  instructions,
		StatusMessage:        fmt.Sprintf("Created change: %s\nSchema: %s\nReady to generate artifacts.", changeName, we.defaultSchema),
		Change:               change,
	}, nil
}

func (we *WorkflowEngine) handleNew(cmd *Command) (*WorkflowResult, error) {
	changeName := cmd.ChangeName
	if changeName == "" {
		return &WorkflowResult{
			UserMessageOverride: "The user wants to start a new OpenSpec change but didn't specify a name. " +
				"Ask them for a kebab-case name for the change.",
		}, nil
	}

	change, err := we.manager.CreateChange(changeName, we.defaultSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to create change: %w", err)
	}

	return &WorkflowResult{
		StatusMessage: fmt.Sprintf("Created openspec/changes/%s/\nSchema: %s\n\nReady to create: proposal\nUse /opsx:continue to create it, or /opsx:ff to create all artifacts.", changeName, we.defaultSchema),
		Change:        change,
	}, nil
}

func (we *WorkflowEngine) handleApply(cmd *Command) (*WorkflowResult, error) {
	changeName := cmd.ChangeName
	if changeName == "" {
		// Try to find the only active change
		changes, err := we.manager.ListChanges()
		if err != nil {
			return nil, err
		}
		if len(changes) == 0 {
			return &WorkflowResult{
				UserMessageOverride: "No active OpenSpec changes found. Use /opsx:propose to create one first.",
			}, nil
		}
		if len(changes) == 1 {
			changeName = changes[0]
		} else {
			return &WorkflowResult{
				UserMessageOverride: fmt.Sprintf("Multiple active changes found: %s\nPlease specify which one to apply: /opsx:apply <change-name>", strings.Join(changes, ", ")),
			}, nil
		}
	}

	change, err := we.manager.GetChange(changeName)
	if err != nil {
		return nil, err
	}

	// Read tasks
	tasksContent, err := we.manager.ReadArtifact(changeName, "tasks")
	if err != nil {
		return &WorkflowResult{
			UserMessageOverride: fmt.Sprintf("Cannot apply change %q: tasks.md not found. Run /opsx:propose or /opsx:ff first to generate planning artifacts.", changeName),
		}, nil
	}

	tasks := ParseTasks(tasksContent)
	if len(tasks) == 0 {
		return &WorkflowResult{
			UserMessageOverride: fmt.Sprintf("No tasks found in %s/tasks.md. The file may be empty or incorrectly formatted.", changeName),
		}, nil
	}

	// Find incomplete tasks
	var incomplete []Task
	for _, t := range tasks {
		if t.Status != TaskStatusComplete {
			incomplete = append(incomplete, t)
		}
	}

	if len(incomplete) == 0 {
		return &WorkflowResult{
			StatusMessage: fmt.Sprintf("All %d tasks in %s are already complete! Use /opsx:verify to validate or /opsx:archive to finish.", len(tasks), changeName),
		}, nil
	}

	// Build apply instructions with full context
	instructions := we.buildApplyInstructions(changeName, change, tasks, incomplete)

	return &WorkflowResult{
		SystemPromptAddition: we.buildApplySystemContext(changeName, change),
		UserMessageOverride:  instructions,
		StatusMessage:        fmt.Sprintf("Implementing %s: %d/%d tasks remaining", changeName, len(incomplete), len(tasks)),
		Change:               change,
	}, nil
}

func (we *WorkflowEngine) handleStatus(cmd *Command) (*WorkflowResult, error) {
	changeName := cmd.ChangeName
	if changeName == "" {
		// List all changes with status
		changes, err := we.manager.ListChanges()
		if err != nil {
			return nil, err
		}
		if len(changes) == 0 {
			return &WorkflowResult{
				StatusMessage: "No active OpenSpec changes.",
			}, nil
		}

		var sb strings.Builder
		sb.WriteString("Active OpenSpec Changes:\n\n")
		for _, name := range changes {
			status, err := we.manager.GetChangeStatus(name)
			if err != nil {
				fmt.Fprintf(&sb, "- %s: (error loading)\n", name)
				continue
			}
			sb.WriteString(formatChangeStatus(status))
			sb.WriteString("\n")
		}
		return &WorkflowResult{
			StatusMessage: sb.String(),
		}, nil
	}

	status, err := we.manager.GetChangeStatus(changeName)
	if err != nil {
		return nil, err
	}

	return &WorkflowResult{
		StatusMessage: formatChangeStatus(status),
	}, nil
}

func (we *WorkflowEngine) handleContinue(cmd *Command) (*WorkflowResult, error) {
	changeName, err := we.resolveChangeName(cmd.ChangeName)
	if err != nil {
		return &WorkflowResult{UserMessageOverride: err.Error()}, nil
	}

	change, err := we.manager.GetChange(changeName)
	if err != nil {
		return nil, err
	}

	// Find ready artifacts
	statuses := make(map[string]ArtifactStatus)
	for id, art := range change.Artifacts {
		statuses[id] = art.Status
	}
	schema := GetSchema(change.Schema)
	ready := GetReadyArtifacts(schema, statuses)

	if len(ready) == 0 {
		return &WorkflowResult{
			StatusMessage: fmt.Sprintf("No artifacts ready to create for %s. All artifacts are either created or have unmet dependencies.", changeName),
		}, nil
	}

	// Create the first ready artifact
	artifactID := ready[0]
	instructions := we.buildContinueInstructions(changeName, change, artifactID)

	return &WorkflowResult{
		SystemPromptAddition: we.buildOpenSpecSystemContext(nil),
		UserMessageOverride:  instructions,
		Change:               change,
	}, nil
}

func (we *WorkflowEngine) handleFastForward(cmd *Command) (*WorkflowResult, error) {
	changeName, err := we.resolveChangeName(cmd.ChangeName)
	if err != nil {
		return &WorkflowResult{UserMessageOverride: err.Error()}, nil
	}

	change, err := we.manager.GetChange(changeName)
	if err != nil {
		return nil, err
	}

	projConfig, _ := we.manager.ReadProjectConfig()
	instructions := we.buildProposeInstructions(changeName, change, projConfig, cmd)

	return &WorkflowResult{
		SystemPromptAddition: we.buildOpenSpecSystemContext(projConfig),
		UserMessageOverride:  instructions,
		Change:               change,
	}, nil
}

func (we *WorkflowEngine) handleVerify(cmd *Command) (*WorkflowResult, error) {
	changeName, err := we.resolveChangeName(cmd.ChangeName)
	if err != nil {
		return &WorkflowResult{UserMessageOverride: err.Error()}, nil
	}

	change, err := we.manager.GetChange(changeName)
	if err != nil {
		return nil, err
	}

	instructions := we.buildVerifyInstructions(changeName, change)

	return &WorkflowResult{
		SystemPromptAddition: we.buildOpenSpecSystemContext(nil),
		UserMessageOverride:  instructions,
		Change:               change,
	}, nil
}

func (we *WorkflowEngine) handleArchive(cmd *Command) (*WorkflowResult, error) {
	changeName, err := we.resolveChangeName(cmd.ChangeName)
	if err != nil {
		return &WorkflowResult{UserMessageOverride: err.Error()}, nil
	}

	if err := we.manager.ArchiveChange(changeName); err != nil {
		return nil, fmt.Errorf("failed to archive change: %w", err)
	}

	return &WorkflowResult{
		StatusMessage: fmt.Sprintf("Archived change %q successfully.", changeName),
	}, nil
}

func (we *WorkflowEngine) handleExplore(cmd *Command) (*WorkflowResult, error) {
	topic := strings.Join(cmd.Args, " ")
	if topic == "" && cmd.ChangeName != "" {
		topic = cmd.ChangeName
	}

	instructions := "The user wants to explore ideas before committing to a change. " +
		"Help them think through the topic, investigate the codebase, compare options, " +
		"and clarify requirements. No artifacts need to be created yet.\n\n"
	if topic != "" {
		instructions += fmt.Sprintf("Topic to explore: %s\n", topic)
	}

	return &WorkflowResult{
		UserMessageOverride: instructions,
	}, nil
}

func (we *WorkflowEngine) handleSync(cmd *Command) (*WorkflowResult, error) {
	changeName, err := we.resolveChangeName(cmd.ChangeName)
	if err != nil {
		return &WorkflowResult{UserMessageOverride: err.Error()}, nil
	}

	instructions := fmt.Sprintf("The user wants to sync delta specs from change %q into the main openspec/specs/ directory. "+
		"Read the delta specs from openspec/changes/%s/specs/ and merge ADDED/MODIFIED/REMOVED sections into the corresponding main spec files. "+
		"Preserve existing content not mentioned in the delta.", changeName, changeName)

	return &WorkflowResult{
		UserMessageOverride: instructions,
	}, nil
}

// resolveChangeName resolves a change name, auto-selecting if only one active.
func (we *WorkflowEngine) resolveChangeName(name string) (string, error) {
	if name != "" {
		return name, nil
	}
	changes, err := we.manager.ListChanges()
	if err != nil {
		return "", err
	}
	if len(changes) == 0 {
		return "", fmt.Errorf("no active OpenSpec changes found; use /opsx:propose to create one")
	}
	if len(changes) == 1 {
		return changes[0], nil
	}
	return "", fmt.Errorf("multiple active changes found: %s\nPlease specify which one", strings.Join(changes, ", "))
}

// buildOpenSpecSystemContext builds system prompt context for OpenSpec mode.
func (we *WorkflowEngine) buildOpenSpecSystemContext(projConfig *ProjectConfig) string {
	var sb strings.Builder
	sb.WriteString("\n## OpenSpec Context\n\n")
	sb.WriteString("You are working in a project using OpenSpec (spec-driven development).\n")
	sb.WriteString("OpenSpec organizes changes into structured artifacts: proposal → specs → design → tasks → implementation.\n\n")

	if projConfig != nil && projConfig.Context != "" {
		sb.WriteString("### Project Context\n")
		sb.WriteString(projConfig.Context)
		sb.WriteString("\n\n")
	}

	// Add active changes summary
	changes, err := we.manager.ListChanges()
	if err == nil && len(changes) > 0 {
		sb.WriteString("### Active Changes\n")
		for _, name := range changes {
			status, err := we.manager.GetChangeStatus(name)
			if err != nil {
				continue
			}
			sb.WriteString(formatChangeStatusCompact(status))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildApplySystemContext builds system prompt context for /opsx:apply.
func (we *WorkflowEngine) buildApplySystemContext(changeName string, _ *Change) string {
	var sb strings.Builder
	sb.WriteString("\n## OpenSpec Implementation Mode\n\n")
	fmt.Fprintf(&sb, "You are implementing tasks for OpenSpec change: %s\n\n", changeName)

	// Read and include design context
	design, err := we.manager.ReadArtifact(changeName, "design")
	if err == nil && design != "" {
		sb.WriteString("### Technical Design\n")
		// Truncate if too long
		if len(design) > 2000 {
			sb.WriteString(design[:2000])
			sb.WriteString("\n...(truncated)\n")
		} else {
			sb.WriteString(design)
		}
		sb.WriteString("\n")
	}

	// Read and include specs context
	specs, err := we.manager.ReadArtifact(changeName, "specs")
	if err == nil && specs != "" {
		sb.WriteString("### Specifications\n")
		if len(specs) > 2000 {
			sb.WriteString(specs[:2000])
			sb.WriteString("\n...(truncated)\n")
		} else {
			sb.WriteString(specs)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildProposeInstructions builds LLM instructions for /opsx:propose.
func (we *WorkflowEngine) buildProposeInstructions(changeName string, change *Change, projConfig *ProjectConfig, cmd *Command) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "The user has requested /opsx:propose for change '%s'.\n\n", changeName)
	sb.WriteString("Generate ALL planning artifacts in dependency order. For each artifact, write the content to the appropriate file:\n\n")

	schema := GetSchema(change.Schema)
	order := GetArtifactOrder(schema)

	for _, id := range order {
		art := change.Artifacts[id]
		if art != nil && art.Status == ArtifactStatusCreated {
			fmt.Fprintf(&sb, "- %s: ALREADY EXISTS (skip)\n", id)
		} else {
			for _, sa := range schema.Artifacts {
				if sa.ID == id {
					fmt.Fprintf(&sb, "- %s → write to: openspec/changes/%s/%s\n", id, changeName, sa.Generates)
					break
				}
			}
		}
	}

	sb.WriteString("\n### Artifact Guidelines:\n\n")
	sb.WriteString("**proposal.md**: Capture intent (what problem are you solving?), scope (in/out of bounds), and approach (high-level strategy).\n\n")
	sb.WriteString("**specs/ (delta specs)**: Define ADDED/MODIFIED/REMOVED requirements with Given/When/Then scenarios. Use RFC 2119 keywords (MUST/SHALL/SHOULD/MAY).\n\n")
	sb.WriteString("**design.md**: Technical approach, architecture decisions, data flow, file changes.\n\n")
	sb.WriteString("**tasks.md**: Implementation checklist with hierarchical numbering (1.1, 1.2, etc.) using markdown checkboxes.\n\n")

	if projConfig != nil {
		if projConfig.Context != "" {
			sb.WriteString("### Project Context\n")
			sb.WriteString(projConfig.Context)
			sb.WriteString("\n\n")
		}
		for artID, rules := range projConfig.Rules {
			fmt.Fprintf(&sb, "### Rules for %s\n", artID)
			for _, rule := range rules {
				fmt.Fprintf(&sb, "- %s\n", rule)
			}
			sb.WriteString("\n")
		}
	}

	// Include additional description from arguments
	if len(cmd.Args) > 0 {
		// Drop the leading change-name argument when it matches changeName
		descArgs := cmd.Args
		if len(descArgs) > 0 && descArgs[0] == changeName {
			descArgs = descArgs[1:]
		}
		desc := strings.Join(descArgs, " ")
		if desc != "" && desc != changeName {
			fmt.Fprintf(&sb, "### User Description\n%s\n", desc)
		}
	}

	return sb.String()
}

// buildApplyInstructions builds LLM instructions for /opsx:apply.
func (we *WorkflowEngine) buildApplyInstructions(changeName string, _ *Change, allTasks []Task, incomplete []Task) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Implement the tasks for OpenSpec change '%s'.\n\n", changeName)
	fmt.Fprintf(&sb, "Progress: %d/%d tasks completed.\n\n", len(allTasks)-len(incomplete), len(allTasks))

	sb.WriteString("### Remaining Tasks:\n")
	for _, t := range incomplete {
		fmt.Fprintf(&sb, "- [ ] %s %s", t.ID, t.Description)
		if t.GroupName != "" {
			fmt.Fprintf(&sb, " (group: %s)", t.GroupName)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n### Instructions:\n")
	sb.WriteString("1. Work through each task sequentially\n")
	sb.WriteString("2. After completing each task, update the checkbox in tasks.md: [ ] → [x]\n")
	sb.WriteString("3. Use read_file, write_file, edit_file, and run_shell_command as needed\n")
	sb.WriteString("4. Follow the design.md and specs for implementation details\n")
	sb.WriteString("5. Run tests after implementation to verify correctness\n")
	sb.WriteString("6. Call task_done when all tasks are complete\n")

	return sb.String()
}

// buildContinueInstructions builds LLM instructions for /opsx:continue.
func (we *WorkflowEngine) buildContinueInstructions(changeName string, change *Change, artifactID string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Create the next artifact for OpenSpec change '%s'.\n\n", changeName)
	fmt.Fprintf(&sb, "Artifact to create: %s\n\n", artifactID)

	// Read dependency artifacts as context
	schema := GetSchema(change.Schema)
	for _, sa := range schema.Artifacts {
		if sa.ID == artifactID {
			for _, dep := range sa.Requires {
				content, err := we.manager.ReadArtifact(changeName, dep)
				if err == nil && content != "" {
					fmt.Fprintf(&sb, "### %s (dependency context)\n%s\n\n", dep, content)
				}
			}
			fmt.Fprintf(&sb, "Write the %s artifact to: openspec/changes/%s/%s\n", artifactID, changeName, sa.Generates)
			break
		}
	}

	return sb.String()
}

// buildVerifyInstructions builds LLM instructions for /opsx:verify.
func (we *WorkflowEngine) buildVerifyInstructions(changeName string, _ *Change) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Verify the implementation of OpenSpec change '%s'.\n\n", changeName)
	sb.WriteString("Check three dimensions:\n\n")
	sb.WriteString("1. **Completeness**: All tasks done? All requirements implemented? Scenarios covered?\n")
	sb.WriteString("2. **Correctness**: Implementation matches spec intent? Edge cases handled?\n")
	sb.WriteString("3. **Coherence**: Design decisions reflected in code? Patterns consistent?\n\n")
	sb.WriteString("Read the specs, design, and tasks artifacts, then search the codebase to verify each requirement.\n")
	sb.WriteString("Report issues as CRITICAL, WARNING, or SUGGESTION.\n")

	return sb.String()
}

// formatChangeStatus formats a change status for display.
func formatChangeStatus(status *ChangeStatus) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Change: %s\n", status.Name)

	artifactOrder := []string{"proposal", "specs", "design", "tasks"}
	for _, id := range artifactOrder {
		s, ok := status.ArtifactStatuses[id]
		if !ok {
			continue
		}
		icon := "○" // pending
		switch s {
		case ArtifactStatusCreated:
			icon = "✓"
		case ArtifactStatusReady:
			icon = "◆"
		case ArtifactStatusOutdated:
			icon = "⚠"
		}
		fmt.Fprintf(&sb, "  %s %s\n", icon, id)
	}

	if status.TasksTotal > 0 {
		fmt.Fprintf(&sb, "  Tasks: %d/%d completed\n", status.TasksCompleted, status.TasksTotal)
	}

	if len(status.ReadyArtifacts) > 0 {
		fmt.Fprintf(&sb, "  Ready to create: %s\n", strings.Join(status.ReadyArtifacts, ", "))
	}

	return sb.String()
}

// formatChangeStatusCompact formats a change status as a single line.
func formatChangeStatusCompact(status *ChangeStatus) string {
	var parts []string
	artifactOrder := []string{"proposal", "specs", "design", "tasks"}
	for _, id := range artifactOrder {
		s, ok := status.ArtifactStatuses[id]
		if !ok {
			continue
		}
		icon := "○"
		switch s {
		case ArtifactStatusCreated:
			icon = "✓"
		case ArtifactStatusReady:
			icon = "◆"
		}
		parts = append(parts, fmt.Sprintf("%s %s", icon, id))
	}
	line := fmt.Sprintf("- %s: %s", status.Name, strings.Join(parts, " | "))
	if status.TasksTotal > 0 {
		line += fmt.Sprintf(" (%d/%d tasks)", status.TasksCompleted, status.TasksTotal)
	}
	return line + "\n"
}

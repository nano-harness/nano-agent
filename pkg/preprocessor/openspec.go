package preprocessor

import (
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/openspec"
)

// OpenSpecOptions configures OpenSpec command preprocessing without coupling
// callers to the global application config.
type OpenSpecOptions struct {
	Enabled         bool
	RootDir         string
	WorkingDir      string
	DefaultSchema   string
	MaxArtifactSize int64
}

// OpenSpecResult is the behavior-preserving output of preprocessing an
// /opsx: command.
type OpenSpecResult struct {
	Handled              bool
	CommandType          string
	ChangeName           string
	UserInput            string
	SystemPromptAddition string
	Err                  error
}

// ProcessOpenSpecCommand detects /opsx: commands and returns the input/system
// prompt changes that should be applied to the turn.
func ProcessOpenSpecCommand(input string, opts OpenSpecOptions) OpenSpecResult {
	if !opts.Enabled {
		return OpenSpecResult{}
	}

	cmd := openspec.ParseCommand(input)
	if cmd == nil {
		return OpenSpecResult{}
	}

	rootDir := opts.RootDir
	if rootDir == "" {
		rootDir = "openspec"
	}

	am := openspec.NewArtifactManager(rootDir, opts.WorkingDir)
	if opts.MaxArtifactSize > 0 {
		am.SetMaxArtifactSize(opts.MaxArtifactSize)
	}
	engine := openspec.NewWorkflowEngine(am, opts.DefaultSchema)

	result, err := engine.HandleCommand(cmd)
	if err != nil {
		return OpenSpecResult{
			Handled:     true,
			CommandType: string(cmd.Type),
			ChangeName:  cmd.ChangeName,
			UserInput:   fmt.Sprintf("The user ran the OpenSpec command '%s' but it failed: %v\nHelp them resolve the issue.", cmd.RawInput, err),
			Err:         err,
		}
	}

	userInput := input
	if result.StatusMessage != "" && result.UserMessageOverride == "" {
		userInput = result.StatusMessage
	} else if result.UserMessageOverride != "" {
		userInput = result.UserMessageOverride
	}

	return OpenSpecResult{
		Handled:              true,
		CommandType:          string(cmd.Type),
		ChangeName:           cmd.ChangeName,
		UserInput:            userInput,
		SystemPromptAddition: result.SystemPromptAddition,
	}
}

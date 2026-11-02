package openspec

import (
	"strings"
)

// ParseCommand parses user input to detect /opsx: slash commands.
// Returns nil if the input is not an /opsx: command.
func ParseCommand(input string) *Command {
	trimmed := strings.TrimSpace(input)

	// Check for /opsx: prefix (case-insensitive)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "/opsx:") {
		return nil
	}

	// Extract the part after "/opsx:"
	rest := strings.TrimSpace(trimmed[len("/opsx:"):])

	// Split into command and arguments
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return nil
	}

	cmdStr := strings.ToLower(parts[0])
	args := parts[1:]

	cmd := &Command{
		RawInput: trimmed,
		Args:     args,
	}

	// Map command string to CommandType
	switch cmdStr {
	case "propose":
		cmd.Type = CommandPropose
	case "explore":
		cmd.Type = CommandExplore
	case "new":
		cmd.Type = CommandNew
	case "continue":
		cmd.Type = CommandContinue
	case "ff":
		cmd.Type = CommandFastForward
	case "apply":
		cmd.Type = CommandApply
	case "verify":
		cmd.Type = CommandVerify
	case "sync":
		cmd.Type = CommandSync
	case "archive":
		cmd.Type = CommandArchive
	case "bulk-archive":
		cmd.Type = CommandBulkArchive
	case "status":
		cmd.Type = CommandStatus
	default:
		return nil // Unknown command
	}

	// Extract change name from first argument if present
	if len(args) > 0 {
		// Skip flags (args starting with --)
		for _, arg := range args {
			if !strings.HasPrefix(arg, "--") {
				cmd.ChangeName = arg
				break
			}
		}
	}

	return cmd
}

// IsOpenSpecCommand checks if user input starts with /opsx: prefix.
func IsOpenSpecCommand(input string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(input)), "/opsx:")
}

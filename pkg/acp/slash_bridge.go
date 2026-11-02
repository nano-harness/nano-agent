package acp

import (
	"strings"

	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/slash"
)

// advertiseSlashCommands sends available_commands_update to the client
func (s *Server) advertiseSlashCommands(sessionID string, cwd string) {
	commands := s.buildAvailableCommands(cwd)

	// Send session/update notification with available_commands_update
	update := map[string]interface{}{
		"sessionId": sessionID,
		"update": map[string]interface{}{
			"sessionUpdate":     "available_commands_update",
			"availableCommands": commands,
		},
	}

	err := s.transport.SendNotification("session/update", update)
	if err != nil {
		logger.Warnf("ACP: Failed to send available_commands_update: %v", err)
	} else {
		logger.Debugf("ACP: Advertised %d slash commands to session %s", len(commands), sessionID)
	}
}

// buildAvailableCommands constructs the list of available slash commands
func (s *Server) buildAvailableCommands(cwd string) []AvailableCommand {
	registry := slash.NewRegistry(cwd)
	slashCommands := registry.All()

	commands := make([]AvailableCommand, 0, len(slashCommands))
	for _, cmd := range slashCommands {
		availCmd := AvailableCommand{
			Name:        cmd.Name,
			Description: cmd.Description,
		}

		// Add input hint based on usage
		if cmd.Usage != "" && cmd.Usage != "/"+cmd.Name {
			// Extract hint from usage (e.g., "/model use <model>" -> "model")
			usage := strings.TrimPrefix(cmd.Usage, "/"+cmd.Name)
			usage = strings.TrimSpace(usage)
			if usage != "" {
				availCmd.Input = &CommandInputConfig{
					Hint: usage,
				}
			}
		}

		commands = append(commands, availCmd)
	}

	return commands
}

// handleSlashCommand processes a slash command input
func (s *Server) handleSlashCommand(session *ACPSession, input string, bridge *EventBridge) bool {
	// Create dispatcher for this session
	dispatcher := slash.NewLocalDispatcher("", session.CWD)

	// Dispatch the command
	result := dispatcher.Dispatch(input)

	if !result.Handled && !result.ShouldSubmit {
		// Not a locally-handled slash command
		return false
	}

	if result.Handled {
		// Command was handled locally, send the result as a message event
		if result.Message != "" {
			// Send the message as an agent_message_chunk
			update := map[string]interface{}{
				"sessionId": session.ACPSessionID,
				"update": map[string]interface{}{
					"sessionUpdate": "agent_message_chunk",
					"content": map[string]interface{}{
						"type": "text",
						"text": result.Message,
					},
				},
			}
			_ = s.transport.SendNotification("session/update", update)
		}
		return true
	}

	if result.ShouldSubmit {
		// Command should be submitted to the agent
		// For now, we don't support this in ACP - the command was rewritten
		// but we can't automatically submit it. Log a warning.
		logger.Warnf("ACP: Slash command requires submission but auto-submit not supported: %s", input)
		update := map[string]interface{}{
			"sessionId": session.ACPSessionID,
			"update": map[string]interface{}{
				"sessionUpdate": "agent_message_chunk",
				"content": map[string]interface{}{
					"type": "text",
					"text": "⚠️ This command requires agent processing, which is not yet supported in ACP mode.",
				},
			},
		}
		_ = s.transport.SendNotification("session/update", update)
		return true
	}

	return false
}

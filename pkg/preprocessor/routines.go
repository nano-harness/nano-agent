// Package preprocessor contains input enrichment helpers that run before the
// turn executor sends messages to the model.
package preprocessor

import (
	"fmt"
	"strings"
)

// RewriteRoutinesCommand converts /routines slash commands into prompts that
// instruct the model to call the routine management tool.
func RewriteRoutinesCommand(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/routines") {
		return input, false
	}

	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "/routines"))
	sub, args := splitFirstWord(rest)

	switch sub {
	case "", "list":
		return "Please list all routine tasks by calling manage_routine with action='list'.", true
	case "add":
		if args == "" {
			return "Usage: /routines add <description> e.g. '/routines add every 5 minutes run go test'", true
		}
		return fmt.Sprintf(
			"The user wants to add a recurring routine: %q\n"+
				"Parse the schedule (cron expression or natural language like 'every 5 minutes', "+
				"'daily at 9am') and the command, then call manage_routine with action='create'. "+
				"After confirmation, report the resulting task ID and schedule.",
			args,
		), true
	case "remove":
		if args == "" {
			return "Usage: /routines remove <id>", true
		}
		return fmt.Sprintf(
			"Please remove the routine task with ID %q by calling manage_routine with action='delete' and task_id=%q.",
			args, args,
		), true
	case "status":
		if args == "" {
			return "Please call manage_routine with action='list' and report all tasks with their schedules and last-run information.", true
		}
		return fmt.Sprintf(
			"Please show the status of routine %q by calling manage_routine action='list' and finding the matching task ID.",
			args,
		), true
	case "pause":
		if args == "" {
			return "Usage: /routines pause <id>", true
		}
		return fmt.Sprintf(
			"Please pause routine task %q by calling manage_routine with action='pause' and task_id=%q.",
			args, args,
		), true
	case "resume":
		if args == "" {
			return "Usage: /routines resume <id>", true
		}
		return fmt.Sprintf(
			"Please resume routine task %q by calling manage_routine with action='resume' and task_id=%q.",
			args, args,
		), true
	case "run":
		if args == "" {
			return "Usage: /routines run <id>", true
		}
		return fmt.Sprintf(
			"Please immediately run routine task %q by calling manage_routine with action='run' and task_id=%q.",
			args, args,
		), true
	default:
		return fmt.Sprintf(
			"Unknown /routines subcommand: %q\nValid: list, add, remove, status, pause, resume, run",
			sub,
		), true
	}
}

// splitFirstWord splits "list abc def" into ("list", "abc def").
func splitFirstWord(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	idx := strings.IndexAny(s, " \t")
	if idx < 0 {
		return s, ""
	}
	return s[:idx], strings.TrimSpace(s[idx+1:])
}

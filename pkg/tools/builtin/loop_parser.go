package builtin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	cronlib "github.com/robfig/cron/v3"
)

// ParseNaturalSchedule converts a natural language schedule expression or a
// standard cron expression into a 6-field cron expression compatible with
// the scheduler's WithSeconds() mode (format: second minute hour dom month dow).
// Standard cron expressions (containing digits and *) are normalised to 6 fields.
//
// Supported natural language patterns:
//
//	"every <n> minutes"      → "0 */N * * * *"
//	"every <n> hours"        → "0 0 */N * * *"
//	"every minute"           → "0 * * * * *"
//	"every hour"             → "0 0 * * * *"
//	"every day"/"daily"      → "0 0 0 * * *"
//	"daily at <H>am/pm"      → "0 0 H * * *"
//	"weekdays at <H>am/pm"   → "0 0 H * * 1-5"
//	"weekends at <H>am/pm"   → "0 0 H * * 0,6"
//	"every <weekday> at <H>" → "0 0 H * * W"
func ParseNaturalSchedule(input string) (string, error) {
	s := strings.TrimSpace(strings.ToLower(input))

	// If the input already looks like a cron expression (5 or 6 fields of
	// digits/*/- separated by spaces), validate and normalise to 6 fields.
	if isCronLike(s) {
		return validateCron(s)
	}

	// "every minute"
	if s == "every minute" || s == "every 1 minute" || s == "every 1 minutes" {
		return "0 * * * * *", nil
	}

	// "every hour"
	if s == "every hour" || s == "every 1 hour" || s == "every 1 hours" {
		return "0 0 * * * *", nil
	}

	// "every day" / "daily"
	if s == "every day" || s == "daily" {
		return "0 0 0 * * *", nil
	}

	// "every <n> minutes"
	if m := regexp.MustCompile(`^every (\d+) minutes?$`).FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n <= 0 || n > 59 {
			return "", fmt.Errorf("interval %d minutes is out of range (1-59)", n)
		}
		return fmt.Sprintf("0 */%d * * * *", n), nil
	}

	// "every <n> hours"
	if m := regexp.MustCompile(`^every (\d+) hours?$`).FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n <= 0 || n > 23 {
			return "", fmt.Errorf("interval %d hours is out of range (1-23)", n)
		}
		return fmt.Sprintf("0 0 */%d * * *", n), nil
	}

	// "daily at <time>" or "every day at <time>"
	if m := regexp.MustCompile(`^(?:daily|every day) at (.+)$`).FindStringSubmatch(s); m != nil {
		h, err := parseHour(m[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("0 0 %d * * *", h), nil
	}

	// "weekdays at <time>"
	if m := regexp.MustCompile(`^weekdays at (.+)$`).FindStringSubmatch(s); m != nil {
		h, err := parseHour(m[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("0 0 %d * * 1-5", h), nil
	}

	// "weekends at <time>"
	if m := regexp.MustCompile(`^weekends at (.+)$`).FindStringSubmatch(s); m != nil {
		h, err := parseHour(m[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("0 0 %d * * 0,6", h), nil
	}

	// "every <weekday> at <time>"
	weekdayRe := regexp.MustCompile(`^every (monday|tuesday|wednesday|thursday|friday|saturday|sunday) at (.+)$`)
	if m := weekdayRe.FindStringSubmatch(s); m != nil {
		dow, err := weekdayNumber(m[1])
		if err != nil {
			return "", err
		}
		h, err := parseHour(m[2])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("0 0 %d * * %d", h, dow), nil
	}

	return "", fmt.Errorf("unrecognised schedule %q; use a cron expression or natural language like 'every 5 minutes', 'daily at 9am'", input)
}

// LoopCommand represents a parsed /loop slash command.
type LoopCommand struct {
	// Action is "start", "stop", or "list"
	Action string
	// Interval is the human-readable interval string (e.g. "5m", "2h")
	Interval string
	// CronExpr is the derived cron expression (empty for stop/list)
	CronExpr string
	// Command is the text to execute on each tick
	Command string
	// TaskID is the task to stop (action == "stop")
	TaskID string
}

// ParseLoopCommand parses a /loop slash command.
//
// Supported formats:
//
//	/loop <interval> <command>   e.g. "/loop 5m check build"
//	/loop stop <task-id>
//	/loop list
//
// Interval suffixes: s (seconds not supported in cron), m (minutes), h (hours).
func ParseLoopCommand(input string) (*LoopCommand, error) {
	s := strings.TrimPrefix(strings.TrimSpace(input), "/loop")
	s = strings.TrimSpace(s)

	if s == "" || s == "list" {
		return &LoopCommand{Action: "list"}, nil
	}

	parts := strings.Fields(s)
	if len(parts) == 0 {
		return &LoopCommand{Action: "list"}, nil
	}

	if parts[0] == "stop" {
		if len(parts) < 2 {
			return nil, fmt.Errorf("/loop stop requires a task ID")
		}
		return &LoopCommand{Action: "stop", TaskID: parts[1]}, nil
	}

	// Parse interval: first token must be <number><unit>
	interval := parts[0]
	cronExpr, err := intervalToCron(interval)
	if err != nil {
		return nil, fmt.Errorf("invalid interval %q: %w", interval, err)
	}

	command := strings.Join(parts[1:], " ")
	if command == "" {
		return nil, fmt.Errorf("/loop requires a command after the interval")
	}

	return &LoopCommand{
		Action:   "start",
		Interval: interval,
		CronExpr: cronExpr,
		Command:  command,
	}, nil
}

// intervalToCron converts a compact interval string (e.g. "5m", "2h") to a
// 6-field cron expression compatible with the scheduler's WithSeconds() mode.
func intervalToCron(interval string) (string, error) {
	interval = strings.ToLower(strings.TrimSpace(interval))
	if len(interval) < 2 {
		return "", fmt.Errorf("interval must be <number><unit> (e.g. 5m, 2h)")
	}

	unit := interval[len(interval)-1]
	numStr := interval[:len(interval)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return "", fmt.Errorf("invalid number in interval %q", interval)
	}
	if n <= 0 {
		return "", fmt.Errorf("interval must be positive")
	}

	switch unit {
	case 'm':
		if n > 59 {
			return "", fmt.Errorf("minute interval must be 1-59, got %d", n)
		}
		return fmt.Sprintf("0 */%d * * * *", n), nil
	case 'h':
		if n > 23 {
			return "", fmt.Errorf("hour interval must be 1-23, got %d", n)
		}
		return fmt.Sprintf("0 0 */%d * * *", n), nil
	default:
		return "", fmt.Errorf("unsupported unit %q; use m (minutes) or h (hours)", string(unit))
	}
}

// parseHour parses a time string like "9am", "9pm", "14", "9:00am".
func parseHour(s string) (int, error) {
	s = strings.TrimSpace(s)

	isPM := strings.HasSuffix(s, "pm")
	isAM := strings.HasSuffix(s, "am")
	if isPM || isAM {
		s = s[:len(s)-2]
	}

	// Strip optional :00 or :30 (we only support hour-level granularity)
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}

	h, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid hour %q", s)
	}

	if isPM && h != 12 {
		h += 12
	}
	if isAM && h == 12 {
		h = 0
	}
	if h < 0 || h > 23 {
		return 0, fmt.Errorf("hour %d is out of range (0-23)", h)
	}
	return h, nil
}

// weekdayNumber maps weekday name to cron DOW (0=Sunday).
func weekdayNumber(name string) (int, error) {
	days := map[string]int{
		"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3,
		"thursday": 4, "friday": 5, "saturday": 6,
	}
	if n, ok := days[strings.ToLower(name)]; ok {
		return n, nil
	}
	return 0, fmt.Errorf("unknown weekday %q", name)
}

// isCronLike returns true if the string looks like a standard cron expression.
func isCronLike(s string) bool {
	fields := strings.Fields(s)
	if len(fields) != 5 && len(fields) != 6 {
		return false
	}
	cronChar := regexp.MustCompile(`^[\d*/,\-]+$`)
	for _, f := range fields {
		if !cronChar.MatchString(f) {
			return false
		}
	}
	return true
}

// validateCron validates a cron expression and normalises it to 6 fields
// (seconds minute hour dom month dow) for use with WithSeconds() scheduler.
// A 5-field expression is prefixed with "0 " (seconds=0).
func validateCron(s string) (string, error) {
	fields := strings.Fields(s)
	if len(fields) == 5 {
		s = "0 " + s // normalise to 6-field: prepend seconds=0
	}
	// Validate using the same parser configuration as the cron scheduler.
	p := cronlib.NewParser(cronlib.Second | cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow | cronlib.Descriptor)
	if _, err := p.Parse(s); err != nil {
		return "", fmt.Errorf("invalid cron expression: %w", err)
	}
	return s, nil
}

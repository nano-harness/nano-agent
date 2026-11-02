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

	// Validate minimum interval to prevent overly frequent executions
	if err := validateMinimumInterval(s); err != nil {
		return "", err
	}

	return s, nil
}

// validateMinimumInterval checks that a cron expression doesn't run more frequently
// than once per minute to prevent resource exhaustion and DoS attacks.
func validateMinimumInterval(cronExpr string) error {
	fields := strings.Fields(cronExpr)
	if len(fields) != 6 {
		return fmt.Errorf("expected 6-field cron expression, got %d fields", len(fields))
	}

	secondField := fields[0]

	// Check for expressions that run every second or multiple times per minute
	// Examples: "* * * * * *", "*/5 * * * * *", "0,30 * * * * *"

	// If seconds field is "*" or contains "/" or ",", it runs multiple times per minute
	if secondField == "*" {
		return fmt.Errorf("cron expression runs every second (too frequent); minimum interval is 1 minute")
	}

	// Check for second-level intervals like "*/5" or ranges "0-30"
	if strings.Contains(secondField, "/") {
		return fmt.Errorf("cron expression uses second-level intervals (too frequent); minimum interval is 1 minute")
	}

	// Check for multiple second values like "0,15,30,45"
	if strings.Contains(secondField, ",") {
		return fmt.Errorf("cron expression runs multiple times per minute (too frequent); minimum interval is 1 minute")
	}

	// Check for second ranges like "0-30"
	if strings.Contains(secondField, "-") {
		return fmt.Errorf("cron expression uses second ranges (too frequent); minimum interval is 1 minute")
	}

	// Seconds field must be a single fixed value (0-59)
	// This ensures the task runs at most once per minute at a specific second
	if secondField != "0" {
		// Allow other fixed seconds like "15", "30", etc., but validate it's a single number
		if _, err := strconv.Atoi(secondField); err != nil {
			return fmt.Errorf("invalid seconds field %q; must be a single value (0-59)", secondField)
		}
	}

	return nil
}

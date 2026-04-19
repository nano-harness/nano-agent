package builtin

import (
	"testing"
)

func TestParseNaturalSchedule(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"0 * * * * *", "0 * * * * *", false}, // already 6-field
		{"0 9 * * *", "0 0 9 * * *", false},   // 5-field → normalised to 6
		{"every minute", "0 * * * * *", false},
		{"every 1 minute", "0 * * * * *", false},
		{"every hour", "0 0 * * * *", false},
		{"every day", "0 0 0 * * *", false},
		{"daily", "0 0 0 * * *", false},
		{"every 5 minutes", "0 */5 * * * *", false},
		{"every 30 minutes", "0 */30 * * * *", false},
		{"every 2 hours", "0 0 */2 * * *", false},
		{"daily at 9am", "0 0 9 * * *", false},
		{"daily at 2pm", "0 0 14 * * *", false},
		{"every day at 12pm", "0 0 12 * * *", false},
		{"weekdays at 10am", "0 0 10 * * 1-5", false},
		{"weekends at 8am", "0 0 8 * * 0,6", false},
		{"every monday at 9am", "0 0 9 * * 1", false},
		{"every friday at 5pm", "0 0 17 * * 5", false},
		{"invalid input xyz", "", true},
		{"every 60 minutes", "", true},
		{"every 24 hours", "", true},
	}

	for _, tc := range tests {
		got, err := ParseNaturalSchedule(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseNaturalSchedule(%q) expected error, got %q", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseNaturalSchedule(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("ParseNaturalSchedule(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestParseLoopCommand(t *testing.T) {
	tests := []struct {
		input       string
		wantAction  string
		wantCommand string
		wantCron    string
		wantTaskID  string
		wantErr     bool
	}{
		{"/loop list", "list", "", "", "", false},
		{"/loop", "list", "", "", "", false},
		{"/loop stop task-123", "stop", "", "", "task-123", false},
		{"/loop 5m check build", "start", "check build", "0 */5 * * * *", "", false},
		{"/loop 1h run tests", "start", "run tests", "0 0 */1 * * *", "", false},
		{"/loop 2h monitor services", "start", "monitor services", "0 0 */2 * * *", "", false},
		{"/loop stop", "", "", "", "", true},       // missing task ID
		{"/loop 0m command", "", "", "", "", true}, // invalid interval
		{"/loop 5m", "", "", "", "", true},         // missing command
	}

	for _, tc := range tests {
		cmd, err := ParseLoopCommand(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseLoopCommand(%q) expected error, got: %+v", tc.input, cmd)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLoopCommand(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if cmd.Action != tc.wantAction {
			t.Errorf("ParseLoopCommand(%q).Action = %q, want %q", tc.input, cmd.Action, tc.wantAction)
		}
		if cmd.Command != tc.wantCommand {
			t.Errorf("ParseLoopCommand(%q).Command = %q, want %q", tc.input, cmd.Command, tc.wantCommand)
		}
		if tc.wantCron != "" && cmd.CronExpr != tc.wantCron {
			t.Errorf("ParseLoopCommand(%q).CronExpr = %q, want %q", tc.input, cmd.CronExpr, tc.wantCron)
		}
		if cmd.TaskID != tc.wantTaskID {
			t.Errorf("ParseLoopCommand(%q).TaskID = %q, want %q", tc.input, cmd.TaskID, tc.wantTaskID)
		}
	}
}

package openspec

import (
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input string
		want  *Command
	}{
		{
			input: "/opsx:propose add-dark-mode",
			want:  &Command{Type: CommandPropose, ChangeName: "add-dark-mode"},
		},
		{
			input: "/opsx:apply",
			want:  &Command{Type: CommandApply, ChangeName: ""},
		},
		{
			input: "/opsx:apply my-change",
			want:  &Command{Type: CommandApply, ChangeName: "my-change"},
		},
		{
			input: "/opsx:status",
			want:  &Command{Type: CommandStatus, ChangeName: ""},
		},
		{
			input: "/opsx:verify test-feature",
			want:  &Command{Type: CommandVerify, ChangeName: "test-feature"},
		},
		{
			input: "/opsx:archive done-feature",
			want:  &Command{Type: CommandArchive, ChangeName: "done-feature"},
		},
		{
			input: "/opsx:explore how should we handle auth?",
			want:  &Command{Type: CommandExplore, ChangeName: "how"},
		},
		{
			input: "/opsx:new my-feature",
			want:  &Command{Type: CommandNew, ChangeName: "my-feature"},
		},
		{
			input: "/opsx:continue",
			want:  &Command{Type: CommandContinue, ChangeName: ""},
		},
		{
			input: "/opsx:ff my-feature",
			want:  &Command{Type: CommandFastForward, ChangeName: "my-feature"},
		},
		{
			input: "/opsx:sync",
			want:  &Command{Type: CommandSync, ChangeName: ""},
		},
		{
			input: "/opsx:bulk-archive",
			want:  &Command{Type: CommandBulkArchive, ChangeName: ""},
		},
		// Case insensitive
		{
			input: "/OPSX:PROPOSE test",
			want:  &Command{Type: CommandPropose, ChangeName: "test"},
		},
		// With leading whitespace
		{
			input: "  /opsx:status  ",
			want:  &Command{Type: CommandStatus, ChangeName: ""},
		},
		// Not a command
		{
			input: "hello world",
			want:  nil,
		},
		{
			input: "/openspec:proposal test",
			want:  nil,
		},
		// Unknown command
		{
			input: "/opsx:unknown",
			want:  nil,
		},
		// Empty after prefix
		{
			input: "/opsx:",
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := ParseCommand(tc.input)
			if tc.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil command")
			}
			if got.Type != tc.want.Type {
				t.Errorf("type: got %q, want %q", got.Type, tc.want.Type)
			}
			if got.ChangeName != tc.want.ChangeName {
				t.Errorf("changeName: got %q, want %q", got.ChangeName, tc.want.ChangeName)
			}
		})
	}
}

func TestIsOpenSpecCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/opsx:propose test", true},
		{"/opsx:apply", true},
		{"/OPSX:STATUS", true},
		{"  /opsx:verify  ", true},
		{"hello", false},
		{"", false},
		{"/openspec:test", false},
	}

	for _, tc := range tests {
		got := IsOpenSpecCommand(tc.input)
		if got != tc.want {
			t.Errorf("IsOpenSpecCommand(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

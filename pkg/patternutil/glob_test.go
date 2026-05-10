package patternutil

import "testing"

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "anything", true},
		{"run_*", "run_shell_command", true},
		{"*_file", "read_file", true},
		{"read_file", "read_file", true},
		{"write_file", "read_file", false},
	}
	for _, tt := range tests {
		if got := MatchGlob(tt.pattern, tt.value); got != tt.want {
			t.Fatalf("MatchGlob(%q, %q)=%v want %v", tt.pattern, tt.value, got, tt.want)
		}
	}
}

package cli

import (
	"testing"
)

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input string
		want  []int
	}{
		{"v1.2.3", []int{1, 2, 3}},
		{"1.2.3", []int{1, 2, 3}},
		{"v0.0.1", []int{0, 0, 1}},
		{"v1.10.0", []int{1, 10, 0}},
		{"v1.2.3-beta.1", []int{1, 2, 3}},
		{"dev", nil},
		{"abc123", nil},
		{"v1.2", nil},
	}
	for _, tt := range tests {
		got := parseSemver(tt.input)
		if tt.want == nil {
			if got != nil {
				t.Errorf("parseSemver(%q) = %v, want nil", tt.input, got)
			}
			continue
		}
		if got == nil || len(got) != 3 {
			t.Errorf("parseSemver(%q) = nil, want %v", tt.input, tt.want)
			continue
		}
		for i := range 3 {
			if got[i] != tt.want[i] {
				t.Errorf("parseSemver(%q)[%d] = %d, want %d", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.1", "v1.0.0", false},
		{"v1.1.0", "v1.0.9", false},
		// non-semver triggers update
		{"dev", "v1.0.0", true},
		{"v1.0.0", "dev", true},
	}
	for _, tt := range tests {
		got := isNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

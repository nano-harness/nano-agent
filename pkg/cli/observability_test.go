package cli

import (
	"strings"
	"testing"
)

func TestRootIncludesObservabilityCommands(t *testing.T) {
	var names []string
	for _, child := range rootCmd.Commands() {
		names = append(names, child.Name())
	}
	got := strings.Join(names, ",")
	for _, want := range []string{"events", "audit", "doctor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("root commands %q missing %q", got, want)
		}
	}
}

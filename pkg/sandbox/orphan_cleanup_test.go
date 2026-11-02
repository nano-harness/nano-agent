package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestDockerResourceArgs(t *testing.T) {
	cases := []struct {
		name string
		r    ResourceLimits
		want []string
	}{
		{"empty", ResourceLimits{}, nil},
		{"only-cpu", ResourceLimits{CPU: 2.0}, []string{"--cpus", "2"}},
		{"only-memory", ResourceLimits{MemoryMB: 512}, []string{"--memory", "512m"}},
		{"only-pids", ResourceLimits{PIDsLimit: 64}, []string{"--pids-limit", "64"}},
		{"all", ResourceLimits{CPU: 1.5, MemoryMB: 4096, PIDsLimit: 256, Timeout: time.Second},
			[]string{"--memory", "4096m", "--cpus", "1.5", "--pids-limit", "256"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dockerResourceArgs(tc.r)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("dockerResourceArgs = %v, want %v", got, tc.want)
			}
		})
	}
}

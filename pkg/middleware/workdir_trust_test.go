package middleware

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPathWithinWorkdir(t *testing.T) {
	workdir := t.TempDir()
	sub := filepath.Join(workdir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workdir, "outside-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	tests := []struct {
		name      string
		candidate string
		want      bool
	}{
		{"empty", "", true},
		{"relative subdir", "./sub", true},
		{"absolute inside", filepath.Join(sub, "file.go"), true},
		{"workdir itself", workdir, true},
		{"absolute outside", filepath.Join(outside, "x.txt"), false},
		{"relative escape", "../../escape", false},
		{"etc passwd", "/etc/passwd", false},
		{"symlink outside", filepath.Join(workdir, "outside-link", "x.txt"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPathWithinWorkdir(workdir, tt.candidate); got != tt.want {
				t.Fatalf("IsPathWithinWorkdir(%q) = %v, want %v", tt.candidate, got, tt.want)
			}
		})
	}
}

func TestExtractShellPathArgs(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{"grep foo src/a.go", []string{"foo", "src/a.go"}},
		{"grep -rn foo .", []string{"foo", "."}},
		{"ls -la", nil},
		{"rg --files-with-matches foo", []string{"foo"}},
		{"find . -name '*.go'", []string{".", "'*.go'"}},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			pc, err := ParseCommand(tt.command)
			if err != nil {
				t.Fatal(err)
			}
			got := ExtractShellPathArgs(pc.Statements[0])
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

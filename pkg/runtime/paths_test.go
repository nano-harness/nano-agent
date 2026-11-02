package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimePathsUseNanoHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, want := HomeDir(), filepath.Join(home, ".nano"); got != want {
		t.Fatalf("HomeDir() = %q, want %q", got, want)
	}
	if got, want := TeamsDir(), filepath.Join(home, ".nano", "teams"); got != want {
		t.Fatalf("TeamsDir() = %q, want %q", got, want)
	}
	if got, want := TeamDir("alpha"), filepath.Join(home, ".nano", "teams", "alpha"); got != want {
		t.Fatalf("TeamDir() = %q, want %q", got, want)
	}
	if got, want := MailboxDir("alpha"), filepath.Join(home, ".nano", "teams", "alpha", "mailbox"); got != want {
		t.Fatalf("MailboxDir() = %q, want %q", got, want)
	}
}

func TestMigrateLegacyPathsMovesRuntimeState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldTeamDir := filepath.Join(home, ".nano-agent", "teams", "old")
	if err := os.MkdirAll(oldTeamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldTeamDir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateLegacyPaths(); err != nil {
		t.Fatalf("MigrateLegacyPaths() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".nano", "teams", "old", "config.json")); err != nil {
		t.Fatalf("migrated config missing: %v", err)
	}
	if _, err := os.Stat(oldTeamDir); !os.IsNotExist(err) {
		t.Fatalf("legacy team dir still exists or unexpected stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".nano-agent", "README.md")); err != nil {
		t.Fatalf("legacy README missing: %v", err)
	}
}

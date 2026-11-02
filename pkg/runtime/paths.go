package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

const (
	homeDirName   = ".nano"
	legacyDirName = ".nano-agent"
)

var migratedHomes sync.Map

// HomeDir returns the unified nano runtime home directory (~/.nano).
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", homeDirName)
	}
	onceValue, _ := migratedHomes.LoadOrStore(home, &sync.Once{})
	once, ok := onceValue.(*sync.Once)
	if !ok {
		// Defensive: should never happen, but avoid panicking on type mismatch.
		logger.Warnf("runtime: unexpected migratedHomes value type %T", onceValue)
		once = &sync.Once{}
	}
	once.Do(func() {
		if err := MigrateLegacyPaths(); err != nil {
			logger.Warnf("runtime: failed to migrate legacy paths: %v", err)
		}
	})
	dir := filepath.Join(home, homeDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warnf("runtime: failed to create home directory %s: %v", dir, err)
	}
	return dir
}

// TeamsDir returns the root directory for team state.
func TeamsDir() string {
	return filepath.Join(HomeDir(), "teams")
}

// TeamDir returns the directory for a specific team.
func TeamDir(team string) string {
	return filepath.Join(TeamsDir(), team)
}

// MailboxDir returns the mailbox directory for a specific team.
func MailboxDir(team string) string {
	return filepath.Join(TeamDir(team), "mailbox")
}

// SessionsDir returns the daemon runtime session directory.
func SessionsDir() string {
	return filepath.Join(HomeDir(), "sessions")
}

// MigrateLegacyPaths migrates legacy ~/.nano-agent runtime state to ~/.nano.
func MigrateLegacyPaths() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	legacyRoot := filepath.Join(home, legacyDirName)
	newRoot := filepath.Join(home, homeDirName)

	info, err := os.Stat(legacyRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	if err := os.MkdirAll(newRoot, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(legacyRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "README.md" {
			continue
		}
		src := filepath.Join(legacyRoot, entry.Name())
		dst := filepath.Join(newRoot, entry.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("migrate %s to %s: %w", src, dst, err)
		}
	}

	readme := []byte("nano-agent runtime data has moved to ~/.nano/.\n")
	if err := os.WriteFile(filepath.Join(legacyRoot, "README.md"), readme, 0o644); err != nil {
		return err
	}
	return nil
}

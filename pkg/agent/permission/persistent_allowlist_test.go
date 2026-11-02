package permission

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPersistentAllowlistStore_LoadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allowlist.json")

	store := NewPersistentAllowlistStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() on missing file should not error: %v", err)
	}

	rules := store.RulesForWorkdir("/some/dir")
	if len(rules) != 0 {
		t.Errorf("Expected empty rules, got %d rules", len(rules))
	}
}

func TestPersistentAllowlistStore_LoadCorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allowlist.json")

	// Write corrupt JSON
	if err := os.WriteFile(path, []byte("not valid json{{{"), 0o600); err != nil {
		t.Fatalf("Failed to write corrupt file: %v", err)
	}

	store := NewPersistentAllowlistStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() on corrupt file should not error: %v", err)
	}

	rules := store.RulesForWorkdir("/some/dir")
	if len(rules) != 0 {
		t.Errorf("Expected empty rules after corrupt load, got %d rules", len(rules))
	}
}

func TestPersistentAllowlistStore_AddAndRetrieve(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allowlist.json")

	store := NewPersistentAllowlistStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	workdir := tmpDir
	rule1 := "run_shell_command(git *)"
	rule2 := "read_file"

	// Add first rule
	added, err := store.AddRuleForWorkdir(workdir, rule1)
	if err != nil {
		t.Fatalf("AddRuleForWorkdir() failed: %v", err)
	}
	if !added {
		t.Errorf("Expected rule1 to be added")
	}

	// Add second rule
	added, err = store.AddRuleForWorkdir(workdir, rule2)
	if err != nil {
		t.Fatalf("AddRuleForWorkdir() failed: %v", err)
	}
	if !added {
		t.Errorf("Expected rule2 to be added")
	}

	// Retrieve rules
	rules := store.RulesForWorkdir(workdir)
	if len(rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(rules))
	}
	if rules[0] != rule1 || rules[1] != rule2 {
		t.Errorf("Rules mismatch: got %v, want [%s, %s]", rules, rule1, rule2)
	}
}

func TestPersistentAllowlistStore_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allowlist.json")

	store := NewPersistentAllowlistStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	workdir := tmpDir
	rule := "run_shell_command(git *)"

	// Add rule twice
	added, err := store.AddRuleForWorkdir(workdir, rule)
	if err != nil {
		t.Fatalf("First AddRuleForWorkdir() failed: %v", err)
	}
	if !added {
		t.Errorf("Expected first add to succeed")
	}

	added, err = store.AddRuleForWorkdir(workdir, rule)
	if err != nil {
		t.Fatalf("Second AddRuleForWorkdir() failed: %v", err)
	}
	if added {
		t.Errorf("Expected second add to be rejected as duplicate")
	}

	// Should have only one rule
	rules := store.RulesForWorkdir(workdir)
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule after deduplication, got %d", len(rules))
	}
}

func TestPersistentAllowlistStore_IsolationBetweenWorkdirs(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allowlist.json")

	store := NewPersistentAllowlistStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	workdir1 := filepath.Join(tmpDir, "project1")
	workdir2 := filepath.Join(tmpDir, "project2")
	rule1 := "run_shell_command(git *)"
	rule2 := "read_file"

	// Add rule1 to workdir1
	if _, err := store.AddRuleForWorkdir(workdir1, rule1); err != nil {
		t.Fatalf("AddRuleForWorkdir() failed: %v", err)
	}

	// Add rule2 to workdir2
	if _, err := store.AddRuleForWorkdir(workdir2, rule2); err != nil {
		t.Fatalf("AddRuleForWorkdir() failed: %v", err)
	}

	// Verify isolation
	rules1 := store.RulesForWorkdir(workdir1)
	if len(rules1) != 1 || rules1[0] != rule1 {
		t.Errorf("workdir1 rules mismatch: got %v, want [%s]", rules1, rule1)
	}

	rules2 := store.RulesForWorkdir(workdir2)
	if len(rules2) != 1 || rules2[0] != rule2 {
		t.Errorf("workdir2 rules mismatch: got %v, want [%s]", rules2, rule2)
	}
}

func TestPersistentAllowlistStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allowlist.json")

	// Create and populate store
	store1 := NewPersistentAllowlistStore(path)
	if err := store1.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	workdir := tmpDir
	rule := "run_shell_command(git *)"

	if _, err := store1.AddRuleForWorkdir(workdir, rule); err != nil {
		t.Fatalf("AddRuleForWorkdir() failed: %v", err)
	}

	// Create new store instance and load from same file
	store2 := NewPersistentAllowlistStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() on second instance failed: %v", err)
	}

	rules := store2.RulesForWorkdir(workdir)
	if len(rules) != 1 || rules[0] != rule {
		t.Errorf("Rules not persisted correctly: got %v, want [%s]", rules, rule)
	}
}

func TestPersistentAllowlistStore_RemoveRule(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allowlist.json")

	store := NewPersistentAllowlistStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	workdir := tmpDir
	rule1 := "run_shell_command(git *)"
	rule2 := "read_file"

	// Add two rules
	if _, err := store.AddRuleForWorkdir(workdir, rule1); err != nil {
		t.Fatalf("AddRuleForWorkdir() failed: %v", err)
	}
	if _, err := store.AddRuleForWorkdir(workdir, rule2); err != nil {
		t.Fatalf("AddRuleForWorkdir() failed: %v", err)
	}

	// Remove rule1
	removed, err := store.RemoveRuleForWorkdir(workdir, rule1)
	if err != nil {
		t.Fatalf("RemoveRuleForWorkdir() failed: %v", err)
	}
	if !removed {
		t.Errorf("Expected rule1 to be removed")
	}

	// Verify rule2 remains
	rules := store.RulesForWorkdir(workdir)
	if len(rules) != 1 || rules[0] != rule2 {
		t.Errorf("Rules after removal: got %v, want [%s]", rules, rule2)
	}

	// Try to remove non-existent rule
	removed, err = store.RemoveRuleForWorkdir(workdir, "nonexistent")
	if err != nil {
		t.Fatalf("RemoveRuleForWorkdir() on non-existent rule failed: %v", err)
	}
	if removed {
		t.Errorf("Expected non-existent rule removal to return false")
	}
}

func TestPersistentAllowlistStore_ConcurrentAdds(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allowlist.json")

	store := NewPersistentAllowlistStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	workdir := tmpDir
	const numGoroutines = 10
	const rulesPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()
			for j := 0; j < rulesPerGoroutine; j++ {
				// Use unique rule names to avoid collisions
				rule := fmt.Sprintf("run_shell_command(cmd-%d-%d *)", n, j)
				if _, err := store.AddRuleForWorkdir(workdir, rule); err != nil {
					t.Errorf("Concurrent AddRuleForWorkdir() failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	rules := store.RulesForWorkdir(workdir)
	expectedCount := numGoroutines * rulesPerGoroutine
	if len(rules) != expectedCount {
		t.Errorf("Expected %d rules after concurrent adds, got %d", expectedCount, len(rules))
	}
}

func TestPersistentAllowlistStore_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "allowlist.json")

	store := NewPersistentAllowlistStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	workdir := tmpDir
	rule := "run_shell_command(git *)"

	if _, err := store.AddRuleForWorkdir(workdir, rule); err != nil {
		t.Fatalf("AddRuleForWorkdir() failed: %v", err)
	}

	// Check file permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Failed to stat allowlist file: %v", err)
	}

	mode := info.Mode()
	if mode.Perm() != 0o600 {
		t.Errorf("Expected file permissions 0600, got %o", mode.Perm())
	}
}

func TestDefaultPersistentAllowlistPath(t *testing.T) {
	path, err := DefaultPersistentAllowlistPath()
	if err != nil {
		t.Fatalf("DefaultPersistentAllowlistPath() failed: %v", err)
	}

	if path == "" {
		t.Errorf("Expected non-empty path")
	}

	if !filepath.IsAbs(path) {
		t.Errorf("Expected absolute path, got %s", path)
	}

	if filepath.Base(path) != "allowlist.json" {
		t.Errorf("Expected filename to be allowlist.json, got %s", filepath.Base(path))
	}
}

package config //nolint:revive

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStateStore_LoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := NewStateStore(path)

	// File doesn't exist yet — Load should succeed with empty state
	if err := s.Load(); err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if got := s.GetActiveSkills(); got != nil {
		t.Errorf("expected nil active skills, got %v", got)
	}

	// Set some state and save
	s.SetActiveSkills([]string{"skill-a", "skill-b"})
	s.AddTask(PersistedTask{
		ID:       "task-1",
		CronExpr: "0 * * * *",
		Command:  "run tests",
		Source:   "tui",
	})

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if s.IsDirty() {
		t.Error("IsDirty should be false after Save")
	}

	// Verify the backup was created
	bakPath := path + ".bak"
	// On first save there's no previous file so no .bak, that's ok.

	// Reload from disk
	s2 := NewStateStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load after Save: %v", err)
	}

	skills := s2.GetActiveSkills()
	if len(skills) != 2 {
		t.Errorf("expected 2 skills, got %d: %v", len(skills), skills)
	}

	tasks := s2.GetTasks()
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != "task-1" {
		t.Errorf("expected task ID 'task-1', got %q", tasks[0].ID)
	}

	// Save again to generate .bak
	s2.SetActiveSkills([]string{"skill-c"})
	if err := s2.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if _, err := os.Stat(bakPath); err != nil {
		t.Errorf("expected .bak file to exist after second save: %v", err)
	}
}

func TestStateStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := NewStateStore(path)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	s.SetActiveSkills([]string{"a"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// Ensure no temp file is left behind
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file should not exist after save: %v", err)
	}
}

func TestStateStore_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write corrupt JSON
	if err := os.WriteFile(path, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStateStore(path)
	// Load should NOT return an error — it falls back to empty state
	if err := s.Load(); err != nil {
		t.Errorf("Load on corrupt file should not return error, got: %v", err)
	}
	if got := s.GetActiveSkills(); got != nil {
		t.Errorf("expected nil active skills after corrupt load, got %v", got)
	}
}

func TestStateStore_RemoveTask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := NewStateStore(path)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	s.AddTask(PersistedTask{ID: "t1", CronExpr: "* * * * *", Command: "cmd1"})
	s.AddTask(PersistedTask{ID: "t2", CronExpr: "* * * * *", Command: "cmd2"})

	s.RemoveTask("t1")

	tasks := s.GetTasks()
	if len(tasks) != 1 {
		t.Errorf("expected 1 task after remove, got %d", len(tasks))
	}
	if tasks[0].ID != "t2" {
		t.Errorf("expected remaining task 't2', got %q", tasks[0].ID)
	}
}

func TestStateStore_SetTaskPaused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := NewStateStore(path)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	s.AddTask(PersistedTask{ID: "t1", CronExpr: "* * * * *", Command: "cmd1"})
	s.SetTaskPaused("t1", true)

	tasks := s.GetTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if !tasks[0].Paused {
		t.Fatalf("expected task to be paused")
	}

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s2 := NewStateStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	tasks = s2.GetTasks()
	if len(tasks) != 1 || !tasks[0].Paused {
		t.Fatalf("expected persisted paused task, got %+v", tasks)
	}

	s2.SetTaskPaused("t1", false)
	tasks = s2.GetTasks()
	if len(tasks) != 1 || tasks[0].Paused {
		t.Fatalf("expected resumed task to be unpaused, got %+v", tasks)
	}
}

func TestStateStore_MCPStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := NewStateStore(path)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	s.SetMCPServerStatus("filesystem", "healthy")
	if got := s.GetMCPServerStatus("filesystem"); got != "healthy" {
		t.Errorf("expected 'healthy', got %q", got)
	}
	if got := s.GetMCPServerStatus("nonexistent"); got != "" {
		t.Errorf("expected '' for unknown server, got %q", got)
	}
}

func TestStateStore_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := NewStateStore(path)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.SetActiveSkills([]string{"skill-x"})
			_ = s.GetActiveSkills()
			s.AddTask(PersistedTask{ID: "tx", CronExpr: "* * * * *", Command: "x"})
			_ = s.GetTasks()
		}()
	}
	wg.Wait()
	// If we reach here without a race detector failure, concurrent access is safe.
}

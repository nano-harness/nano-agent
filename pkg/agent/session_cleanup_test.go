package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestIndex creates a sessions index for the given project storage with
// the supplied entries and returns the storage. Each entry's ModifiedAt
// must be specified explicitly so the test can simulate aged sessions.
func writeTestIndex(t *testing.T, dir string, entries []SessionIndexEntry) *ProjectSessionStorage {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	indexPath := filepath.Join(dir, "sessions-index.json")
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(indexPath, data, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return &ProjectSessionStorage{
		projectDir:  dir,
		sessionsDir: filepath.Join(dir, "sessions"),
		indexPath:   indexPath,
	}
}

func TestPreviewCleanupCandidates_AgeBased(t *testing.T) {
	now := time.Now()
	old := now.Add(-MaxSessionAge - time.Hour).Unix()
	fresh := now.Add(-time.Hour).Unix()
	storage := writeTestIndex(t, t.TempDir(), []SessionIndexEntry{
		{ID: "old-1", ModifiedAt: old, CreatedAt: old},
		{ID: "fresh-1", ModifiedAt: fresh, CreatedAt: fresh},
	})

	cands, err := PreviewCleanupCandidates(storage)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(cands) != 1 || cands[0].SessionID != "old-1" || cands[0].Reason != "idle_ttl" {
		t.Errorf("expected 1 idle_ttl candidate for old-1, got %+v", cands)
	}
}

func TestPreviewCleanupCandidates_MaxPerProject(t *testing.T) {
	now := time.Now()
	entries := make([]SessionIndexEntry, 0, MaxSessionsPerProject+5)
	for i := 0; i < MaxSessionsPerProject+5; i++ {
		// Stagger modification times by one minute so ordering is deterministic.
		ts := now.Add(-time.Duration(i) * time.Minute).Unix()
		entries = append(entries, SessionIndexEntry{
			ID:         "s" + string(rune('a'+i%26)) + "-" + string(rune('0'+i/26)),
			ModifiedAt: ts,
			CreatedAt:  ts,
		})
	}
	storage := writeTestIndex(t, t.TempDir(), entries)

	cands, err := PreviewCleanupCandidates(storage)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(cands) != 5 {
		t.Errorf("expected 5 max_per_project candidates, got %d: %+v", len(cands), cands)
	}
	for _, c := range cands {
		if c.Reason != "max_per_project" {
			t.Errorf("expected reason max_per_project, got %q", c.Reason)
		}
	}
}

func TestPreviewCleanupCandidates_PreservesNonAged(t *testing.T) {
	now := time.Now()
	storage := writeTestIndex(t, t.TempDir(), []SessionIndexEntry{
		{ID: "fresh-1", ModifiedAt: now.Add(-time.Hour).Unix(), CreatedAt: now.Unix()},
		{ID: "fresh-2", ModifiedAt: now.Add(-2 * time.Hour).Unix(), CreatedAt: now.Unix()},
	})

	cands, err := PreviewCleanupCandidates(storage)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(cands) != 0 {
		t.Errorf("expected no candidates for fresh sessions, got %+v", cands)
	}
}

func TestPreviewCleanupCandidates_DoesNotMutateStorage(t *testing.T) {
	now := time.Now()
	old := now.Add(-MaxSessionAge - time.Hour).Unix()
	storage := writeTestIndex(t, t.TempDir(), []SessionIndexEntry{
		{ID: "old-1", ModifiedAt: old, CreatedAt: old},
	})
	if _, err := PreviewCleanupCandidates(storage); err != nil {
		t.Fatalf("preview: %v", err)
	}
	// Index file must still exist and contain old-1.
	infos, err := storage.ListSessionInfos()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != "old-1" {
		t.Errorf("preview must not delete sessions; got %+v", infos)
	}
}

func TestCleanupAllProjectsByReason(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectDir := filepath.Join(home, ".nano", "projects", "proj")
	now := time.Now()
	old := now.Add(-MaxSessionAge - time.Hour).Unix()
	fresh := now.Add(-time.Hour).Unix()
	storage := writeTestIndex(t, projectDir, []SessionIndexEntry{
		{ID: "old-1", ModifiedAt: old, CreatedAt: old},
		{ID: "fresh-1", ModifiedAt: fresh, CreatedAt: fresh},
	})
	if err := os.WriteFile(filepath.Join(storage.sessionsDir, "old-1.jsonl"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storage.sessionsDir, "fresh-1.jsonl"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CleanupAllProjectsByReason("idle_ttl"); err != nil {
		t.Fatalf("cleanup by reason: %v", err)
	}
	infos, err := storage.ListSessionInfos()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != "fresh-1" {
		t.Fatalf("expected only fresh-1 to remain, got %+v", infos)
	}
	if _, err := os.Stat(filepath.Join(storage.sessionsDir, "old-1.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("expected old session file removed, stat err=%v", err)
	}
}

package checkpoint

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotRestoreFileCopy(t *testing.T) {
	work := t.TempDir()
	backups := t.TempDir()
	target := filepath.Join(work, "hello.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, err := NewFSManager(Options{
		WorkingDir:   work,
		BackupRoot:   backups,
		GitDisable:   true,
		RetentionAge: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	cp, err := mgr.Snapshot("before edit", "write_file")
	if err != nil {
		t.Fatal(err)
	}
	if cp.Strategy != StrategyFileCopy {
		t.Fatalf("expected file-copy strategy, got %s", cp.Strategy)
	}

	// Modify the file.
	if err := os.WriteFile(target, []byte("oops"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Restore(cp.ID); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Fatalf("expected restore to recover content, got %q", got)
	}

	// List
	cps, err := mgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(cps) != 1 || cps[0].ID != cp.ID {
		t.Fatalf("unexpected list: %#v", cps)
	}

	// Delete
	if err := mgr.Delete(cp.ID); err != nil {
		t.Fatal(err)
	}
	cps, _ = mgr.List()
	if len(cps) != 0 {
		t.Fatalf("expected empty list after delete, got %d", len(cps))
	}
}

func TestRetentionTrimsByCount(t *testing.T) {
	work := t.TempDir()
	backups := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr, err := NewFSManager(Options{
		WorkingDir: work, BackupRoot: backups, GitDisable: true, MaxCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := mgr.Snapshot("snap", ""); err != nil {
			t.Fatal(err)
		}
	}
	cps, _ := mgr.List()
	if len(cps) != 2 {
		t.Fatalf("expected 2 retained checkpoints, got %d", len(cps))
	}
}

func TestRestoreUnknownIDReturnsErrNotFound(t *testing.T) {
	work := t.TempDir()
	backups := t.TempDir()
	mgr, _ := NewFSManager(Options{WorkingDir: work, BackupRoot: backups, GitDisable: true})
	if err := mgr.Restore("missing"); err == nil {
		t.Fatal("expected error")
	} else if err.Error() == "" {
		t.Fatal("expected non-empty error")
	}
}

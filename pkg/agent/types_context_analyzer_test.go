package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAnalyzeContext_GoProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wc, err := NewContextAnalyzer().AnalyzeContext(dir)
	if err != nil {
		t.Fatalf("AnalyzeContext returned error: %v", err)
	}
	if wc.ProjectType != "go" {
		t.Fatalf("expected go project, got %q", wc.ProjectType)
	}
	if len(wc.Files) != 0 || len(wc.RecentFiles) != 0 {
		t.Fatalf("expected default context to skip file inventory, got files=%v recent=%v", wc.Files, wc.RecentFiles)
	}
}

func TestAnalyzeContextWithFiles_PopulatesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wc, err := NewContextAnalyzer().AnalyzeContextWithFiles(context.Background(), dir, 10)
	if err != nil {
		t.Fatalf("AnalyzeContextWithFiles returned error: %v", err)
	}
	if len(wc.Files) == 0 {
		t.Fatal("expected file inventory")
	}
	if !containsString(wc.Files, "main.go") {
		t.Fatalf("expected main.go in file inventory, got %v", wc.Files)
	}
}

func TestAnalyzeContext_GitStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runTestGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wc, err := NewContextAnalyzer().AnalyzeContext(dir)
	if err != nil {
		t.Fatalf("AnalyzeContext returned error: %v", err)
	}
	if wc.GitStatus == nil {
		t.Fatal("expected git status")
	}
	if wc.GitStatus.Branch == "" {
		t.Fatal("expected git branch")
	}
	if !wc.GitStatus.HasChanges || wc.GitStatus.ModifiedCount == 0 {
		t.Fatalf("expected changes, got %#v", wc.GitStatus)
	}
}

func TestAnalyzeContext_Fallback(t *testing.T) {
	dir := t.TempDir()
	wc, err := NewContextAnalyzer().AnalyzeContext(dir)
	if err != nil {
		t.Fatalf("AnalyzeContext should degrade without error, got %v", err)
	}
	if wc == nil {
		t.Fatal("expected partial context")
	}
	if wc.WorkingDirectory == "" {
		t.Fatal("expected working directory")
	}
	if wc.GitStatus != nil {
		t.Fatalf("expected no git status outside git repo, got %#v", wc.GitStatus)
	}
}

func runTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestScanWorkspaceFiles_GitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runTestGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, dir, "add", ".")

	files, err := scanWorkspaceFiles(context.Background(), dir, 20)
	if err != nil {
		t.Fatalf("scanWorkspaceFiles returned error: %v", err)
	}
	if !containsString(files, ".gitignore") || !containsString(files, "main.go") {
		t.Fatalf("expected tracked files, got %v", files)
	}
	if containsString(files, "ignored.txt") {
		t.Fatalf("expected ignored file to be omitted, got %v", files)
	}
}

func TestScanWorkspaceFiles_NonGitFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := scanWorkspaceFiles(context.Background(), dir, 20)
	if err != nil {
		t.Fatalf("scanWorkspaceFiles returned error: %v", err)
	}
	if !containsString(files, ".gitignore") || !containsString(files, "main.go") {
		t.Fatalf("expected fallback files, got %v", files)
	}
	if containsString(files, "ignored.txt") {
		t.Fatalf("expected ignored file to be omitted, got %v", files)
	}
}

func TestScanWorkspaceFiles_Timeout(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scanWorkspaceFiles(ctx, dir, 20)
	if err == nil {
		t.Fatal("expected canceled context error")
	}
}

func TestScanWorkspaceFiles_GitRepoCanceledContext(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runTestGit(t, dir, "init")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := scanWorkspaceFiles(ctx, dir, 20)
	if err == nil {
		t.Fatal("expected canceled context error")
	}
}

func TestScanWorkspaceFiles_MaxFiles(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%c.go", 'a'+i)), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := scanWorkspaceFiles(context.Background(), dir, 3)
	if err != nil {
		t.Fatalf("scanWorkspaceFiles returned error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d: %v", len(files), files)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

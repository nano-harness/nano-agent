package skill

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testSkillContent = `---
name: test-skill
description: "A test skill"
triggers:
  - "test"
auto_invoke: true
priority: 5
---

# Test Skill

These are the test skill instructions.
`

func TestInstaller_LocalFile(t *testing.T) {
	personalDir := t.TempDir()

	// Write a local SKILL.md file
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "SKILL.md")
	if err := os.WriteFile(srcPath, []byte(testSkillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := NewInstaller(personalDir, DefaultMaxSkillSize)
	sk, data, err := inst.Install(context.Background(), srcPath)
	if err != nil {
		t.Fatalf("Install from local file: %v", err)
	}
	if sk.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", sk.Name)
	}
	if len(data) == 0 {
		t.Error("expected non-empty data")
	}

	// Verify file was written
	installedPath := filepath.Join(personalDir, "test-skill", "SKILL.md")
	if _, err := os.Stat(installedPath); err != nil {
		t.Errorf("installed SKILL.md not found: %v", err)
	}
}

func TestInstaller_LocalDir(t *testing.T) {
	personalDir := t.TempDir()

	// Create a local skill directory
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte(testSkillContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "extra.txt"), []byte("extra content"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := NewInstaller(personalDir, DefaultMaxSkillSize)
	sk, _, err := inst.Install(context.Background(), srcDir)
	if err != nil {
		t.Fatalf("Install from local dir: %v", err)
	}
	if sk.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", sk.Name)
	}

	// Verify extra file was also copied
	if _, err := os.Stat(filepath.Join(personalDir, "test-skill", "extra.txt")); err != nil {
		t.Errorf("extra.txt not copied: %v", err)
	}
}

func TestInstaller_LocalZip(t *testing.T) {
	personalDir := t.TempDir()

	// Build a ZIP archive in memory
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(testSkillContent)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	// Write to temp file
	zipPath := filepath.Join(t.TempDir(), "skill.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := NewInstaller(personalDir, DefaultMaxSkillSize)
	sk, _, err := inst.Install(context.Background(), zipPath)
	if err != nil {
		t.Fatalf("Install from local zip: %v", err)
	}
	if sk.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", sk.Name)
	}
}

func TestInstaller_LocalTarGz(t *testing.T) {
	personalDir := t.TempDir()

	// Build a TAR.GZ archive in memory
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte(testSkillContent)
	hdr := &tar.Header{
		Name: "SKILL.md",
		Mode: 0o644,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	// Write to temp file
	tgzPath := filepath.Join(t.TempDir(), "skill.tar.gz")
	if err := os.WriteFile(tgzPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := NewInstaller(personalDir, DefaultMaxSkillSize)
	sk, _, err := inst.Install(context.Background(), tgzPath)
	if err != nil {
		t.Fatalf("Install from tar.gz: %v", err)
	}
	if sk.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", sk.Name)
	}
}

func TestInstaller_HTTPFile(t *testing.T) {
	personalDir := t.TempDir()

	// Start a test HTTP server serving SKILL.md
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(testSkillContent))
	}))
	defer srv.Close()

	inst := NewInstaller(personalDir, DefaultMaxSkillSize)
	sk, _, err := inst.Install(context.Background(), srv.URL+"/SKILL.md")
	if err != nil {
		t.Fatalf("Install from HTTP file: %v", err)
	}
	if sk.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", sk.Name)
	}
}

func TestInstaller_HTTPZip(t *testing.T) {
	personalDir := t.TempDir()

	// Build ZIP in memory
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("SKILL.md")
	_, _ = f.Write([]byte(testSkillContent))
	_ = zw.Close()
	zipBytes := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	inst := NewInstaller(personalDir, DefaultMaxSkillSize)
	sk, _, err := inst.Install(context.Background(), srv.URL+"/skill.zip")
	if err != nil {
		t.Fatalf("Install from HTTP zip: %v", err)
	}
	if sk.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", sk.Name)
	}
}

func TestInstaller_PathTraversalInZip(t *testing.T) {
	personalDir := t.TempDir()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Add a path traversal entry
	f, _ := zw.Create("../evil.sh")
	_, _ = f.Write([]byte("evil"))
	_ = zw.Close()

	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := NewInstaller(personalDir, DefaultMaxSkillSize)
	_, _, err := inst.Install(context.Background(), zipPath)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestInstaller_EmptySource(t *testing.T) {
	inst := NewInstaller(t.TempDir(), DefaultMaxSkillSize)
	_, _, err := inst.Install(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty source")
	}
}

func TestInstaller_NoPersonalDir(t *testing.T) {
	inst := NewInstaller("", DefaultMaxSkillSize)
	_, _, err := inst.Install(context.Background(), "http://example.com/SKILL.md")
	if err == nil {
		t.Error("expected error when personalDir is empty")
	}
}

func TestInstaller_DetectSourceType(t *testing.T) {
	inst := NewInstaller(t.TempDir(), DefaultMaxSkillSize)

	tests := []struct {
		source   string
		expected SourceType
	}{
		{"https://example.com/SKILL.md", SourceHTTPFile},
		{"http://example.com/skill.zip", SourceHTTPArchive},
		{"https://example.com/skill.tar.gz", SourceHTTPArchive},
		{"https://example.com/skill.tgz", SourceHTTPArchive},
		{"/path/to/skill.zip", SourceLocalArchive},
		{"/path/to/skill.tar.gz", SourceLocalArchive},
		{"/path/to/SKILL.md", SourceLocalFile},
	}

	for _, tc := range tests {
		got := inst.detectSourceType(tc.source)
		if got != tc.expected {
			t.Errorf("detectSourceType(%q) = %d, want %d", tc.source, got, tc.expected)
		}
	}
}

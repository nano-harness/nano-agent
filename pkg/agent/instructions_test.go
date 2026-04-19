package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestInstructionLoader(t *testing.T) (*InstructionLoader, string) {
	t.Helper()
	dir := t.TempDir()
	il := &InstructionLoader{
		workingDir: dir,
		homeDir:    dir, // point home at temp dir to avoid reading real ~/.nano/NANO.md
		cache:      make(map[string]string),
	}
	return il, dir
}

func TestLoadAll_NoFiles(t *testing.T) {
	il, _ := newTestInstructionLoader(t)
	result := il.LoadAll()
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestLoadAll_GlobalNANO(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	globalDir := filepath.Join(dir, ".nano")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "NANO.md"), []byte("global instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Use a separate working dir so the global ~/.nano/NANO.md is found
	il.homeDir = dir
	il.workingDir = t.TempDir() // different from home to avoid project pickup
	result := il.LoadAll()
	if !strings.Contains(result, "global instructions") {
		t.Errorf("expected global instructions, got %q", result)
	}
}

func TestLoadAll_ProjectNANO(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	if err := os.WriteFile(filepath.Join(dir, "NANO.md"), []byte("project instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := il.LoadAll()
	if !strings.Contains(result, "project instructions") {
		t.Errorf("expected project instructions, got %q", result)
	}
}

func TestLoadAll_FallbackDotNanoNANO(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	nanoDir := filepath.Join(dir, ".nano")
	if err := os.MkdirAll(nanoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nanoDir, "NANO.md"), []byte("dotNano instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := il.LoadAll()
	if !strings.Contains(result, "dotNano instructions") {
		t.Errorf("expected .nano/NANO.md instructions, got %q", result)
	}
}

func TestLoadAll_ConcatenatesMultipleLayers(t *testing.T) {
	// Use separate home and working dirs
	homeDir := t.TempDir()
	workDir := t.TempDir()
	il := &InstructionLoader{workingDir: workDir, homeDir: homeDir, cache: make(map[string]string)}

	if err := os.MkdirAll(filepath.Join(homeDir, ".nano"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".nano", "NANO.md"), []byte("layer1-global"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "NANO.md"), []byte("layer2-project"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "NANO.local.md"), []byte("layer4-local"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := il.LoadAll()
	if !strings.Contains(result, "layer1-global") {
		t.Errorf("expected layer1-global, got %q", result)
	}
	if !strings.Contains(result, "layer2-project") {
		t.Errorf("expected layer2-project, got %q", result)
	}
	if !strings.Contains(result, "layer4-local") {
		t.Errorf("expected layer4-local, got %q", result)
	}
}

func TestLoadForDirectory(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "NANO.md"), []byte("subdir instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := il.LoadForDirectory(subDir)
	if !strings.Contains(result, "subdir instructions") {
		t.Errorf("expected subdir instructions, got %q", result)
	}
}

func TestLoadForDirectory_Missing(t *testing.T) {
	il, _ := newTestInstructionLoader(t)
	result := il.LoadForDirectory("/nonexistent/path/that/does/not/exist")
	if result != "" {
		t.Errorf("expected empty string for missing dir, got %q", result)
	}
}

func TestLoadRules_NoFrontmatterAlwaysLoaded(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	rulesDir := filepath.Join(dir, ".nano", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "rule1.md"), []byte("# Always loaded rule"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := il.LoadRules(nil)
	if !strings.Contains(result, "Always loaded rule") {
		t.Errorf("unconditional rule should always be loaded, got %q", result)
	}
}

func TestLoadRules_WithPathsFrontmatterConditional(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	rulesDir := filepath.Join(dir, ".nano", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\npaths:\n  - \"*.go\"\n---\n# Go-specific rule"
	if err := os.WriteFile(filepath.Join(rulesDir, "go-rule.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should NOT be loaded when no active files
	result := il.LoadRules(nil)
	if strings.Contains(result, "Go-specific rule") {
		t.Error("conditional rule should not be loaded when no active files")
	}

	// Should be loaded when a .go file is active
	result = il.LoadRules([]string{"main.go"})
	if !strings.Contains(result, "Go-specific rule") {
		t.Errorf("conditional rule should be loaded when *.go matches, got %q", result)
	}
}

func TestLoadRules_NoRulesDir(t *testing.T) {
	il, _ := newTestInstructionLoader(t)
	result := il.LoadRules(nil)
	if result != "" {
		t.Errorf("expected empty when no rules dir, got %q", result)
	}
}

func TestStripHTMLComments(t *testing.T) {
	il, _ := newTestInstructionLoader(t)
	input := "before <!-- this is a comment --> after"
	result := il.stripHTMLComments(input)
	if strings.Contains(result, "this is a comment") {
		t.Errorf("HTML comment not stripped: %q", result)
	}
	if !strings.Contains(result, "before") || !strings.Contains(result, "after") {
		t.Errorf("non-comment content removed: %q", result)
	}
}

func TestStripHTMLComments_Multiline(t *testing.T) {
	il, _ := newTestInstructionLoader(t)
	input := "line1\n<!-- \nmultiline\ncomment\n-->\nline2"
	result := il.stripHTMLComments(input)
	if strings.Contains(result, "multiline") {
		t.Errorf("multiline HTML comment not stripped: %q", result)
	}
	if !strings.Contains(result, "line1") || !strings.Contains(result, "line2") {
		t.Errorf("non-comment content removed: %q", result)
	}
}

func TestResolveImports(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	importedFile := filepath.Join(dir, "imported.md")
	if err := os.WriteFile(importedFile, []byte("imported content"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "@import \"imported.md\"\nafter import"
	result := il.resolveImports(content, 0)
	if !strings.Contains(result, "imported content") {
		t.Errorf("import not resolved, got %q", result)
	}
	if !strings.Contains(result, "after import") {
		t.Errorf("content after import missing, got %q", result)
	}
}

func TestResolveImports_MaxDepth(t *testing.T) {
	il, _ := newTestInstructionLoader(t)
	// At max depth, @import directives should be left as-is (no recursion)
	content := "@import \"something.md\""
	result := il.resolveImports(content, maxImportDepth)
	// Should not panic, and return content unchanged
	if result != content {
		t.Errorf("expected unchanged content at max depth, got %q", result)
	}
}

func TestResolveImports_MissingFile(t *testing.T) {
	il, _ := newTestInstructionLoader(t)
	content := "@import \"nonexistent.md\"\nother content"
	result := il.resolveImports(content, 0)
	// Missing import should be silently skipped; other content preserved
	if !strings.Contains(result, "other content") {
		t.Errorf("content after missing import should be preserved, got %q", result)
	}
}

func TestResolveImports_AbsolutePathRejected(t *testing.T) {
	il, _ := newTestInstructionLoader(t)
	// An absolute path import should be silently rejected.
	content := "@import \"/etc/passwd\"\nafter"
	result := il.resolveImports(content, 0)
	if strings.Contains(result, "root") || strings.Contains(result, "nobody") {
		t.Error("absolute path import should be rejected, not read")
	}
	if !strings.Contains(result, "after") {
		t.Errorf("content after rejected import should be preserved, got %q", result)
	}
}

func TestResolveImports_PathTraversalRejected(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	// Create a secret file outside the working dir.
	parent := filepath.Dir(dir)
	secretFile := filepath.Join(parent, "secret.md")
	if err := os.WriteFile(secretFile, []byte("secret content"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "@import \"../secret.md\"\nafter"
	result := il.resolveImports(content, 0)
	if strings.Contains(result, "secret content") {
		t.Error("path traversal import should be rejected")
	}
	if !strings.Contains(result, "after") {
		t.Errorf("content after rejected import should be preserved, got %q", result)
	}
}

func TestResolveImports_StripHTMLCommentsFromImported(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	importedFile := filepath.Join(dir, "imported.md")
	if err := os.WriteFile(importedFile, []byte("<!-- hidden -->visible"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "@import \"imported.md\""
	result := il.resolveImports(content, 0)
	if strings.Contains(result, "hidden") {
		t.Error("HTML comments in imported file should be stripped")
	}
	if !strings.Contains(result, "visible") {
		t.Errorf("non-comment content in imported file should be preserved, got %q", result)
	}
}

func TestReadFile_Caches(t *testing.T) {
	il, dir := newTestInstructionLoader(t)
	file := filepath.Join(dir, "test.md")
	if err := os.WriteFile(file, []byte("cached content"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = il.readFile(file)
	// Modify file on disk; cache should still return original
	if err := os.WriteFile(file, []byte("modified content"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := il.readFile(file)
	if result != "cached content" {
		t.Errorf("expected cached content, got %q", result)
	}
}

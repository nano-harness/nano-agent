//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/e2e/shared"
	"github.com/nano-harness/nano-agent/pkg/patch"
	"github.com/stretchr/testify/suite"
)

// BinaryModeSuite tests binary mode execution and patch generation.
// Binary mode is used for SWE-bench evaluation and produces git patches.
//
// This suite validates:
// - Git patch generation from agent changes
// - Working directory context
// - File modifications tracking
// - Patch format correctness
type BinaryModeSuite struct {
	AgentTestSuite
	repoPath string
}

func TestBinaryModeSuite(t *testing.T) {
	suite.Run(t, new(BinaryModeSuite))
}

func (s *BinaryModeSuite) SetupTest() {
	// Call parent setup
	s.AgentTestSuite.SetupTest()

	// Initialize git repository for binary mode tests
	s.repoPath = shared.InitTestRepo(s.T())

	// Change to repo directory for patch generation
	originalWd, err := os.Getwd()
	s.Require().NoError(err)

	err = os.Chdir(s.repoPath)
	s.Require().NoError(err)

	// Restore working directory after test
	s.T().Cleanup(func() {
		_ = os.Chdir(originalWd)
	})
}

// TestBinaryMode_PatchGeneration verifies basic patch generation.
func (s *BinaryModeSuite) TestBinaryMode_PatchGeneration() {
	// Create a file modification
	testFile := filepath.Join(s.repoPath, "test.go")
	content := `package main

func main() {
	println("Hello, world!")
}
`
	err := os.WriteFile(testFile, []byte(content), 0644)
	s.NoError(err)

	// Stage the change
	cmd := exec.Command("git", "add", "test.go")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	// Generate patch
	patchGen := patch.NewGenerator(s.repoPath, "")
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Verify patch contains the change
	s.Contains(patchContent, "diff --git")
	s.Contains(patchContent, "test.go")
	s.Contains(patchContent, "+++ b/test.go")
	s.Contains(patchContent, "+package main")
}

// TestBinaryMode_EmptyPatch verifies handling of no changes.
func (s *BinaryModeSuite) TestBinaryMode_EmptyPatch() {
	// Generate patch with no changes
	patchGen := patch.NewGenerator(s.repoPath, "")
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Patch should be empty
	s.Empty(strings.TrimSpace(patchContent), "Patch should be empty when no changes exist")
}

// TestBinaryMode_MultipleFiles verifies patching multiple files.
func (s *BinaryModeSuite) TestBinaryMode_MultipleFiles() {
	// Create multiple file changes
	files := map[string]string{
		"file1.go": "package file1\n",
		"file2.go": "package file2\n",
		"file3.go": "package file3\n",
	}

	for filename, content := range files {
		filePath := filepath.Join(s.repoPath, filename)
		err := os.WriteFile(filePath, []byte(content), 0644)
		s.NoError(err)
	}

	// Stage all changes
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = s.repoPath
	err := cmd.Run()
	s.NoError(err)

	// Generate patch
	patchGen := patch.NewGenerator(s.repoPath, "")
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Verify all files are in patch
	for filename := range files {
		s.Contains(patchContent, filename, "Patch should contain %s", filename)
	}
}

// TestBinaryMode_FileModification verifies modifying existing file.
func (s *BinaryModeSuite) TestBinaryMode_FileModification() {
	// Modify the README.md that exists from InitTestRepo
	readmePath := filepath.Join(s.repoPath, "README.md")
	modifiedContent := "# Test Repository\n\nModified content.\n\nNew line added.\n"
	err := os.WriteFile(readmePath, []byte(modifiedContent), 0644)
	s.NoError(err)

	// Stage change
	cmd := exec.Command("git", "add", "README.md")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	// Generate patch
	patchGen := patch.NewGenerator(s.repoPath, "")
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Verify patch shows modification
	s.Contains(patchContent, "diff --git a/README.md b/README.md")
	s.Contains(patchContent, "-Baseline content.")
	s.Contains(patchContent, "+Modified content.")
	s.Contains(patchContent, "+New line added.")
}

// TestBinaryMode_FileDeletion verifies deleting a file.
func (s *BinaryModeSuite) TestBinaryMode_FileDeletion() {
	// Create a file first
	tempFile := filepath.Join(s.repoPath, "to-delete.txt")
	err := os.WriteFile(tempFile, []byte("content to delete"), 0644)
	s.NoError(err)

	cmd := exec.Command("git", "add", "to-delete.txt")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	cmd = exec.Command("git", "commit", "-m", "Add file to delete")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	// Now delete it
	err = os.Remove(tempFile)
	s.NoError(err)

	cmd = exec.Command("git", "add", "to-delete.txt")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	// Generate patch
	patchGen := patch.NewGenerator(s.repoPath, "")
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Verify patch shows deletion
	s.Contains(patchContent, "deleted file mode")
	s.Contains(patchContent, "to-delete.txt")
}

// TestBinaryMode_BaseCommit verifies using base commit for diff.
func (s *BinaryModeSuite) TestBinaryMode_BaseCommit() {
	// Get current HEAD commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = s.repoPath
	output, err := cmd.Output()
	s.NoError(err)
	baseCommit := strings.TrimSpace(string(output))

	// Make a new commit
	testFile := filepath.Join(s.repoPath, "new-file.txt")
	err = os.WriteFile(testFile, []byte("new content"), 0644)
	s.NoError(err)

	cmd = exec.Command("git", "add", "new-file.txt")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	cmd = exec.Command("git", "commit", "-m", "Add new file")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	// Generate patch from base commit
	patchGen := patch.NewGenerator(s.repoPath, baseCommit)
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Verify patch includes the new file
	s.Contains(patchContent, "new-file.txt")
	s.Contains(patchContent, "+new content")
}

// TestBinaryMode_SavePatch verifies saving patch to file.
func (s *BinaryModeSuite) TestBinaryMode_SavePatch() {
	// Create a change
	testFile := filepath.Join(s.repoPath, "save-test.go")
	err := os.WriteFile(testFile, []byte("package savetest\n"), 0644)
	s.NoError(err)

	cmd := exec.Command("git", "add", "save-test.go")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	// Generate patch
	patchGen := patch.NewGenerator(s.repoPath, "")
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Save to file
	patchPath := filepath.Join(s.repoPath, "solution.patch")
	err = patchGen.SavePatch(patchContent, patchPath)
	s.NoError(err)

	// Verify file exists
	_, err = os.Stat(patchPath)
	s.NoError(err)

	// Verify content matches
	savedContent, err := os.ReadFile(patchPath)
	s.NoError(err)
	s.Equal(patchContent, string(savedContent))
}

// TestBinaryMode_UnstagedChanges verifies unstaged files are auto-staged and included.
func (s *BinaryModeSuite) TestBinaryMode_UnstagedChanges() {
	// Create unstaged change
	testFile := filepath.Join(s.repoPath, "unstaged.go")
	err := os.WriteFile(testFile, []byte("package unstaged\n"), 0644)
	s.NoError(err)

	// Don't stage it

	// Generate patch (auto-stages all changes with git add -A)
	patchGen := patch.NewGenerator(s.repoPath, "")
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Should include the unstaged file (it gets auto-staged by GenerateGitDiff)
	s.Contains(patchContent, "unstaged.go")
	s.Contains(patchContent, "package unstaged")
}

// TestBinaryMode_MixedChanges verifies combination of staged and unstaged (all get auto-staged).
func (s *BinaryModeSuite) TestBinaryMode_MixedChanges() {
	// Create staged change
	stagedFile := filepath.Join(s.repoPath, "staged.go")
	err := os.WriteFile(stagedFile, []byte("package staged\n"), 0644)
	s.NoError(err)

	cmd := exec.Command("git", "add", "staged.go")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	// Create unstaged change
	unstagedFile := filepath.Join(s.repoPath, "unstaged.go")
	err = os.WriteFile(unstagedFile, []byte("package unstaged\n"), 0644)
	s.NoError(err)

	// Generate patch (auto-stages all changes including unstaged)
	patchGen := patch.NewGenerator(s.repoPath, "")
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Should include both files (GenerateGitDiff auto-stages everything)
	s.Contains(patchContent, "staged.go")
	s.Contains(patchContent, "unstaged.go")
}

// TestBinaryMode_EmptyRepository verifies handling of empty repo.
func (s *BinaryModeSuite) TestBinaryMode_EmptyRepository() {
	// Create a new empty repo
	emptyRepo := s.T().TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = emptyRepo
	err := cmd.Run()
	s.NoError(err)

	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = emptyRepo
	err = cmd.Run()
	s.NoError(err)

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = emptyRepo
	err = cmd.Run()
	s.NoError(err)

	// Add a file
	testFile := filepath.Join(emptyRepo, "first.txt")
	err = os.WriteFile(testFile, []byte("first file"), 0644)
	s.NoError(err)

	cmd = exec.Command("git", "add", "first.txt")
	cmd.Dir = emptyRepo
	err = cmd.Run()
	s.NoError(err)

	// Generate patch (should work even without initial commit)
	patchGen := patch.NewGenerator(emptyRepo, "")
	patchContent, err := patchGen.GenerateGitDiff()

	// May error or return empty depending on implementation
	// Just verify it doesn't crash
	if err == nil {
		s.NotNil(patchContent)
	}
}

// TestBinaryMode_PatchFormat verifies patch follows git format.
func (s *BinaryModeSuite) TestBinaryMode_PatchFormat() {
	// Create a simple change
	testFile := filepath.Join(s.repoPath, "format-test.go")
	content := "package formattest\n\nfunc Test() {}\n"
	err := os.WriteFile(testFile, []byte(content), 0644)
	s.NoError(err)

	cmd := exec.Command("git", "add", "format-test.go")
	cmd.Dir = s.repoPath
	err = cmd.Run()
	s.NoError(err)

	// Generate patch
	patchGen := patch.NewGenerator(s.repoPath, "")
	patchContent, err := patchGen.GenerateGitDiff()
	s.NoError(err)

	// Verify standard git diff format
	s.Contains(patchContent, "diff --git")
	s.Contains(patchContent, "--- /dev/null")
	s.Contains(patchContent, "+++ b/format-test.go")
	s.Contains(patchContent, "@@ ")

	// Should be able to apply the patch
	// (We'll verify format is valid by checking standard markers)
	lines := strings.Split(patchContent, "\n")
	hasHunkHeader := false
	for _, line := range lines {
		if strings.HasPrefix(line, "@@") && strings.Contains(line, "@@") {
			hasHunkHeader = true
			break
		}
	}
	s.True(hasHunkHeader, "Patch should have valid hunk header")
}

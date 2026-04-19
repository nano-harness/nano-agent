package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper to create a temporary git repository
func createTempGitRepo(t *testing.T) string {
	tempDir, err := os.MkdirTemp("", "git-test-*")
	require.NoError(t, err)

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	err = cmd.Run()
	require.NoError(t, err)

	// Configure git user
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tempDir
	err = cmd.Run()
	require.NoError(t, err)

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tempDir
	err = cmd.Run()
	require.NoError(t, err)

	t.Cleanup(func() {
		os.RemoveAll(tempDir)
	})

	return tempDir
}

func TestGitManagerTool_GetStatus_Clean(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git status output for clean repo
	mockExecutor.SetResult([]string{"status", "--porcelain"}, &GitResult{
		Output:    "",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
		Output:    "main",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{}

	result, err := gitManager.getStatus(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "📍 当前分支：main")
	assert.Contains(t, result.UserContent, "✅ 工作目录干净")
	assert.Contains(t, result.LLMContent, "Git repository status: CLEAN")
	assert.Contains(t, result.LLMContent, "Branch: main")

	// Verify Data field
	data, ok := result.Data.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "main", data["current_branch"])
	assert.True(t, data["clean"].(bool))
}

func TestGitManagerTool_GetStatus_WithChanges(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git status output with changes
	statusOutput := ` M file1.go
 A file2.go
 D file3.go
?? file4.go`
	mockExecutor.SetResult([]string{"status", "--porcelain"}, &GitResult{
		Output:    statusOutput,
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
		Output:    "feature/test",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{}

	result, err := gitManager.getStatus(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "📍 当前分支：feature/test")
	assert.Contains(t, result.UserContent, "📝 已修改：1 个文件")
	assert.Contains(t, result.UserContent, "ile1.go")
	assert.Contains(t, result.LLMContent, "Git repository status: DIRTY")
	assert.Contains(t, result.LLMContent, "Modified files (1): ile1.go")
	assert.Contains(t, result.LLMContent, "Staged files (1): file2.go")
	assert.Contains(t, result.LLMContent, "Deleted files (1): file3.go")
	assert.Contains(t, result.LLMContent, "Untracked files (1): file4.go")

	// Verify Data field
	data, ok := result.Data.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "feature/test", data["current_branch"])
	assert.False(t, data["clean"].(bool))
	assert.Equal(t, 1, len(data["modified"].([]string)))
	assert.Equal(t, 1, len(data["added"].([]string)))
	assert.Equal(t, 1, len(data["deleted"].([]string)))
	assert.Equal(t, 1, len(data["untracked"].([]string)))
}

func TestGitManagerTool_CommitChanges_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git commit output
	commitOutput := `[main 1234567] Test commit message
 2 files changed, 10 insertions(+), 5 deletions(-)`
	mockExecutor.SetResult([]string{"commit", "-m", "Test commit message"}, &GitResult{
		Output:    commitOutput,
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
		Output:    "main",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"show", "--stat", "--format=", "HEAD"}, &GitResult{
		Output:    "file1.go | 5 +++++\nfile2.go | 5 -----\n 2 files changed, 10 insertions(+), 5 deletions(-)",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"show", "-s", "--format=%ci", "HEAD"}, &GitResult{
		Output:    "2023-01-01 12:00:00 +0000",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"log", "-1", "--format=%H|%s|%ct"}, &GitResult{
		Output:    "1234567890abcdef|Test commit message|1640995200",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"commit_message": "Test commit message",
	}

	result, err := gitManager.commitChanges(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "✅ 成功提交到分支")
	assert.Contains(t, result.UserContent, "Test commit message")
	assert.Contains(t, result.LLMContent, "Git commit successful")
	assert.Contains(t, result.LLMContent, "Branch: main")
	assert.Contains(t, result.LLMContent, "Hash: 1234567")
	assert.Contains(t, result.LLMContent, "Files changed: 2")
	assert.Contains(t, result.LLMContent, "Insertions: 10")
	assert.Contains(t, result.LLMContent, "Deletions: 5")

	// Verify Data field
	data, ok := result.Data.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "main", data["branch"])
	assert.Equal(t, "1234567", data["commit_hash"])
	assert.Equal(t, "Test commit message", data["commit_message"])
}

func TestGitManagerTool_CommitChanges_MissingMessage(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	ctx := context.Background()
	params := map[string]interface{}{}

	result, err := gitManager.commitChanges(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "commit_message parameter is required")
	assert.Contains(t, result.LLMContent, "git commit failed: commit_message parameter is required")
}

func TestGitManagerTool_PushChanges_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git push output
	pushOutput := `To https://github.com/user/repo.git
   1234567..abcdefg  main -> main`
	mockExecutor.SetResult([]string{"push", "origin", "main"}, &GitResult{
		Output:    pushOutput,
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
		Output:    "main",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"remote", "get-url", "origin"}, &GitResult{
		Output:    "https://github.com/user/repo.git",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"log", "origin/main..HEAD", "--oneline"}, &GitResult{
		Output:    "abcdefg Test commit",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"remote": "origin",
	}

	result, err := gitManager.pushChanges(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "✅ 成功推送分支 main 到远程 origin")
	assert.Contains(t, result.LLMContent, "Git push operation completed successfully")
	assert.Contains(t, result.LLMContent, "Branch: main")
	assert.Contains(t, result.LLMContent, "Remote: origin")
	assert.Contains(t, result.LLMContent, "Remote URL: https://github.com/user/repo.git")

	// Verify Data field
	data, ok := result.Data.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "main", data["branch"])
	assert.Equal(t, "origin", data["remote"])
	assert.Equal(t, "https://github.com/user/repo.git", data["remote_url"])
}

func TestGitManagerTool_PullChanges_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git pull output
	pullOutput := `From https://github.com/user/repo
   1234567..abcdefg  main     -> origin/main
Updating 1234567..abcdefg
Fast-forward
 file1.go | 5 +++++
 file2.go | 3 ---
 2 files changed, 5 insertions(+), 3 deletions(-)`
	mockExecutor.SetResult([]string{"pull", "origin"}, &GitResult{
		Output:    pullOutput,
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
		Output:    "main",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"remote": "origin",
	}

	result, err := gitManager.pullChanges(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "✅ 成功从 origin 拉取更新")
	assert.Contains(t, result.LLMContent, "Git pull operation completed successfully")
	assert.Contains(t, result.LLMContent, "Branch: main")
	assert.Contains(t, result.LLMContent, "Remote: origin")
	assert.Contains(t, result.LLMContent, "Files changed: 2")
	assert.Contains(t, result.LLMContent, "Lines added: 5")
	assert.Contains(t, result.LLMContent, "Lines removed: 3")
	assert.Contains(t, result.LLMContent, "Merge type: standard")

	// Verify Data field
	data, ok := result.Data.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "main", data["current_branch"])
	assert.Equal(t, "origin", data["remote"])
}

func TestGitManagerTool_PullChanges_UpToDate(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git pull output for up-to-date repo
	pullOutput := `Already up to date.`
	mockExecutor.SetResult([]string{"pull", "origin"}, &GitResult{
		Output:    pullOutput,
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
		Output:    "main",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"remote": "origin",
		"branch": "main",
	}

	result, err := gitManager.pullChanges(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "✅ 已是最新版本，无需更新")
	assert.Contains(t, result.LLMContent, "already up-to-date")

	// Verify Data field
	data, ok := result.Data.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "main", data["current_branch"])
	assert.True(t, data["up_to_date"].(bool))
}

func TestGitManagerTool_GetCommitLog_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git log output
	logOutput := `1234567890abcdef|1234567|John Doe|john@example.com|2024-01-15 10:30:00 +0000|feat: add new feature
abcdef1234567890|abcdef1|Jane Smith|jane@example.com|2024-01-14 15:45:00 +0000|fix: bug fix
567890abcdef1234|567890a|John Doe|john@example.com|2024-01-13 09:15:00 +0000|refactor: code cleanup`

	mockExecutor.SetResult([]string{"log", "--pretty=format:%H|%h|%an|%ae|%ad|%s", "--date=iso", "-10"}, &GitResult{
		Output:    logOutput,
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"limit": 10,
	}

	result, err := gitManager.getCommitLog(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "📜 最近的提交记录")
	assert.Contains(t, result.UserContent, "John Doe")
	assert.Contains(t, result.UserContent, "feat: add new feature")
	assert.Contains(t, result.LLMContent, "Git commit log analysis")
	assert.Contains(t, result.LLMContent, "Retrieved 3 commits from 2 unique contributors")
	assert.Contains(t, result.LLMContent, "1 features, 1 bugfixes, 1 refactors")

	// Verify Data field
	data, ok := result.Data.(map[string]interface{})
	assert.True(t, ok)
	commits, ok := data["commits"].([]map[string]string)
	assert.True(t, ok)
	assert.Equal(t, 3, len(commits))
	assert.Equal(t, "1234567890abcdef", commits[0]["full_hash"])
	assert.Equal(t, "feat: add new feature", commits[0]["message"])
	assert.Equal(t, "John Doe", commits[0]["author_name"])
}

// Integration test with real git repository
func TestGitManagerTool_Integration_RealRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tempDir := createTempGitRepo(t)
	gitManager := NewGitManagerTool(tempDir, nil, nil)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(testFile, []byte("Hello, World!"), 0644)
	require.NoError(t, err)

	ctx := context.Background()

	// Test status - should show untracked file
	result, err := gitManager.Execute(ctx, map[string]interface{}{
		"action": "status",
	})
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "test.txt")

	// Test add file
	result, err = gitManager.Execute(ctx, map[string]interface{}{
		"action": "add",
		"files":  []interface{}{"test.txt"},
	})
	assert.NoError(t, err)
	assert.True(t, result.Success)

	// Test commit
	result, err = gitManager.Execute(ctx, map[string]interface{}{
		"action":         "commit",
		"commit_message": "Add test file",
	})
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "✅ 成功提交到分支")

	// Test log
	result, err = gitManager.Execute(ctx, map[string]interface{}{
		"action": "log",
		"limit":  5,
	})
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "Add test file")
}

// Test helper functions
func TestHelperFunction_Min(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)

	// Test min function through reflection or by testing its usage
	// Since min is used in getStatus, we can test it indirectly
	mockExecutor := NewMockGitExecutor()

	// Create a status output with more than 5 files to test truncation
	statusOutput := ""
	for i := 1; i <= 10; i++ {
		statusOutput += fmt.Sprintf(" M file%d.go\n", i)
	}
	mockExecutor.SetResult([]string{"status", "--porcelain"}, &GitResult{
		Output:    statusOutput,
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})
	mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
		Output:    "main",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	result, err := gitManager.getStatus(ctx, map[string]interface{}{}, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	// Should only show first 5 files + "and X more" message
	assert.Contains(t, result.LLMContent, "ile1.go, file2.go, file3.go, file4.go, file5.go")
	assert.Contains(t, result.LLMContent, "(and 5 more)")
}

func TestHelperFunction_GetPushType(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)

	testCases := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "fast-forward",
			output:   "fast-forward\n   1234567..abcdefg  main -> main",
			expected: "fast-forward",
		},
		{
			name:     "up-to-date",
			output:   "Everything up-to-date",
			expected: "up-to-date",
		},
		{
			name:     "rejected",
			output:   "! [rejected]        main -> main (fetch first)",
			expected: "rejected",
		},
		{
			name:     "standard",
			output:   "To https://github.com/user/repo.git\n   1234567..abcdefg  main -> main",
			expected: "standard",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test getPushType function indirectly through push operation
			mockExecutor := NewMockGitExecutor()
			// Mock both possible push commands
			mockExecutor.SetResult([]string{"push", "origin"}, &GitResult{
				Output:    tc.output,
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})
			mockExecutor.SetResult([]string{"push", "origin", "main"}, &GitResult{
				Output:    tc.output,
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})
			mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
				Output:    "main",
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})
			mockExecutor.SetResult([]string{"remote", "get-url", "origin"}, &GitResult{
				Output:    "https://github.com/user/repo.git",
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			ctx := context.Background()
			result, err := gitManager.pushChanges(ctx, map[string]interface{}{
				"remote": "origin",
			}, mockExecutor)

			assert.NoError(t, err)
			assert.True(t, result.Success)
			assert.Contains(t, result.LLMContent, fmt.Sprintf("Push type: %s", tc.expected))
		})
	}
}

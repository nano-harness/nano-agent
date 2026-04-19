package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGitManagerTool_GetStatus_LLMContent tests the LLMContent output for git status
func TestGitManagerTool_GetStatus_LLMContent(t *testing.T) {
	tests := []struct {
		name           string
		gitOutput      string
		expectedFields []string
		description    string
	}{
		{
			name: "Clean repository",
			gitOutput: "On branch main\n" +
				"Your branch is up to date with 'origin/main'.\n\n" +
				"nothing to commit, working tree clean",
			expectedFields: []string{
				"Git repository status: CLEAN",
				"Branch: main",
				"Working directory has no changes",
			},
			description: "Clean repository should show organized status information",
		},
		{
			name: "Repository with changes",
			gitOutput: " M file1.go\n" +
				"A  file2.go\n" +
				" D file3.go\n" +
				"?? file4.go\n",
			expectedFields: []string{
				"Git repository status: DIRTY",
				"Branch: main",
				"Modified files (1):",
				"Staged files (1):",
				"Deleted files (1):",
				"Untracked files (1):",
				"Total changes: 4 files affected",
			},
			description: "Repository with changes should show detailed categorized information",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create GitManagerTool
			gitTool := NewGitManagerTool("/tmp/test", nil, nil)

			// Create mock executor
			mockExecutor := NewMockGitExecutor()

			// Mock git status --porcelain
			mockExecutor.SetResult([]string{"status", "--porcelain"}, &GitResult{
				Output:    tt.gitOutput,
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Mock git branch --show-current
			mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
				Output:    "main",
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Execute getStatus with proper parameters
			params := map[string]interface{}{}
			result, err := gitTool.getStatus(context.Background(), params, mockExecutor)
			require.NoError(t, err)

			// Verify LLMContent contains expected fields
			for _, field := range tt.expectedFields {
				assert.Contains(t, result.LLMContent, field,
					"LLMContent should contain '%s' for %s", field, tt.description)
			}

			// Verify LLMContent is well-structured
			assert.NotEmpty(t, result.LLMContent, "LLMContent should not be empty")
		})
	}
}

// TestGitManagerTool_CommitChanges_LLMContent tests the LLMContent output for git commit
func TestGitManagerTool_CommitChanges_LLMContent(t *testing.T) {
	tests := []struct {
		name           string
		gitOutput      string
		expectedFields []string
		description    string
	}{
		{
			name: "Successful commit",
			gitOutput: "[main 1a2b3c4] Add new feature\n" +
				" 3 files changed, 45 insertions(+), 12 deletions(-)\n" +
				" create mode 100644 new_file.go\n" +
				" delete mode 100644 old_file.go",
			expectedFields: []string{
				"Git commit successful",
				"Branch: main",
				"Hash: 1a2b3c4",
				"Message: \"Add new feature\"",
			},
			description: "Successful commit should show detailed commit information",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create GitManagerTool
			gitTool := NewGitManagerTool("/tmp/test", nil, nil)

			// Create mock executor
			mockExecutor := NewMockGitExecutor()

			// Mock git commit
			mockExecutor.SetResult([]string{"commit", "-m", "Add new feature"}, &GitResult{
				Output:    tt.gitOutput,
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Mock git branch --show-current
			mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
				Output:    "main",
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Mock git show --stat
			mockExecutor.SetResult([]string{"show", "--stat", "--format=", "HEAD"}, &GitResult{
				Output:    " 3 files changed, 45 insertions(+), 12 deletions(-)",
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Mock git show timestamp
			mockExecutor.SetResult([]string{"show", "-s", "--format=%ci", "HEAD"}, &GitResult{
				Output:    "2024-01-15 10:30:00 +0000",
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Execute commitChanges with proper parameters
			params := map[string]interface{}{
				"commit_message": "Add new feature",
			}
			result, err := gitTool.commitChanges(context.Background(), params, mockExecutor)
			require.NoError(t, err)

			// Verify LLMContent contains expected fields
			for _, field := range tt.expectedFields {
				assert.Contains(t, result.LLMContent, field,
					"LLMContent should contain '%s' for %s", field, tt.description)
			}

			// Verify LLMContent structure
			assert.NotEmpty(t, result.LLMContent, "LLMContent should not be empty")
		})
	}
}

// TestGitManagerTool_PushChanges_LLMContent tests the LLMContent output for git push
func TestGitManagerTool_PushChanges_LLMContent(t *testing.T) {
	tests := []struct {
		name           string
		gitOutput      string
		expectedFields []string
		description    string
	}{
		{
			name: "Successful push with new commits",
			gitOutput: "Enumerating objects: 5, done.\n" +
				"Counting objects: 100% (5/5), done.\n" +
				"Delta compression using up to 8 threads\n" +
				"Compressing objects: 100% (3/3), done.\n" +
				"Writing objects: 100% (3/3), 456 bytes | 456.00 KiB/s, done.\n" +
				"Total 3 (delta 1), reused 0 (delta 0), pack-reused 0\n" +
				"To https://github.com/user/repo.git\n" +
				"   abc123d..def456e  main -> main",
			expectedFields: []string{
				"Git push operation completed successfully",
				"Branch: main",
				"Remote: origin",
			},
			description: "Successful push should show detailed transfer information",
		},
		{
			name:      "Up to date push",
			gitOutput: "Everything up-to-date",
			expectedFields: []string{
				"Push type: up-to-date",
			},
			description: "Up-to-date push should show synchronization status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create GitManagerTool
			gitTool := NewGitManagerTool("/tmp/test", nil, nil)

			// Create mock executor
			mockExecutor := NewMockGitExecutor()

			// Mock git branch --show-current
			mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
				Output:    "main",
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Mock git push
			mockExecutor.SetResult([]string{"push", "origin"}, &GitResult{
				Output:    tt.gitOutput,
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Execute pushChanges with proper parameters
			params := map[string]interface{}{
				"remote": "origin",
			}
			result, err := gitTool.pushChanges(context.Background(), params, mockExecutor)
			require.NoError(t, err)

			// Verify LLMContent contains expected fields
			for _, field := range tt.expectedFields {
				assert.Contains(t, result.LLMContent, field,
					"LLMContent should contain '%s' for %s", field, tt.description)
			}

			// Verify LLMContent structure
			assert.NotEmpty(t, result.LLMContent, "LLMContent should not be empty")
		})
	}
}

// TestGitManagerTool_PullChanges_LLMContent tests the LLMContent output for git pull
func TestGitManagerTool_PullChanges_LLMContent(t *testing.T) {
	tests := []struct {
		name           string
		gitOutput      string
		expectedFields []string
		description    string
	}{
		{
			name: "Successful pull with updates",
			gitOutput: "From https://github.com/user/repo\n" +
				"   abc123d..def456e  main     -> origin/main\n" +
				"Updating abc123d..def456e\n" +
				"Fast-forward\n" +
				" file1.go | 10 ++++++++++\n" +
				" file2.go |  5 -----\n" +
				" 2 files changed, 10 insertions(+), 5 deletions(-)",
			expectedFields: []string{
				"Git pull operation completed successfully",
				"Branch: main",
				"Remote: origin",
				"Files changed: 2",
			},
			description: "Successful pull should show detailed update information",
		},
		{
			name:      "Already up to date pull",
			gitOutput: "Already up to date.",
			expectedFields: []string{
				"already up-to-date",
			},
			description: "Up-to-date pull should show synchronization status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create GitManagerTool
			gitTool := NewGitManagerTool("/tmp/test", nil, nil)

			// Create mock executor
			mockExecutor := NewMockGitExecutor()

			// Mock git branch --show-current
			mockExecutor.SetResult([]string{"branch", "--show-current"}, &GitResult{
				Output:    "main",
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Mock git pull
			mockExecutor.SetResult([]string{"pull", "origin"}, &GitResult{
				Output:    tt.gitOutput,
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Execute pullChanges with proper parameters
			params := map[string]interface{}{
				"remote": "origin",
			}
			result, err := gitTool.pullChanges(context.Background(), params, mockExecutor)
			require.NoError(t, err)

			// Verify LLMContent contains expected fields
			for _, field := range tt.expectedFields {
				assert.Contains(t, result.LLMContent, field,
					"LLMContent should contain '%s' for %s", field, tt.description)
			}

			// Verify LLMContent structure
			assert.NotEmpty(t, result.LLMContent, "LLMContent should not be empty")
		})
	}
}

// TestGitManagerTool_GetCommitLog_LLMContent tests the LLMContent output for git log
func TestGitManagerTool_GetCommitLog_LLMContent(t *testing.T) {
	tests := []struct {
		name           string
		gitOutput      string
		expectedFields []string
		description    string
	}{
		{
			name: "Multiple commits log",
			gitOutput: "def456e789012345678901234567890123456789|def456e|John Doe|john@example.com|2024-01-15 10:30:00 +0000|Add new feature\n" +
				"abc123d456789012345678901234567890123456|abc123d|Jane Smith|jane@example.com|2024-01-14 15:45:00 +0000|Fix bug in authentication\n" +
				"789xyz0123456789012345678901234567890123|789xyz0|Bob Wilson|bob@example.com|2024-01-13 09:15:00 +0000|Update documentation",
			expectedFields: []string{
				"Git commit log analysis",
				"Retrieved 3 commits",
				"def456e",
				"John Doe",
				"Add new feature",
			},
			description: "Multiple commits should show structured log information",
		},
		{
			name:      "Single commit log",
			gitOutput: "abc123d456789012345678901234567890123456|abc123d|Developer|dev@example.com|2024-01-16 14:20:00 +0000|Initial commit",
			expectedFields: []string{
				"Git commit log analysis",
				"Retrieved 1 commits",
				"abc123d",
				"Developer",
				"Initial commit",
			},
			description: "Single commit should show basic log information",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create GitManagerTool
			gitTool := NewGitManagerTool("/tmp/test", nil, nil)

			// Create mock executor
			mockExecutor := NewMockGitExecutor()

			// Mock git log
			mockExecutor.SetResult([]string{"log", "--pretty=format:%H|%h|%an|%ae|%ad|%s", "--date=iso", "-10"}, &GitResult{
				Output:    tt.gitOutput,
				ExitCode:  0,
				Error:     nil,
				Timestamp: time.Now(),
			})

			// Execute getCommitLog with proper parameters
			params := map[string]interface{}{
				"limit": 10,
			}
			result, err := gitTool.getCommitLog(context.Background(), params, mockExecutor)
			require.NoError(t, err)

			// Verify LLMContent contains expected fields
			for _, field := range tt.expectedFields {
				assert.Contains(t, result.LLMContent, field,
					"LLMContent should contain '%s' for %s", field, tt.description)
			}

			// Verify LLMContent structure
			assert.NotEmpty(t, result.LLMContent, "LLMContent should not be empty")
		})
	}
}

// TestLLMContentStructure tests the overall structure and quality of LLMContent
func TestLLMContentStructure(t *testing.T) {
	tests := []struct {
		name        string
		operation   string
		gitOutput   string
		checkFields []string
	}{
		{
			name:      "Status LLMContent structure",
			operation: "status",
			gitOutput: "",
			checkFields: []string{
				"Git repository status:",
				"Branch:",
			},
		},
		{
			name:      "Commit LLMContent structure",
			operation: "commit",
			gitOutput: "[main abc123d] Test commit\n 1 file changed, 1 insertion(+)",
			checkFields: []string{
				"Git commit successful",
				"Branch:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitTool := NewGitManagerTool("/tmp/test", nil, nil)
			mockExecutor := NewMockGitExecutor()

			var result *interfaces.ToolResult
			var err error

			switch tt.operation {
			case "status":
				mockExecutor.SetResult([]string{"status", "--porcelain"}, &GitResult{
					Output:    tt.gitOutput,
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
				params := map[string]interface{}{}
				result, err = gitTool.getStatus(context.Background(), params, mockExecutor)
			case "commit":
				mockExecutor.SetResult([]string{"commit", "-m", "Test commit"}, &GitResult{
					Output:    tt.gitOutput,
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
					Output:    " 1 file changed, 1 insertion(+)",
					ExitCode:  0,
					Error:     nil,
					Timestamp: time.Now(),
				})
				mockExecutor.SetResult([]string{"show", "-s", "--format=%ci", "HEAD"}, &GitResult{
					Output:    "2024-01-15 10:30:00 +0000",
					ExitCode:  0,
					Error:     nil,
					Timestamp: time.Now(),
				})
				params := map[string]interface{}{
					"commit_message": "Test commit",
				}
				result, err = gitTool.commitChanges(context.Background(), params, mockExecutor)
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			// Check that LLMContent is well-formatted
			lines := strings.Split(result.LLMContent, "\n")
			assert.Greater(t, len(lines), 0, "LLMContent should have content")

			// Check for required fields
			for _, field := range tt.checkFields {
				assert.Contains(t, result.LLMContent, field,
					"LLMContent should contain required field: %s", field)
			}

			// Check that LLMContent is not just raw git output
			if tt.gitOutput != "" {
				assert.NotEqual(t, strings.TrimSpace(tt.gitOutput), strings.TrimSpace(result.LLMContent),
					"LLMContent should be enhanced, not just raw git output")
			}
		})
	}
}

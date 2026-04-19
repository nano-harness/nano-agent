package workspace

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGitManagerTool_RemoteList_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git remote -v output
	remoteOutput := `origin	https://github.com/user/repo.git (fetch)
origin	https://github.com/user/repo.git (push)
upstream	https://github.com/upstream/repo.git (fetch)
upstream	https://github.com/upstream/repo.git (push)`

	mockExecutor.SetResult([]string{"remote", "-v"}, &GitResult{
		Output:    remoteOutput,
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()

	// Call listRemotes directly
	result, err := gitManager.listRemotes(ctx, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "📋 远程仓库列表")
	assert.Contains(t, result.UserContent, "origin")
	assert.Contains(t, result.UserContent, "upstream")
	assert.Contains(t, result.UserContent, "https://github.com/user/repo.git")
	assert.Contains(t, result.LLMContent, "Git remote list retrieved successfully")
	assert.Contains(t, result.LLMContent, "Found 2 remotes")

	// Verify Data field
	data, ok := result.Data.(map[string]interface{})
	assert.True(t, ok)
	remotes, ok := data["remotes"].([]map[string]string)
	assert.True(t, ok)
	assert.Equal(t, 2, len(remotes))
	assert.Equal(t, "origin", remotes[0]["name"])
	assert.Equal(t, "https://github.com/user/repo.git", remotes[0]["url"])
}

func TestGitManagerTool_RemoteList_Empty(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock empty git remote -v output
	mockExecutor.SetResult([]string{"remote", "-v"}, &GitResult{
		Output:    "",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()

	// Call listRemotes directly
	result, err := gitManager.listRemotes(ctx, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "No remotes configured")
	assert.Contains(t, result.LLMContent, "Found 0 remotes")

	// Verify Data field
	data, ok := result.Data.(map[string]interface{})
	assert.True(t, ok)
	remotes, ok := data["remotes"].([]map[string]string)
	assert.True(t, ok)
	assert.Equal(t, 0, len(remotes))
}

func TestGitManagerTool_RemoteAdd_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock successful git remote add
	mockExecutor.SetResult([]string{"remote", "add", "origin", "https://github.com/user/repo.git"}, &GitResult{
		Output:    "",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"name": "origin",
		"url":  "https://github.com/user/repo.git",
	}

	// Call addRemote directly
	result, err := gitManager.addRemote(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "成功添加远程仓库")
	assert.Contains(t, result.UserContent, "origin")
	assert.Contains(t, result.UserContent, "https://github.com/user/repo.git")

	// Check data
	data := result.Data.(map[string]interface{})
	assert.Equal(t, "origin", data["name"])
	assert.Equal(t, "https://github.com/user/repo.git", data["url"])
}

func TestGitManagerTool_RemoteAdd_MissingParams(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	ctx := context.Background()
	params := map[string]interface{}{
		"subcommand": "add",
		// Missing name and url
	}

	// Call manageRemotes directly
	result, err := gitManager.manageRemotes(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "Remote name is required for add operation")
}

func TestGitManagerTool_RemoteRemove_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git remote remove output (usually no output on success)
	mockExecutor.SetResult([]string{"remote", "remove", "origin"}, &GitResult{
		Output:    "",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"subcommand": "remove",
		"name":       "origin",
	}

	// Call manageRemotes directly
	result, err := gitManager.manageRemotes(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "成功删除远程仓库")
	assert.Contains(t, result.UserContent, "origin")

	// Check data
	data := result.Data.(map[string]interface{})
	assert.Equal(t, "origin", data["name"])
}

func TestGitManagerTool_RemoteRename_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git remote rename output (usually no output on success)
	mockExecutor.SetResult([]string{"remote", "rename", "origin", "upstream"}, &GitResult{
		Output:    "",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"subcommand": "rename",
		"old_name":   "origin",
		"new_name":   "upstream",
	}

	// Call manageRemotes directly
	result, err := gitManager.manageRemotes(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "成功重命名远程仓库")
	assert.Contains(t, result.UserContent, "origin")
	assert.Contains(t, result.UserContent, "upstream")

	// Check data
	data := result.Data.(map[string]interface{})
	assert.Equal(t, "origin", data["old_name"])
	assert.Equal(t, "upstream", data["new_name"])
}

func TestGitManagerTool_RemoteGetURL_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git remote get-url output
	mockExecutor.SetResult([]string{"remote", "get-url", "origin"}, &GitResult{
		Output:    "https://github.com/user/repo.git",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"subcommand": "get-url",
		"name":       "origin",
	}

	// Call manageRemotes directly
	result, err := gitManager.manageRemotes(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "远程仓库 origin 的URL")
	assert.Contains(t, result.UserContent, "https://github.com/user/repo.git")

	// Check data
	data := result.Data.(map[string]interface{})
	assert.Equal(t, "origin", data["name"])
	assert.Equal(t, "https://github.com/user/repo.git", data["url"])
}

func TestGitManagerTool_RemoteSetURL_Success(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git remote set-url output (usually no output on success)
	mockExecutor.SetResult([]string{"remote", "set-url", "origin", "https://github.com/newuser/repo.git"}, &GitResult{
		Output:    "",
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		"subcommand": "set-url",
		"name":       "origin",
		"url":        "https://github.com/newuser/repo.git",
	}

	// Call manageRemotes directly
	result, err := gitManager.manageRemotes(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "成功更新远程仓库")
	assert.Contains(t, result.UserContent, "origin")
	assert.Contains(t, result.UserContent, "https://github.com/newuser/repo.git")

	// Check data
	data := result.Data.(map[string]interface{})
	assert.Equal(t, "origin", data["name"])
	assert.Equal(t, "https://github.com/newuser/repo.git", data["url"])
}

func TestGitManagerTool_RemoteDefaultToList(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	// Mock git remote -v output
	remoteOutput := `origin	https://github.com/user/repo.git (fetch)
origin	https://github.com/user/repo.git (push)`

	mockExecutor.SetResult([]string{"remote", "-v"}, &GitResult{
		Output:    remoteOutput,
		ExitCode:  0,
		Error:     nil,
		Timestamp: time.Now(),
	})

	ctx := context.Background()
	params := map[string]interface{}{
		// No subcommand specified, should default to "list"
	}

	// Call manageRemotes directly
	result, err := gitManager.manageRemotes(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.UserContent, "远程仓库列表")
}

func TestGitManagerTool_RemoteError_InvalidSubcommand(t *testing.T) {
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)
	mockExecutor := NewMockGitExecutor()

	ctx := context.Background()
	params := map[string]interface{}{
		"subcommand": "invalid",
	}

	// Call manageRemotes directly
	result, err := gitManager.manageRemotes(ctx, params, mockExecutor)

	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "Unsupported remote subcommand")
}

func TestGitManagerTool_RemoteIntegration_Execute(t *testing.T) {
	// This test verifies that the remote action is properly handled
	// by checking the schema and basic functionality
	gitManager := NewGitManagerTool("/tmp/test", nil, nil)

	// Test schema contains remote-related properties
	schema := gitManager.Schema()
	assert.NotNil(t, schema)
	assert.Contains(t, schema.Properties, "subcommand")
	assert.Contains(t, schema.Properties, "name")
	assert.Contains(t, schema.Properties, "url")
	assert.Contains(t, schema.Properties, "old_name")
	assert.Contains(t, schema.Properties, "new_name")

	// Test that remote is in allowed commands
	assert.Contains(t, gitManager.gitConfig.AllowedCommands, "remote")
}

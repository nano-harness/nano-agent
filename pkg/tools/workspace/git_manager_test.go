package workspace

import (
	"context"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/stretchr/testify/assert"
)

func TestGitManagerTool_Basic(t *testing.T) {
	// Create a temporary directory for testing
	workingDir := "/tmp/test-git-repo"
	config := map[string]interface{}{
		"git": map[string]interface{}{
			"command_timeout": "10s",
			"enable_cache":    true,
		},
	}

	// Create optimized git manager
	gitManager := NewGitManagerTool(workingDir, config, nil)

	// Test basic properties
	assert.Equal(t, "git_manager", gitManager.Name())
	assert.NotEmpty(t, gitManager.Description())
	assert.Equal(t, interfaces.CategoryGit, gitManager.Category())
	assert.True(t, gitManager.RequiresConfirmation())

	// Test schema
	schema := gitManager.Schema()
	assert.NotNil(t, schema)
	assert.Contains(t, schema.Properties, "action")
	assert.Contains(t, schema.Required, "action")
}

func TestGitManagerTool_SecurityValidation(t *testing.T) {
	workingDir := "/tmp/test-repo"
	gitManager := NewGitManagerTool(workingDir, nil, nil)

	// Test path validation
	tests := []struct {
		name        string
		path        string
		shouldError bool
	}{
		{"valid relative path", "src/main.go", false},
		{"path traversal", "../../../etc/passwd", true},
		{"absolute path", "/etc/passwd", true},
		{"current directory", ".", false},
		{"nested valid path", "src/components/app.js", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gitManager.validatePath(tt.path)
			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGitManagerTool_BranchNameValidation(t *testing.T) {
	workingDir := "/tmp/test-repo"
	gitManager := NewGitManagerTool(workingDir, nil, nil)

	tests := []struct {
		name        string
		branchName  string
		shouldError bool
	}{
		{"valid branch name", "feature/new-feature", false},
		{"valid main branch", "main", false},
		{"invalid with space", "feature branch", true},
		{"invalid with colon", "feature:branch", true},
		{"invalid starting with dash", "-feature", true},
		{"invalid ending with dot", "feature.", true},
		{"empty name", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gitManager.validateBranchName(tt.branchName)
			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGitManagerTool_RemoteURLValidation(t *testing.T) {
	workingDir := "/tmp/test-repo"
	gitManager := NewGitManagerTool(workingDir, nil, nil)

	tests := []struct {
		name        string
		url         string
		shouldError bool
	}{
		{"valid github https", "https://github.com/user/repo.git", false},
		{"valid gitlab https", "https://gitlab.com/user/repo.git", false},
		{"valid github ssh", "git@github.com:user/repo.git", false},
		{"invalid domain", "https://malicious.com/repo.git", true},
		{"empty url", "", false}, // Empty is allowed (optional parameter)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gitManager.validateRemoteURL(tt.url)
			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGitManagerTool_GitArgsValidation(t *testing.T) {
	workingDir := "/tmp/test-git"
	config := map[string]interface{}{
		"git": map[string]interface{}{
			"allowed_urls": []string{"https://github.com/*"},
		},
	}
	gitManager := NewGitManagerTool(workingDir, config, nil)

	tests := []struct {
		name        string
		args        []string
		shouldError bool
	}{
		{"safe args", []string{"status", "--porcelain"}, false},
		{"dangerous exec", []string{"--exec", "rm -rf /"}, true},
		{"dangerous config", []string{"--config", "core.editor=evil"}, true},
		{"safe commit", []string{"commit", "-m", "message"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gitManager.validateGitArgs(tt.args)
			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGitManagerTool_ErrorHandling(t *testing.T) {
	workingDir := "/tmp/test-git"
	gitManager := NewGitManagerTool(workingDir, nil, nil)

	tests := []struct {
		name         string
		output       string
		expectedType string
		expectedCode int
	}{
		{"not git repo", "not a git repository", "not_git_repo", 1001},
		{"permission denied", "Permission denied", "permission_denied", 1002},
		{"network error", "Connection refused", "network_error", 1003},
		{"nothing to commit", "nothing to commit", "nothing_to_commit", 1004},
		{"merge conflict", "merge conflict", "merge_conflict", 1005},
		{"branch exists", "branch already exists", "branch_exists", 1006},
		{"unknown error", "some other error", "unknown", 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitErr := gitManager.handleGitError(assert.AnError, tt.output)
			assert.NotNil(t, gitErr)
			assert.Equal(t, tt.expectedType, gitErr.Type)
			assert.Equal(t, tt.expectedCode, gitErr.Code)
		})
	}
}

func TestGitManagerTool_Cache(t *testing.T) {
	workingDir := "/tmp/test-git"
	config := map[string]interface{}{
		"git": map[string]interface{}{
			"enable_cache":     true,
			"cache_expiration": "1s",
		},
	}
	gitManager := NewGitManagerTool(workingDir, config, nil)

	// Test cache set and get
	key := "test_key"
	result := &interfaces.ToolResult{
		Success:     true,
		UserContent: "test content",
	}

	// Set cache
	gitManager.setCachedResult(key, result)

	// Get from cache
	cached, found := gitManager.getCachedResult(key)
	assert.True(t, found)
	assert.Equal(t, result.UserContent, cached.UserContent)

	// Wait for expiration
	time.Sleep(1100 * time.Millisecond)

	// Should not find expired cache
	_, found = gitManager.getCachedResult(key)
	assert.False(t, found)
}

func TestGitManagerTool_ExecuteValidation(t *testing.T) {
	workingDir := "/tmp/test-git"
	gitManager := NewGitManagerTool(workingDir, nil, nil)

	ctx := context.Background()

	// Test missing parameters
	result, err := gitManager.Execute(ctx, nil)
	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "parameters are missing")

	// Test missing action
	result, err = gitManager.Execute(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "action parameter is required")

	// Test invalid action
	result, err = gitManager.Execute(ctx, map[string]interface{}{
		"action": "invalid_action",
	})
	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "not allowed")

	// Test valid action (should fail due to no git repo, but validation should pass)
	result, err = gitManager.Execute(ctx, map[string]interface{}{ //nolint:staticcheck
		"action": "status",
	})
	assert.NoError(t, err)
	// This will fail because there's no actual git repo, but that's expected
}

func TestGitManagerTool_Configuration(t *testing.T) {
	workingDir := "/tmp/test-git"

	// Test with custom configuration
	config := map[string]interface{}{
		"git": map[string]interface{}{
			"command_timeout": "5s",
			"max_output_size": 512,
			"enable_cache":    false,
		},
	}

	gitManager := NewGitManagerTool(workingDir, config, nil)

	// Verify configuration was applied
	assert.Equal(t, 5*time.Second, gitManager.gitConfig.CommandTimeout)
	assert.Equal(t, 512, gitManager.gitConfig.MaxOutputSize)
	assert.False(t, gitManager.gitConfig.EnableCache)
}

// Benchmark tests for performance optimization
func BenchmarkGitManagerToolEnhanced_ValidatePath(b *testing.B) {
	workingDir := "/tmp/test-git"
	gitManager := NewGitManagerTool(workingDir, nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gitManager.validatePath("src/main.go")
	}
}

func BenchmarkGitManagerToolEnhanced_ValidateBranchName(b *testing.B) {
	workingDir := "/tmp/test-git"
	gitManager := NewGitManagerTool(workingDir, nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gitManager.validateBranchName("feature/new-feature")
	}
}

func BenchmarkGitManagerToolEnhanced_HandleGitError(b *testing.B) {
	workingDir := "/tmp/test-git"
	gitManager := NewGitManagerTool(workingDir, nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gitManager.handleGitError(assert.AnError, "not a git repository")
	}
}

func TestGitManagerTool_URLValidation(t *testing.T) {
	workingDir := "/tmp/test-git"
	gitManager := NewGitManagerTool(workingDir, nil, nil)

	tests := []struct {
		name        string
		url         string
		shouldError bool
	}{
		{"valid github https", "https://github.com/user/repo.git", false},
		{"valid gitlab https", "https://gitlab.com/user/repo.git", false},
		{"valid github ssh", "git@github.com:user/repo.git", false},
		{"invalid domain", "https://malicious.com/repo.git", true},
		{"empty url", "", false}, // Empty is allowed (optional parameter)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gitManager.validateRemoteURL(tt.url)
			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGitManagerTool_ConfigurationParsing(t *testing.T) {
	workingDir := "/tmp/test-git"

	// Test with nested configuration
	config := map[string]interface{}{
		"git": map[string]interface{}{
			"allowed_urls": []string{"https://github.com/*"},
		},
	}
	gitManager := NewGitManagerTool(workingDir, config, nil)

	tests := []struct {
		name        string
		url         string
		shouldError bool
	}{
		{"valid github https", "https://github.com/user/repo.git", false},
		{"valid gitlab https", "https://gitlab.com/user/repo.git", false},
		{"valid github ssh", "git@github.com:user/repo.git", false},
		{"invalid domain", "https://malicious.com/repo.git", true},
		{"empty url", "", false}, // Empty is allowed (optional parameter)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gitManager.validateRemoteURL(tt.url)
			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

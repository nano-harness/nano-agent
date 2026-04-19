package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// getPushType analyzes git push output to determine the push type
func getPushType(output string) string {
	if strings.Contains(output, "fast-forward") {
		return "fast-forward"
	} else if strings.Contains(output, "up-to-date") {
		return "up-to-date"
	} else if strings.Contains(output, "rejected") {
		return "rejected"
	}
	return "standard"
}

// GitConfig holds configuration for Git operations
type GitConfig struct {
	DefaultRemote     string        `yaml:"default_remote"`
	CommandTimeout    time.Duration `yaml:"command_timeout"`
	MaxOutputSize     int           `yaml:"max_output_size"`
	EnableCache       bool          `yaml:"enable_cache"`
	CacheExpiration   time.Duration `yaml:"cache_expiration"`
	AllowedCommands   []string      `yaml:"allowed_commands"`
	AllowedRemoteURLs []string      `yaml:"allowed_remote_urls"`
}

// GitError represents a categorized Git error
type GitError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int    `json:"code"`
	Details string `json:"details,omitempty"`
}

func (e *GitError) Error() string {
	return fmt.Sprintf("Git error [%s]: %s (code: %d)", e.Type, e.Message, e.Code)
}

// GitResult represents the result of a Git command execution
type GitResult struct {
	Output    string
	ExitCode  int
	Error     error
	Timestamp time.Time
}

// GitExecutor interface for executing Git commands
type GitExecutor interface {
	Execute(ctx context.Context, args ...string) (*GitResult, error)
}

// DefaultGitExecutor implements GitExecutor
type DefaultGitExecutor struct {
	workingDir string
	timeout    time.Duration
	maxOutput  int
}

func NewDefaultGitExecutor(workingDir string, timeout time.Duration, maxOutput int) *DefaultGitExecutor { //nolint:revive
	return &DefaultGitExecutor{
		workingDir: workingDir,
		timeout:    timeout,
		maxOutput:  maxOutput,
	}
}

func (e *DefaultGitExecutor) Execute(ctx context.Context, args ...string) (*GitResult, error) { //nolint:revive
	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = e.workingDir

	output, err := cmd.CombinedOutput()

	// Limit output size
	outputStr := string(output)
	if len(outputStr) > e.maxOutput {
		outputStr = outputStr[:e.maxOutput] + "\n... (output truncated)"
	}

	result := &GitResult{
		Output:    outputStr,
		ExitCode:  0,
		Error:     err,
		Timestamp: time.Now(),
	}

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	// Return error if command failed
	if result.ExitCode != 0 || err != nil {
		return result, err
	}

	return result, nil
}

// CacheEntry represents a cached result
type CacheEntry struct {
	Result    *interfaces.ToolResult
	Timestamp time.Time
}

// GitRepositoryInfo holds information about a discovered Git repository
type GitRepositoryInfo struct {
	Path         string // Absolute path to the repository root
	RelativePath string // Relative path from workspace root
	IsSubmodule  bool   // Whether this is a Git submodule
}

// GitManagerTool implements Git repository management tool with multi-repository support
type GitManagerTool struct {
	workspaceRoot string // Root directory of the workspace
	config        map[string]interface{}
	gitConfig     *GitConfig
	cache         *sync.Map
	mutex         sync.RWMutex //nolint:unused

	// Repository discovery cache
	repoCache    map[string]*GitRepositoryInfo
	repoCacheMu  sync.RWMutex
	lastScanTime time.Time
	scanInterval time.Duration
}

// NewGitManagerTool creates a new GitManagerTool instance.
// The third parameter (pathChecker) is accepted for API consistency but is not
// used by GitManagerTool because git operations are restricted to the workspace
// root rather than arbitrary paths.
func NewGitManagerTool(workingDir string, config map[string]interface{}, _ interface{}) *GitManagerTool {
	// Create configuration manager
	configManager := NewGitConfigManager(config)
	gitConfig := configManager.GetConfig()

	return &GitManagerTool{
		workspaceRoot: workingDir,
		config:        config,
		gitConfig:     gitConfig,
		cache:         &sync.Map{},
		repoCache:     make(map[string]*GitRepositoryInfo),
		scanInterval:  5 * time.Minute, // Rescan repositories every 5 minutes
	}
}

// Name returns the tool name
func (t *GitManagerTool) Name() string {
	return "git_manager"
}

// Description returns the tool description
func (t *GitManagerTool) Description() string {
	return "Git repository management with multi-repository support, enhanced security, error handling, and performance"
}

// Category returns the tool category
func (t *GitManagerTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryGit
}

// RequiresConfirmation checks if the tool requires confirmation
func (t *GitManagerTool) RequiresConfirmation() bool {
	return true
}

// ConcurrencySafe returns false: git operations mutate repository state.
func (t *GitManagerTool) ConcurrencySafe() bool { return false }

// Schema returns the tool schema
func (t *GitManagerTool) Schema() *interfaces.ToolSchema {
	actionProp := interfaces.NewStringPropertyWithEnum(
		"Git action to perform",
		t.gitConfig.AllowedCommands,
	)
	actionProp.Examples = []string{"status", "commit", "push", "branch"}
	actionProp.Usage = "Available actions: " + strings.Join(t.gitConfig.AllowedCommands, ", ")

	repoPathProp := interfaces.NewStringProperty("Repository path (optional, auto-detected if not provided)")
	repoPathProp.Examples = []string{".", "frontend", "backend/api", "libs/common"}
	repoPathProp.Usage = "Relative path to the Git repository within workspace. If not provided, will auto-detect based on current context."

	repoURLProp := interfaces.NewStringProperty("Repository URL (required for clone)")
	repoURLProp.Examples = []string{"https://github.com/user/repo.git", "git@github.com:user/repo.git"}
	repoURLProp.Usage = "HTTPS or SSH URL for Git repository. Must match allowed URL patterns."

	targetDirProp := interfaces.NewStringProperty("Target directory for clone (optional)")
	targetDirProp.Examples = []string{"./my-project", "projects/app"}
	targetDirProp.Usage = "Relative path within working directory. Absolute paths are not allowed for security."

	branchNameProp := interfaces.NewStringProperty("Branch name (required for branch operations)")
	branchNameProp.Examples = []string{"feature/new-feature", "bugfix/issue-123", "main"}
	branchNameProp.Usage = "Valid Git branch name. Must follow Git naming conventions."

	commitMessageProp := interfaces.NewStringProperty("Commit message (required for commit)")
	commitMessageProp.Examples = []string{"Add new feature", "Fix bug in authentication", "Update documentation"}
	commitMessageProp.Usage = "Descriptive commit message. Consider using conventional commit format."

	filesProp := interfaces.NewArrayProperty("Files to add (optional, defaults to all changes)", "string")
	filesProp.Examples = []string{"src/main.go", "README.md", "."}
	filesProp.Usage = "Specific files to stage. Use '.' to add all changes."

	remoteProp := interfaces.NewStringProperty("Remote name (optional, defaults to 'origin')")
	remoteProp.Examples = []string{"origin", "upstream", "fork"}
	remoteProp.Usage = "Name of the remote repository for push/pull operations."

	subcommandProp := interfaces.NewStringPropertyWithEnum(
		"Remote subcommand (for remote action)",
		[]string{"list", "-v", "verbose", "add", "remove", "rm", "rename", "get-url", "set-url"},
	)
	subcommandProp.Examples = []string{"list", "add", "remove", "get-url"}
	subcommandProp.Usage = "Remote operation to perform: list (default), add, remove, rename, get-url, set-url"

	remoteNameProp := interfaces.NewStringProperty("Remote name (required for add/remove/rename/get-url/set-url operations)")
	remoteNameProp.Examples = []string{"origin", "upstream", "fork"}
	remoteNameProp.Usage = "Name of the remote repository"

	remoteURLProp := interfaces.NewStringProperty("Remote URL (required for add/set-url operations)")
	remoteURLProp.Examples = []string{"https://github.com/user/repo.git", "git@github.com:user/repo.git"}
	remoteURLProp.Usage = "HTTPS or SSH URL for remote repository"

	oldNameProp := interfaces.NewStringProperty("Old remote name (required for rename operation)")
	oldNameProp.Examples = []string{"origin", "upstream"}
	oldNameProp.Usage = "Current name of the remote to be renamed"

	newNameProp := interfaces.NewStringProperty("New remote name (required for rename operation)")
	newNameProp.Examples = []string{"origin", "upstream"}
	newNameProp.Usage = "New name for the remote"

	return interfaces.CreateSchema(
		"Git repository management with multi-repository support and enhanced security",
		map[string]*interfaces.PropertySchema{
			"action":         actionProp,
			"repo_path":      repoPathProp,
			"repo_url":       repoURLProp,
			"target_dir":     targetDirProp,
			"branch_name":    branchNameProp,
			"commit_message": commitMessageProp,
			"files":          filesProp,
			"remote":         remoteProp,
			"subcommand":     subcommandProp,
			"name":           remoteNameProp,
			"url":            remoteURLProp,
			"old_name":       oldNameProp,
			"new_name":       newNameProp,
		},
		[]string{"action"},
	)
}

// Repository discovery and management methods

// discoverRepositories scans the workspace for Git repositories
func (t *GitManagerTool) discoverRepositories() error {
	t.repoCacheMu.Lock()
	defer t.repoCacheMu.Unlock()

	// Check if we need to rescan
	if time.Since(t.lastScanTime) < t.scanInterval && len(t.repoCache) > 0 {
		return nil
	}

	// Clear existing cache
	t.repoCache = make(map[string]*GitRepositoryInfo)

	// Walk through workspace directory
	err := filepath.Walk(t.workspaceRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue walking even if there are errors
		}

		// Skip hidden directories except .git
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") && info.Name() != ".git" {
			return filepath.SkipDir
		}

		// Check if this is a .git directory
		if info.IsDir() && info.Name() == ".git" {
			repoRoot := filepath.Dir(path)
			relPath, err := filepath.Rel(t.workspaceRoot, repoRoot)
			if err != nil {
				return nil // Continue if we can't get relative path
			}

			// Check if this is a submodule
			isSubmodule := t.isSubmodule(repoRoot)

			t.repoCache[relPath] = &GitRepositoryInfo{
				Path:         repoRoot,
				RelativePath: relPath,
				IsSubmodule:  isSubmodule,
			}

			// Skip walking into .git directory
			return filepath.SkipDir
		}

		return nil
	})

	t.lastScanTime = time.Now()
	return err
}

// isSubmodule checks if a directory is a Git submodule
func (t *GitManagerTool) isSubmodule(repoPath string) bool {
	gitFile := filepath.Join(repoPath, ".git")
	if info, err := os.Stat(gitFile); err == nil && !info.IsDir() {
		// .git is a file, likely a submodule
		return true
	}
	return false
}

// findRepositoryForPath finds the Git repository that contains the given path
func (t *GitManagerTool) findRepositoryForPath(targetPath string) (*GitRepositoryInfo, error) {
	// Ensure repositories are discovered
	if err := t.discoverRepositories(); err != nil {
		return nil, fmt.Errorf("failed to discover repositories: %w", err)
	}

	t.repoCacheMu.RLock()
	defer t.repoCacheMu.RUnlock()

	// Clean and normalize the target path
	cleanPath := filepath.Clean(targetPath)
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Join(t.workspaceRoot, cleanPath)
	}

	// Find the repository that contains this path
	var bestMatch *GitRepositoryInfo
	var bestMatchLen int

	for _, repo := range t.repoCache {
		// Check if the target path is within this repository
		if strings.HasPrefix(cleanPath, repo.Path) {
			// Find the longest matching path (most specific repository)
			if len(repo.Path) > bestMatchLen {
				bestMatch = repo
				bestMatchLen = len(repo.Path)
			}
		}
	}

	if bestMatch == nil {
		return nil, fmt.Errorf("no Git repository found for path: %s", targetPath)
	}

	return bestMatch, nil
}

// resolveRepositoryPath resolves the repository path from parameters or auto-detects it
func (t *GitManagerTool) resolveRepositoryPath(params map[string]interface{}) (*GitRepositoryInfo, error) {
	// Check if repo_path is explicitly provided
	if repoPath, ok := params["repo_path"].(string); ok && repoPath != "" {
		// Validate the provided path
		if err := t.validatePath(repoPath); err != nil {
			return nil, fmt.Errorf("invalid repository path: %w", err)
		}

		// Convert to absolute path
		absPath := repoPath
		if !filepath.IsAbs(repoPath) {
			absPath = filepath.Join(t.workspaceRoot, repoPath)
		}

		return t.findRepositoryForPath(absPath)
	}

	// Auto-detect: try to find repository in current workspace
	if err := t.discoverRepositories(); err != nil {
		return nil, fmt.Errorf("failed to discover repositories: %w", err)
	}

	t.repoCacheMu.RLock()
	defer t.repoCacheMu.RUnlock()

	// If there's only one repository, use it
	if len(t.repoCache) == 1 {
		for _, repo := range t.repoCache {
			return repo, nil
		}
	}

	// If there are multiple repositories, prefer the root one
	if rootRepo, exists := t.repoCache["."]; exists {
		return rootRepo, nil
	}

	// If no root repository, return an error asking for explicit path
	if len(t.repoCache) > 1 {
		var repoPaths []string
		for relPath := range t.repoCache {
			repoPaths = append(repoPaths, relPath)
		}
		return nil, fmt.Errorf("multiple Git repositories found (%s). Please specify repo_path parameter", strings.Join(repoPaths, ", "))
	}

	// No repositories found
	return nil, fmt.Errorf("no Git repositories found in workspace")
}

// createExecutorForRepo creates a Git executor for the specified repository
func (t *GitManagerTool) createExecutorForRepo(repo *GitRepositoryInfo) GitExecutor {
	return NewDefaultGitExecutor(repo.Path, t.gitConfig.CommandTimeout, t.gitConfig.MaxOutputSize)
}

// Security validation methods
func (t *GitManagerTool) validatePath(path string) error {
	// Clean the path
	cleanPath := filepath.Clean(path)

	// Check for path traversal
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal detected: %s", path)
	}

	// Ensure path is relative and within working directory
	if filepath.IsAbs(cleanPath) {
		return fmt.Errorf("absolute paths not allowed: %s", path)
	}

	return nil
}

func (t *GitManagerTool) validateGitArgs(args []string) error { //nolint:unused
	dangerous := []string{"--exec", "--upload-pack", "--receive-pack", "--config"}
	for _, arg := range args {
		for _, danger := range dangerous {
			if strings.Contains(arg, danger) {
				return fmt.Errorf("dangerous git argument detected: %s", arg)
			}
		}
	}
	return nil
}

func (t *GitManagerTool) validateRemoteURL(url string) error {
	if url == "" {
		return nil
	}

	for _, pattern := range t.gitConfig.AllowedRemoteURLs {
		if t.matchURLPattern(pattern, url) {
			return nil
		}
	}

	return fmt.Errorf("remote URL not allowed: %s. Allowed patterns: %v", url, t.gitConfig.AllowedRemoteURLs)
}

// matchURLPattern matches URL patterns including SSH format
func (t *GitManagerTool) matchURLPattern(pattern, url string) bool {
	// Handle exact match
	if pattern == url {
		return true
	}

	// Handle wildcard patterns
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(url, prefix)
	}

	// Handle SSH format patterns like git@github.com:*
	if strings.Contains(pattern, "@") && strings.Contains(pattern, ":") {
		// Extract the host part from pattern and URL
		patternParts := strings.Split(pattern, ":")
		if len(patternParts) != 2 {
			return false
		}

		urlParts := strings.Split(url, ":")
		if len(urlParts) != 2 {
			return false
		}

		// Match the user@host part
		patternHost := patternParts[0]
		urlHost := urlParts[0]

		if patternHost == urlHost {
			// Match the path part with wildcard
			patternPath := patternParts[1]
			urlPath := urlParts[1]

			if patternPath == "*" {
				return true
			}

			if strings.HasSuffix(patternPath, "*") {
				prefix := strings.TrimSuffix(patternPath, "*")
				return strings.HasPrefix(urlPath, prefix)
			}

			return patternPath == urlPath
		}
	}

	return false
}

func (t *GitManagerTool) validateBranchName(name string) error {
	if name == "" {
		return fmt.Errorf("branch name cannot be empty")
	}

	// Git branch name validation
	invalidChars := regexp.MustCompile(`[~^:\s\[\]\\?*]`)
	if invalidChars.MatchString(name) {
		return fmt.Errorf("invalid branch name: %s. Branch names cannot contain spaces or special characters", name)
	}

	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("invalid branch name: %s. Branch names cannot start with '-' or end with '.'", name)
	}

	return nil
}

// Error handling methods
func (t *GitManagerTool) handleGitError(err error, output string) *GitError {
	if err == nil {
		return nil
	}

	// Categorize common Git errors
	switch {
	case strings.Contains(output, "not a git repository"):
		return &GitError{
			Type:    "not_git_repo",
			Message: "当前目录不是 Git 仓库",
			Code:    1001,
			Details: "请先初始化 Git 仓库或切换到正确的目录",
		}
	case strings.Contains(output, "Permission denied"):
		return &GitError{
			Type:    "permission_denied",
			Message: "权限不足",
			Code:    1002,
			Details: "请检查文件权限或 SSH 密钥配置",
		}
	case strings.Contains(output, "Connection refused"), strings.Contains(output, "Could not resolve hostname"):
		return &GitError{
			Type:    "network_error",
			Message: "网络连接失败",
			Code:    1003,
			Details: "请检查网络连接和远程仓库地址",
		}
	case strings.Contains(output, "nothing to commit"):
		return &GitError{
			Type:    "nothing_to_commit",
			Message: "没有需要提交的更改",
			Code:    1004,
			Details: "工作目录是干净的，没有修改的文件",
		}
	case strings.Contains(output, "merge conflict"):
		return &GitError{
			Type:    "merge_conflict",
			Message: "存在合并冲突",
			Code:    1005,
			Details: "请解决冲突后再次提交",
		}
	case strings.Contains(output, "branch already exists"):
		return &GitError{
			Type:    "branch_exists",
			Message: "分支已存在",
			Code:    1006,
			Details: "请使用不同的分支名称或切换到现有分支",
		}
	default:
		return &GitError{
			Type:    "unknown",
			Message: "Git 操作失败",
			Code:    1000,
			Details: "请检查命令参数和仓库状态",
		}
	}
}

// Cache management methods
func (t *GitManagerTool) getCachedResult(key string) (*interfaces.ToolResult, bool) {
	if !t.gitConfig.EnableCache {
		return nil, false
	}

	if cached, ok := t.cache.Load(key); ok {
		if entry, ok := cached.(*CacheEntry); ok {
			if time.Since(entry.Timestamp) < t.gitConfig.CacheExpiration {
				return entry.Result, true
			}
			// Remove expired entry
			t.cache.Delete(key)
		}
	}
	return nil, false
}

func (t *GitManagerTool) setCachedResult(key string, result *interfaces.ToolResult) {
	if !t.gitConfig.EnableCache {
		return
	}

	entry := &CacheEntry{
		Result:    result,
		Timestamp: time.Now(),
	}
	t.cache.Store(key, entry)
}

// Execute executes the git manager tool
func (t *GitManagerTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	if params == nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "tool parameters are missing",
			UserContent: "❌ Git 操作失败：缺少必要参数",
			LLMContent:  "git_manager failed: tool parameters are missing",
		}, nil
	}

	// Extract and validate action
	action, ok := params["action"].(string)
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "action parameter is required and must be a string",
			UserContent: "❌ Git 操作失败：需要指定操作类型",
			LLMContent:  "git_manager failed: action parameter is required",
		}, nil
	}

	// Validate action is allowed
	allowed := false
	for _, allowedAction := range t.gitConfig.AllowedCommands {
		if action == allowedAction {
			allowed = true
			break
		}
	}
	if !allowed {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("action '%s' is not allowed", action),
			UserContent: fmt.Sprintf("❌ 不允许的 Git 操作：%s", action),
			LLMContent:  fmt.Sprintf("git_manager failed: action '%s' not allowed", action),
		}, nil
	}

	// Special handling for init and clone operations
	var repo *GitRepositoryInfo
	var executor GitExecutor
	var err error

	if action == "init" || action == "clone" {
		// For init and clone, we need to handle the target directory differently
		var targetPath string
		if repoPath, ok := params["repo_path"].(string); ok && repoPath != "" {
			// Validate the provided path
			if err := t.validatePath(repoPath); err != nil {
				return &interfaces.ToolResult{
					Success:     false,
					Error:       fmt.Sprintf("invalid repository path: %s", err.Error()),
					UserContent: fmt.Sprintf("❌ Git 操作失败：无效的仓库路径：%s", err.Error()),
					LLMContent:  fmt.Sprintf("git_manager failed: invalid repository path: %s", err.Error()),
				}, nil
			}

			// Convert to absolute path
			if filepath.IsAbs(repoPath) {
				targetPath = repoPath
			} else {
				targetPath = filepath.Join(t.workspaceRoot, repoPath)
			}
		} else {
			// Use workspace root as default
			targetPath = t.workspaceRoot
		}

		// Create executor for the target directory
		executor = NewDefaultGitExecutor(targetPath, t.gitConfig.CommandTimeout, t.gitConfig.MaxOutputSize)

		// Create a temporary repo info for result metadata
		relPath, _ := filepath.Rel(t.workspaceRoot, targetPath)
		if relPath == "." {
			relPath = ""
		}
		repo = &GitRepositoryInfo{
			Path:         targetPath,
			RelativePath: relPath,
			IsSubmodule:  false,
		}
	} else {
		// For other operations, resolve repository path normally
		repo, err = t.resolveRepositoryPath(params)
		if err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       err.Error(),
				UserContent: fmt.Sprintf("❌ Git 操作失败：%s", err.Error()),
				LLMContent:  fmt.Sprintf("git_manager failed: %s", err.Error()),
			}, nil
		}

		// Create executor for the specific repository
		executor = t.createExecutorForRepo(repo)
	}

	// Check cache for read operations
	if action == "status" || action == "log" {
		cacheKey := fmt.Sprintf("%s_%s", action, repo.Path)
		if cached, found := t.getCachedResult(cacheKey); found {
			return cached, nil
		}
	}

	// Route to specific action handlers
	var result *interfaces.ToolResult

	switch action {
	case "clone":
		result, err = t.cloneRepository(ctx, params, executor)
	case "init":
		result, err = t.initRepository(ctx, params, executor)
	case "status":
		result, err = t.getStatus(ctx, params, executor)
	case "add":
		result, err = t.addFiles(ctx, params, executor)
	case "commit":
		result, err = t.commitChanges(ctx, params, executor)
	case "push":
		result, err = t.pushChanges(ctx, params, executor)
	case "pull":
		result, err = t.pullChanges(ctx, params, executor)
	case "branch":
		result, err = t.manageBranches(ctx, params, executor)
	case "checkout":
		result, err = t.checkoutBranch(ctx, params, executor)
	case "merge":
		result, err = t.mergeBranch(ctx, params, executor)
	case "log":
		result, err = t.getCommitLog(ctx, params, executor)
	case "remote":
		result, err = t.manageRemotes(ctx, params, executor)
	default:
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("unsupported action: %s", action),
			UserContent: fmt.Sprintf("❌ 不支持的 Git 操作：%s", action),
			LLMContent:  fmt.Sprintf("git_manager failed: unsupported action %s", action),
		}, nil
	}

	// Cache successful read operations
	if err == nil && result != nil && result.Success && (action == "status" || action == "log") {
		cacheKey := fmt.Sprintf("%s_%s", action, repo.Path)
		t.setCachedResult(cacheKey, result)
	}

	// Add repository information to result data
	if result != nil && result.Data != nil {
		if data, ok := result.Data.(map[string]interface{}); ok {
			data["repository_path"] = repo.RelativePath
			data["repository_absolute_path"] = repo.Path
			data["is_submodule"] = repo.IsSubmodule
		}
	}

	return result, err
}

// Specific Git operation implementations

func (t *GitManagerTool) cloneRepository(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	repoURL, ok := params["repo_url"].(string)
	if !ok || repoURL == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "repo_url parameter is required for clone operation",
			UserContent: "❌ 克隆失败：需要提供仓库 URL",
			LLMContent:  "git clone failed: repo_url parameter is required",
		}, nil
	}

	// Validate remote URL
	if err := t.validateRemoteURL(repoURL); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ 克隆失败：%s", err.Error()),
			LLMContent:  fmt.Sprintf("git clone failed: %s", err.Error()),
		}, nil
	}

	// Get target directory
	targetDir := ""
	if td, ok := params["target_dir"].(string); ok {
		targetDir = td
	}

	// Validate target directory path
	if targetDir != "" {
		if err := t.validatePath(targetDir); err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       err.Error(),
				UserContent: fmt.Sprintf("❌ 克隆失败：目标目录无效 - %s", err.Error()),
				LLMContent:  fmt.Sprintf("git clone failed: invalid target directory - %s", err.Error()),
			}, nil
		}
	}

	// Build git clone command
	args := []string{"clone", repoURL}
	if targetDir != "" {
		args = append(args, targetDir)
	}

	// Execute git clone
	result, err := executor.Execute(ctx, args...)
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 克隆失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git clone failed: %s", gitErr.Error()),
		}, nil
	}

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: fmt.Sprintf("✅ 成功克隆仓库：%s", repoURL),
		LLMContent:  fmt.Sprintf("Successfully cloned repository: %s", repoURL),
		Data:        map[string]interface{}{"repo_url": repoURL, "target_dir": targetDir},
	}, nil
}

func (t *GitManagerTool) initRepository(ctx context.Context, _ map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	// Execute git init
	result, err := executor.Execute(ctx, "init")
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 初始化失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git init failed: %s", gitErr.Error()),
		}, nil
	}

	// Clear repository cache to force rediscovery of repositories
	t.repoCacheMu.Lock()
	t.repoCache = make(map[string]*GitRepositoryInfo)
	t.lastScanTime = time.Time{}
	t.repoCacheMu.Unlock()

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: "✅ 成功初始化 Git 仓库",
		LLMContent:  "Successfully initialized Git repository",
		Data:        map[string]interface{}{},
	}, nil
}

func (t *GitManagerTool) getStatus(ctx context.Context, _ map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	// Execute git status
	result, err := executor.Execute(ctx, "status", "--porcelain")
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 获取状态失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git status failed: %s", gitErr.Error()),
		}, nil
	}

	// Parse status output
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	var modified, added, deleted, untracked []string

	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		status := line[:2]
		filename := line[3:]

		switch {
		case strings.Contains(status, "M"):
			modified = append(modified, filename)
		case strings.Contains(status, "A"):
			added = append(added, filename)
		case strings.Contains(status, "D"):
			deleted = append(deleted, filename)
		case strings.Contains(status, "??"):
			untracked = append(untracked, filename)
		}
	}

	// Get current branch
	branchResult, err := executor.Execute(ctx, "branch", "--show-current")
	currentBranch := "unknown"
	if err == nil && branchResult.ExitCode == 0 {
		currentBranch = strings.TrimSpace(branchResult.Output)
	}

	// Check if working directory is clean
	isClean := len(modified) == 0 && len(added) == 0 && len(deleted) == 0 && len(untracked) == 0

	statusData := map[string]interface{}{
		"clean":          isClean,
		"current_branch": currentBranch,
		"modified":       modified,
		"added":          added,
		"deleted":        deleted,
		"untracked":      untracked,
		"total_changes":  len(modified) + len(added) + len(deleted) + len(untracked),
	}

	var userContent string
	if isClean {
		userContent = fmt.Sprintf("✅ 工作目录干净\n📍 当前分支：%s", currentBranch)
	} else {
		userContent = fmt.Sprintf("📍 当前分支：%s\n", currentBranch)
		if len(modified) > 0 {
			userContent += fmt.Sprintf("📝 已修改：%d 个文件", len(modified))
			if len(modified) <= 3 {
				userContent += fmt.Sprintf("（%s）", strings.Join(modified, ", "))
			}
			userContent += "\n"
		}
		if len(added) > 0 {
			userContent += fmt.Sprintf("➕ 已暂存：%d 个文件", len(added))
			if len(added) <= 3 {
				userContent += fmt.Sprintf("（%s）", strings.Join(added, ", "))
			}
			userContent += "\n"
		}
		if len(deleted) > 0 {
			userContent += fmt.Sprintf("🗑️ 已删除：%d 个文件", len(deleted))
			if len(deleted) <= 3 {
				userContent += fmt.Sprintf("（%s）", strings.Join(deleted, ", "))
			}
			userContent += "\n"
		}
		if len(untracked) > 0 {
			userContent += fmt.Sprintf("❓ 未跟踪：%d 个文件", len(untracked))
			if len(untracked) <= 3 {
				userContent += fmt.Sprintf("（%s）", strings.Join(untracked, ", "))
			}
		}
	}

	// Create detailed LLM content with file lists
	var llmContent string
	if isClean {
		llmContent = fmt.Sprintf("Git repository status: CLEAN. Branch: %s. Working directory has no changes.", currentBranch)
	} else {
		llmContent = fmt.Sprintf("Git repository status: DIRTY. Branch: %s. Changes detected:", currentBranch)
		if len(modified) > 0 {
			llmContent += fmt.Sprintf(" Modified files (%d): %s.", len(modified), strings.Join(modified[:min(len(modified), 5)], ", "))
			if len(modified) > 5 {
				llmContent += fmt.Sprintf(" (and %d more)", len(modified)-5)
			}
		}
		if len(added) > 0 {
			llmContent += fmt.Sprintf(" Staged files (%d): %s.", len(added), strings.Join(added[:min(len(added), 5)], ", "))
			if len(added) > 5 {
				llmContent += fmt.Sprintf(" (and %d more)", len(added)-5)
			}
		}
		if len(deleted) > 0 {
			llmContent += fmt.Sprintf(" Deleted files (%d): %s.", len(deleted), strings.Join(deleted[:min(len(deleted), 5)], ", "))
			if len(deleted) > 5 {
				llmContent += fmt.Sprintf(" (and %d more)", len(deleted)-5)
			}
		}
		if len(untracked) > 0 {
			llmContent += fmt.Sprintf(" Untracked files (%d): %s.", len(untracked), strings.Join(untracked[:min(len(untracked), 5)], ", "))
			if len(untracked) > 5 {
				llmContent += fmt.Sprintf(" (and %d more)", len(untracked)-5)
			}
		}
		llmContent += fmt.Sprintf(" Total changes: %d files affected.", len(modified)+len(added)+len(deleted)+len(untracked))
	}

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: userContent,
		LLMContent:  llmContent,
		Data:        statusData,
	}, nil
}

func (t *GitManagerTool) addFiles(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	// Get files to add
	files := []string{"."}
	if filesParam, ok := params["files"]; ok {
		if filesList, ok := filesParam.([]interface{}); ok {
			files = make([]string, len(filesList))
			for i, f := range filesList {
				if fileStr, ok := f.(string); ok {
					// Validate each file path
					if fileStr != "." {
						if err := t.validatePath(fileStr); err != nil {
							return &interfaces.ToolResult{
								Success:     false,
								Error:       err.Error(),
								UserContent: fmt.Sprintf("❌ 添加失败：文件路径无效 - %s", err.Error()),
								LLMContent:  fmt.Sprintf("git add failed: invalid file path - %s", err.Error()),
							}, nil
						}
					}
					files[i] = fileStr
				}
			}
		}
	}

	// Build git add command
	args := append([]string{"add"}, files...)

	// Execute git add
	result, err := executor.Execute(ctx, args...)
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 添加失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git add failed: %s", gitErr.Error()),
		}, nil
	}

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: fmt.Sprintf("✅ 成功添加文件：%s", strings.Join(files, ", ")),
		LLMContent:  fmt.Sprintf("Successfully added files: %s", strings.Join(files, ", ")),
		Data:        map[string]interface{}{"files": files},
	}, nil
}

func (t *GitManagerTool) commitChanges(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	// Get commit message
	message, ok := params["commit_message"].(string)
	if !ok || message == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "commit_message parameter is required for commit operation",
			UserContent: "❌ 提交失败：需要提供提交信息",
			LLMContent:  "git commit failed: commit_message parameter is required",
		}, nil
	}

	// Execute git commit
	result, err := executor.Execute(ctx, "commit", "-m", message)
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 提交失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git commit failed: %s", gitErr.Error()),
		}, nil
	}

	// Get the commit hash from the output
	commitHash := ""
	if strings.Contains(result.Output, "[") && strings.Contains(result.Output, "]") {
		// Extract commit hash from output like "[main 1234567] commit message"
		parts := strings.Fields(result.Output)
		for _, part := range parts {
			if strings.HasSuffix(part, "]") {
				commitHash = strings.TrimSuffix(part, "]")
				break
			}
		}
	}

	// Get current branch name
	branchResult, _ := executor.Execute(ctx, "branch", "--show-current")
	currentBranch := strings.TrimSpace(branchResult.Output)
	if currentBranch == "" {
		currentBranch = "unknown"
	}

	// Get commit statistics (files changed, insertions, deletions)
	statsResult, _ := executor.Execute(ctx, "show", "--stat", "--format=", "HEAD")
	var filesChanged, insertions, deletions int
	if statsResult != nil && statsResult.ExitCode == 0 {
		lines := strings.Split(statsResult.Output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "changed") {
				// Parse line like "3 files changed, 15 insertions(+), 2 deletions(-)"
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "files" && i > 0 {
						if num, err := strconv.Atoi(parts[i-1]); err == nil {
							filesChanged = num
						}
					}
					if strings.Contains(part, "insertion") && i > 0 {
						if num, err := strconv.Atoi(parts[i-1]); err == nil {
							insertions = num
						}
					}
					if strings.Contains(part, "deletion") && i > 0 {
						if num, err := strconv.Atoi(parts[i-1]); err == nil {
							deletions = num
						}
					}
				}
				break
			}
		}
	}

	// Get commit timestamp
	timestampResult, _ := executor.Execute(ctx, "show", "-s", "--format=%ci", "HEAD")
	commitTime := "unknown"
	if timestampResult != nil && timestampResult.ExitCode == 0 {
		commitTime = strings.TrimSpace(timestampResult.Output)
	}

	// Create detailed LLM content
	llmContent := fmt.Sprintf("Git commit successful. Branch: %s, Hash: %s, Message: \"%s\", Timestamp: %s",
		currentBranch, commitHash, message, commitTime)
	if filesChanged > 0 {
		llmContent += fmt.Sprintf(", Files changed: %d", filesChanged)
		if insertions > 0 {
			llmContent += fmt.Sprintf(", Insertions: %d", insertions)
		}
		if deletions > 0 {
			llmContent += fmt.Sprintf(", Deletions: %d", deletions)
		}
	}
	llmContent += "."

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: fmt.Sprintf("✅ 成功提交到分支 %s：%s\n📝 提交哈希：%s", currentBranch, message, commitHash),
		LLMContent:  llmContent,
		Data: map[string]interface{}{
			"commit_message": message,
			"commit_hash":    commitHash,
			"branch":         currentBranch,
			"timestamp":      commitTime,
			"files_changed":  filesChanged,
			"insertions":     insertions,
			"deletions":      deletions,
		},
	}, nil
}

// manageRemotes handles remote repository operations
func (t *GitManagerTool) manageRemotes(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	// Get the subcommand (default to "list" if not specified)
	subcommand := "list"
	if sub, ok := params["subcommand"].(string); ok && sub != "" {
		subcommand = sub
	}

	switch subcommand {
	case "list", "-v", "verbose":
		return t.listRemotes(ctx, executor)
	case "add":
		return t.addRemote(ctx, params, executor)
	case "remove", "rm":
		return t.removeRemote(ctx, params, executor)
	case "rename":
		return t.renameRemote(ctx, params, executor)
	case "get-url":
		return t.getRemoteURL(ctx, params, executor)
	case "set-url":
		return t.setRemoteURL(ctx, params, executor)
	default:
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Unsupported remote subcommand: %s", subcommand),
		}, nil
	}
}

// listRemotes lists all remote repositories with their URLs
func (t *GitManagerTool) listRemotes(ctx context.Context, executor GitExecutor) (*interfaces.ToolResult, error) {
	result, err := executor.Execute(ctx, "remote", "-v")
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to list remotes: %v", err),
		}, nil
	}

	if result.ExitCode != 0 {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Git remote command failed: %s", result.Output),
		}, nil
	}

	// Parse the output to extract remote information
	remotes := make(map[string]map[string]string)
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 3 {
			name := parts[0]
			url := parts[1]
			operation := strings.Trim(parts[2], "()")

			if remotes[name] == nil {
				remotes[name] = make(map[string]string)
			}
			remotes[name][operation] = url
		}
	}

	// Convert to array format for easier consumption
	remotesArray := make([]map[string]string, 0, len(remotes))
	userContentBuilder := strings.Builder{}

	for name, urls := range remotes {
		// Use fetch URL if available, otherwise use push URL
		url := ""
		if fetchURL, ok := urls["fetch"]; ok {
			url = fetchURL
		} else if pushURL, ok := urls["push"]; ok {
			url = pushURL
		}

		if url != "" {
			remotesArray = append(remotesArray, map[string]string{
				"name": name,
				"url":  url,
			})
		}
	}

	defaultRemote := ""
	if t.gitConfig != nil {
		defaultRemote = t.gitConfig.DefaultRemote
	}
	sort.Slice(remotesArray, func(i, j int) bool {
		a := remotesArray[i]["name"]
		b := remotesArray[j]["name"]

		if defaultRemote != "" {
			if a == defaultRemote && b != defaultRemote {
				return true
			}
			if b == defaultRemote && a != defaultRemote {
				return false
			}
		}

		return a < b
	})

	// Set appropriate user content based on whether remotes exist
	if len(remotesArray) == 0 {
		userContentBuilder.WriteString("📋 No remotes configured")
	} else {
		userContentBuilder.WriteString("📋 远程仓库列表:\n")
		for _, remote := range remotesArray {
			fmt.Fprintf(&userContentBuilder, "  %s: %s\n", remote["name"], remote["url"])
		}
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"remotes":    remotesArray,
			"raw_output": result.Output,
		},
		LLMContent:  fmt.Sprintf("Git remote list retrieved successfully. Found %d remotes.", len(remotesArray)),
		UserContent: userContentBuilder.String(),
	}, nil
}

// addRemote adds a new remote repository
func (t *GitManagerTool) addRemote(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	name, ok := params["name"].(string)
	if !ok || name == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "Remote name is required for add operation",
		}, nil
	}

	url, ok := params["url"].(string)
	if !ok || url == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "Remote URL is required for add operation",
		}, nil
	}

	// Validate the remote URL
	if err := t.validateRemoteURL(url); err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Invalid remote URL: %v", err),
		}, nil
	}

	result, err := executor.Execute(ctx, "remote", "add", name, url)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to add remote: %v", err),
		}, nil
	}

	if result.ExitCode != 0 {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Git remote add failed: %s", result.Output),
		}, nil
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"name": name,
			"url":  url,
		},
		LLMContent:  fmt.Sprintf("Remote '%s' added successfully", name),
		UserContent: fmt.Sprintf("✅ 成功添加远程仓库 %s: %s", name, url),
	}, nil
}

// removeRemote removes a remote repository
func (t *GitManagerTool) removeRemote(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	name, ok := params["name"].(string)
	if !ok || name == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "Remote name is required for remove operation",
		}, nil
	}

	result, err := executor.Execute(ctx, "remote", "remove", name)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to remove remote: %v", err),
		}, nil
	}

	if result.ExitCode != 0 {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Git remote remove failed: %s", result.Output),
		}, nil
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"name": name,
		},
		LLMContent:  fmt.Sprintf("Remote '%s' removed successfully", name),
		UserContent: fmt.Sprintf("✅ 成功删除远程仓库 %s", name),
	}, nil
}

// renameRemote renames a remote repository
func (t *GitManagerTool) renameRemote(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	oldName, ok := params["old_name"].(string)
	if !ok || oldName == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "Old remote name is required for rename operation",
		}, nil
	}

	newName, ok := params["new_name"].(string)
	if !ok || newName == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "New remote name is required for rename operation",
		}, nil
	}

	result, err := executor.Execute(ctx, "remote", "rename", oldName, newName)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to rename remote: %v", err),
		}, nil
	}

	if result.ExitCode != 0 {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Git remote rename failed: %s", result.Output),
		}, nil
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"old_name": oldName,
			"new_name": newName,
		},
		LLMContent:  fmt.Sprintf("Remote renamed from '%s' to '%s' successfully", oldName, newName),
		UserContent: fmt.Sprintf("✅ 成功重命名远程仓库 %s 为 %s", oldName, newName),
	}, nil
}

// getDefaultBranch detects the default branch of the repository
func (t *GitManagerTool) getDefaultBranch(ctx context.Context, executor GitExecutor) (string, error) {
	// First try to get the default branch from remote
	result, err := executor.Execute(ctx, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil && result.ExitCode == 0 {
		// Extract branch name from refs/remotes/origin/HEAD -> refs/remotes/origin/main
		output := strings.TrimSpace(result.Output)
		if strings.HasPrefix(output, "refs/remotes/origin/") {
			return strings.TrimPrefix(output, "refs/remotes/origin/"), nil
		}
	}

	// If that fails, try to get it from remote show
	result, err = executor.Execute(ctx, "remote", "show", "origin")
	if err == nil && result.ExitCode == 0 {
		lines := strings.Split(result.Output, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "HEAD branch:") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					return parts[2], nil
				}
			}
		}
	}

	// Fallback: check common default branch names
	commonDefaults := []string{"main", "master", "develop"}
	for _, branch := range commonDefaults {
		result, err := executor.Execute(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
		if err == nil && result.ExitCode == 0 {
			return branch, nil
		}
	}

	// Last resort: get the first branch
	result, err = executor.Execute(ctx, "branch", "--format=%(refname:short)")
	if err == nil && result.ExitCode == 0 {
		lines := strings.Split(strings.TrimSpace(result.Output), "\n")
		if len(lines) > 0 && lines[0] != "" {
			return lines[0], nil
		}
	}

	return "", fmt.Errorf("无法检测默认分支")
}

func (t *GitManagerTool) pushChanges(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	// Get remote name
	remote := t.gitConfig.DefaultRemote
	if r, ok := params["remote"].(string); ok && r != "" {
		remote = r
	}

	// Get current branch name
	branchResult, _ := executor.Execute(ctx, "branch", "--show-current")
	currentBranch := strings.TrimSpace(branchResult.Output)

	// Get branch name (optional)
	var args []string
	targetBranch := currentBranch
	if branch, ok := params["branch_name"].(string); ok && branch != "" {
		if err := t.validateBranchName(branch); err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       err.Error(),
				UserContent: fmt.Sprintf("❌ 推送失败：%s", err.Error()),
				LLMContent:  fmt.Sprintf("git push failed: %s", err.Error()),
			}, nil
		}
		targetBranch = branch
		args = []string{"push", remote, branch}
	} else {
		// If no target branch specified, check if current branch has upstream tracking
		_, err := executor.Execute(ctx, "rev-parse", "--abbrev-ref", currentBranch+"@{upstream}")
		if err != nil {
			// No upstream set, try to detect the appropriate default branch
			defaultBranch, err := t.getDefaultBranch(ctx, executor)
			if err == nil && defaultBranch != "" && defaultBranch != currentBranch {
				// Suggest using the detected default branch if different from current
				return &interfaces.ToolResult{
					Success:     false,
					Error:       fmt.Sprintf("no upstream branch set for '%s'. Consider specifying target branch explicitly or set upstream with: git push -u %s %s", currentBranch, remote, currentBranch),
					UserContent: fmt.Sprintf("❌ 当前分支 '%s' 没有设置上游分支\n💡 建议：明确指定目标分支或使用 git push -u %s %s 设置上游分支\n🔍 检测到远程默认分支为: %s", currentBranch, remote, currentBranch, defaultBranch),
					LLMContent:  fmt.Sprintf("No upstream branch set for '%s'. Detected remote default branch: %s. Consider setting upstream or specifying target branch explicitly.", currentBranch, defaultBranch),
				}, nil
			}
		}
		args = []string{"push", remote}
	}

	// Execute git push
	result, err := executor.Execute(ctx, args...)
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 推送失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git push failed: %s", gitErr.Error()),
		}, nil
	}

	// Parse push output for additional information
	var pushedCommits int
	var remoteURL string

	// Count commits pushed (look for "x commits" in output)
	if strings.Contains(result.Output, "->") {
		lines := strings.Split(result.Output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "..") && strings.Contains(line, "->") {
				// Try to extract commit range
				parts := strings.Fields(line)
				for _, part := range parts {
					if strings.Contains(part, "..") {
						// This indicates a range of commits
						pushedCommits = 1 // At least one commit
						break
					}
				}
			}
		}
	}

	// Get remote URL for display
	remoteResult, _ := executor.Execute(ctx, "remote", "get-url", remote)
	if remoteResult != nil && remoteResult.ExitCode == 0 {
		remoteURL = strings.TrimSpace(remoteResult.Output)
	}

	userContent := fmt.Sprintf("✅ 成功推送分支 %s 到远程 %s", targetBranch, remote)
	if remoteURL != "" {
		userContent += fmt.Sprintf("\n🔗 远程地址：%s", remoteURL)
	}
	if pushedCommits > 0 {
		userContent += fmt.Sprintf("\n📤 推送了 %d 个提交", pushedCommits)
	}

	// Create detailed LLM content with network operation details
	llmContent := fmt.Sprintf("Git push operation completed successfully. Branch: %s, Remote: %s", targetBranch, remote)
	if remoteURL != "" {
		llmContent += fmt.Sprintf(", Remote URL: %s", remoteURL)
	}
	if pushedCommits > 0 {
		llmContent += fmt.Sprintf(", Commits pushed: %d", pushedCommits)
	}

	// Add push operation type analysis
	if strings.Contains(result.Output, "fast-forward") {
		llmContent += ", Push type: fast-forward"
	} else if strings.Contains(result.Output, "up-to-date") {
		llmContent += ", Push type: up-to-date (no changes)"
	} else if strings.Contains(result.Output, "rejected") {
		llmContent += ", Push type: rejected (needs pull first)"
	} else {
		llmContent += ", Push type: standard"
	}

	// Add bandwidth/transfer info if available
	if strings.Contains(result.Output, "Counting objects") {
		llmContent += ", Transfer: objects counted and compressed"
	}

	llmContent += "."

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: userContent,
		LLMContent:  llmContent,
		Data: map[string]interface{}{
			"remote":         remote,
			"branch":         targetBranch,
			"remote_url":     remoteURL,
			"pushed_commits": pushedCommits,
			"push_type":      getPushType(result.Output),
		},
	}, nil
}

func (t *GitManagerTool) pullChanges(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	// Get remote name
	remote := t.gitConfig.DefaultRemote
	if r, ok := params["remote"].(string); ok && r != "" {
		remote = r
	}

	// Get current branch
	branchResult, err := executor.Execute(ctx, "branch", "--show-current")
	currentBranch := "unknown"
	if err == nil && branchResult.ExitCode == 0 {
		currentBranch = strings.TrimSpace(branchResult.Output)
	}

	// Execute git pull
	result, err := executor.Execute(ctx, "pull", remote)
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 拉取失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git pull failed: %s", gitErr.Error()),
		}, nil
	}

	// Parse pull output for more details
	output := result.Output
	var filesChanged, insertions, deletions int

	// Extract file changes information
	if strings.Contains(output, "files changed") {
		re := regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)
		matches := re.FindStringSubmatch(output)
		if len(matches) > 1 {
			if f, err := strconv.Atoi(matches[1]); err == nil {
				filesChanged = f
			}
			if len(matches) > 2 && matches[2] != "" {
				if i, err := strconv.Atoi(matches[2]); err == nil {
					insertions = i
				}
			}
			if len(matches) > 3 && matches[3] != "" {
				if d, err := strconv.Atoi(matches[3]); err == nil {
					deletions = d
				}
			}
		}
	}

	// Check if it was already up to date
	upToDate := strings.Contains(output, "Already up to date") || strings.Contains(output, "Already up-to-date")

	var userContent, llmContent string
	pullData := map[string]interface{}{
		"remote":         remote,
		"current_branch": currentBranch,
		"up_to_date":     upToDate,
		"files_changed":  filesChanged,
		"insertions":     insertions,
		"deletions":      deletions,
	}

	if upToDate {
		userContent = fmt.Sprintf("✅ 已是最新版本，无需更新\n📍 当前分支：%s\n🔗 远程：%s", currentBranch, remote)
		llmContent = fmt.Sprintf("Git pull operation: already up-to-date. Branch: %s, Remote: %s. No changes received.", currentBranch, remote)
	} else {
		userContent = fmt.Sprintf("✅ 成功从 %s 拉取更新\n📍 当前分支：%s", remote, currentBranch)
		if filesChanged > 0 {
			userContent += fmt.Sprintf("\n📁 文件变更：%d 个", filesChanged)
			if insertions > 0 {
				userContent += fmt.Sprintf("\n➕ 新增：%d 行", insertions)
			}
			if deletions > 0 {
				userContent += fmt.Sprintf("\n➖ 删除：%d 行", deletions)
			}
		}

		// Create detailed LLM content for pull with changes
		llmContent = fmt.Sprintf("Git pull operation completed successfully. Branch: %s, Remote: %s", currentBranch, remote)
		if filesChanged > 0 {
			llmContent += fmt.Sprintf(", Files changed: %d", filesChanged)
			if insertions > 0 {
				llmContent += fmt.Sprintf(", Lines added: %d", insertions)
			}
			if deletions > 0 {
				llmContent += fmt.Sprintf(", Lines removed: %d", deletions)
			}
		}

		// Add merge type analysis
		if strings.Contains(output, "fast-forward") {
			llmContent += ", Merge type: fast-forward"
		} else if strings.Contains(output, "Merge made by") {
			llmContent += ", Merge type: merge commit created"
		} else if strings.Contains(output, "CONFLICT") {
			llmContent += ", Merge type: conflicts detected (manual resolution required)"
		} else {
			llmContent += ", Merge type: standard"
		}

		// Add network transfer info
		if strings.Contains(output, "Receiving objects") {
			llmContent += ", Transfer: objects received and unpacked"
		}

		llmContent += "."
	}

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: userContent,
		LLMContent:  llmContent,
		Data:        pullData,
	}, nil
}

func (t *GitManagerTool) manageBranches(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	// Get branch name for creation
	if branchName, ok := params["branch_name"].(string); ok && branchName != "" {
		if err := t.validateBranchName(branchName); err != nil {
			return &interfaces.ToolResult{
				Success:     false,
				Error:       err.Error(),
				UserContent: fmt.Sprintf("❌ 分支操作失败：%s", err.Error()),
				LLMContent:  fmt.Sprintf("git branch failed: %s", err.Error()),
			}, nil
		}

		// Create new branch
		result, err := executor.Execute(ctx, "branch", branchName)
		if err != nil || result.ExitCode != 0 {
			gitErr := t.handleGitError(err, result.Output)
			return &interfaces.ToolResult{
				Success:     false,
				Error:       gitErr.Error(),
				UserContent: fmt.Sprintf("❌ 创建分支失败：%s", gitErr.Message),
				LLMContent:  fmt.Sprintf("git branch failed: %s", gitErr.Error()),
			}, nil
		}

		return &interfaces.ToolResult{
			Success:     true,
			UserContent: fmt.Sprintf("✅ 成功创建分支：%s", branchName),
			LLMContent:  fmt.Sprintf("Successfully created branch: %s", branchName),
			Data:        map[string]interface{}{"branch_name": branchName},
		}, nil
	}

	// List branches
	result, err := executor.Execute(ctx, "branch", "-a")
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 获取分支列表失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git branch failed: %s", gitErr.Error()),
		}, nil
	}

	// Parse branch list
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	var branches []string
	var currentBranch string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "*") {
			currentBranch = strings.TrimSpace(line[1:])
			branches = append(branches, currentBranch+" (current)")
		} else {
			branches = append(branches, line)
		}
	}

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: fmt.Sprintf("🌿 分支列表：\n%s", strings.Join(branches, "\n")),
		LLMContent:  fmt.Sprintf("Branch list retrieved. Current branch: %s", currentBranch),
		Data:        map[string]interface{}{"branches": branches, "current_branch": currentBranch},
	}, nil
}

func (t *GitManagerTool) checkoutBranch(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	branchName, ok := params["branch_name"].(string)
	if !ok || branchName == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "branch_name parameter is required for checkout operation",
			UserContent: "❌ 切换失败：需要提供分支名称",
			LLMContent:  "git checkout failed: branch_name parameter is required",
		}, nil
	}

	if err := t.validateBranchName(branchName); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ 切换失败：%s", err.Error()),
			LLMContent:  fmt.Sprintf("git checkout failed: %s", err.Error()),
		}, nil
	}

	// Execute git checkout
	result, err := executor.Execute(ctx, "checkout", branchName)
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 切换分支失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git checkout failed: %s", gitErr.Error()),
		}, nil
	}

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: fmt.Sprintf("✅ 成功切换到分支：%s", branchName),
		LLMContent:  fmt.Sprintf("Successfully checked out branch: %s", branchName),
		Data:        map[string]interface{}{"branch_name": branchName},
	}, nil
}

func (t *GitManagerTool) mergeBranch(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	branchName, ok := params["branch_name"].(string)
	if !ok || branchName == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "branch_name parameter is required for merge operation",
			UserContent: "❌ 合并失败：需要提供分支名称",
			LLMContent:  "git merge failed: branch_name parameter is required",
		}, nil
	}

	if err := t.validateBranchName(branchName); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       err.Error(),
			UserContent: fmt.Sprintf("❌ 合并失败：%s", err.Error()),
			LLMContent:  fmt.Sprintf("git merge failed: %s", err.Error()),
		}, nil
	}

	// Execute git merge
	result, err := executor.Execute(ctx, "merge", branchName)
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 合并分支失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git merge failed: %s", gitErr.Error()),
		}, nil
	}

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: fmt.Sprintf("✅ 成功合并分支：%s", branchName),
		LLMContent:  fmt.Sprintf("Successfully merged branch: %s", branchName),
		Data:        map[string]interface{}{"branch_name": branchName},
	}, nil
}

func (t *GitManagerTool) getCommitLog(ctx context.Context, _ map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	// Execute git log with detailed formatting
	result, err := executor.Execute(ctx, "log", "--pretty=format:%H|%h|%an|%ae|%ad|%s", "--date=iso", "-10")
	if err != nil || result.ExitCode != 0 {
		gitErr := t.handleGitError(err, result.Output)
		return &interfaces.ToolResult{
			Success:     false,
			Error:       gitErr.Error(),
			UserContent: fmt.Sprintf("❌ 获取提交日志失败：%s", gitErr.Message),
			LLMContent:  fmt.Sprintf("git log failed: %s", gitErr.Error()),
		}, nil
	}

	// Parse commit log with detailed information
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	var commits []map[string]string

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) == 6 {
			commits = append(commits, map[string]string{
				"full_hash":    parts[0],
				"short_hash":   parts[1],
				"author_name":  parts[2],
				"author_email": parts[3],
				"date":         parts[4],
				"message":      parts[5],
			})
		}
	}

	userContent := "📜 最近的提交记录：\n"
	for _, commit := range commits {
		userContent += fmt.Sprintf("• %s (%s) - %s\n  👤 %s <%s>\n  📅 %s\n\n",
			commit["short_hash"], commit["message"], commit["author_name"],
			commit["author_name"], commit["author_email"], commit["date"])
	}

	// Create detailed summary for LLM with enhanced statistics
	var authors []string
	authorMap := make(map[string]bool)
	var totalCommitsToday, totalCommitsThisWeek int
	now := time.Now()
	today := now.Format("2006-01-02")
	weekAgo := now.AddDate(0, 0, -7).Format("2006-01-02")

	for _, commit := range commits {
		// Count unique authors
		authorName := commit["author_name"]
		if !authorMap[authorName] {
			authors = append(authors, authorName)
			authorMap[authorName] = true
		}

		// Count commits by time period
		commitDate := commit["date"]
		if strings.Contains(commitDate, today) {
			totalCommitsToday++
		}
		if commitDate >= weekAgo {
			totalCommitsThisWeek++
		}
	}

	// Calculate commit frequency and patterns
	var commitPattern string
	if len(commits) > 0 {
		if totalCommitsToday > 0 {
			commitPattern = fmt.Sprintf("Active today (%d commits)", totalCommitsToday)
		} else if totalCommitsThisWeek > 0 {
			commitPattern = fmt.Sprintf("Active this week (%d commits)", totalCommitsThisWeek)
		} else {
			commitPattern = "Less active recently"
		}
	}

	// Enhanced LLM content with detailed analysis
	llmContent := fmt.Sprintf("Git commit log analysis: Retrieved %d commits from %d unique contributors (%s)",
		len(commits), len(authors), strings.Join(authors, ", "))

	if len(commits) > 0 {
		llmContent += fmt.Sprintf(". Latest commit: %s (%s) by %s on %s",
			commits[0]["short_hash"], commits[0]["message"], commits[0]["author_name"], commits[0]["date"])
	}

	if commitPattern != "" {
		llmContent += fmt.Sprintf(". Repository activity: %s", commitPattern)
	}

	// Add commit message analysis
	var bugfixCount, featureCount, refactorCount int
	for _, commit := range commits {
		message := strings.ToLower(commit["message"])
		if strings.Contains(message, "fix") || strings.Contains(message, "bug") {
			bugfixCount++
		}
		if strings.Contains(message, "feat") || strings.Contains(message, "add") || strings.Contains(message, "new") {
			featureCount++
		}
		if strings.Contains(message, "refactor") || strings.Contains(message, "clean") || strings.Contains(message, "improve") {
			refactorCount++
		}
	}

	if bugfixCount > 0 || featureCount > 0 || refactorCount > 0 {
		llmContent += fmt.Sprintf(". Commit types: %d features, %d bugfixes, %d refactors",
			featureCount, bugfixCount, refactorCount)
	}

	llmContent += "."

	return &interfaces.ToolResult{
		Success:     true,
		UserContent: userContent,
		LLMContent:  llmContent,
		Data: map[string]interface{}{
			"commits":           commits,
			"commit_count":      len(commits),
			"authors":           authors,
			"author_count":      len(authors),
			"commits_today":     totalCommitsToday,
			"commits_this_week": totalCommitsThisWeek,
			"activity_pattern":  commitPattern,
			"feature_commits":   featureCount,
			"bugfix_commits":    bugfixCount,
			"refactor_commits":  refactorCount,
		},
	}, nil
}

// getRemoteURL gets the URL of a remote repository
func (t *GitManagerTool) getRemoteURL(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	name, ok := params["name"].(string)
	if !ok || name == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "Remote name is required for get-url operation",
		}, nil
	}

	result, err := executor.Execute(ctx, "remote", "get-url", name)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to get remote URL: %v", err),
		}, nil
	}

	if result.ExitCode != 0 {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Git remote get-url failed: %s", result.Output),
		}, nil
	}

	url := strings.TrimSpace(result.Output)
	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"name": name,
			"url":  url,
		},
		LLMContent:  fmt.Sprintf("URL for remote '%s' retrieved successfully", name),
		UserContent: fmt.Sprintf("📍 远程仓库 %s 的URL: %s", name, url),
	}, nil
}

// setRemoteURL sets the URL of a remote repository
func (t *GitManagerTool) setRemoteURL(ctx context.Context, params map[string]interface{}, executor GitExecutor) (*interfaces.ToolResult, error) {
	name, ok := params["name"].(string)
	if !ok || name == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "Remote name is required for set-url operation",
		}, nil
	}

	url, ok := params["url"].(string)
	if !ok || url == "" {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "Remote URL is required for set-url operation",
		}, nil
	}

	// Validate the remote URL
	if err := t.validateRemoteURL(url); err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Invalid remote URL: %v", err),
		}, nil
	}

	result, err := executor.Execute(ctx, "remote", "set-url", name, url)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to set remote URL: %v", err),
		}, nil
	}

	if result.ExitCode != 0 {
		return &interfaces.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Git remote set-url failed: %s", result.Output),
		}, nil
	}

	return &interfaces.ToolResult{
		Success: true,
		Data: map[string]interface{}{
			"name": name,
			"url":  url,
		},
		LLMContent:  fmt.Sprintf("URL for remote '%s' updated successfully", name),
		UserContent: fmt.Sprintf("✅ 成功更新远程仓库 %s 的URL为: %s", name, url),
	}, nil
}

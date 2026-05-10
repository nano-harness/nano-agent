package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/memory"
	"github.com/nano-harness/nano-agent/pkg/skill"
)

func newTestSystemPromptBuilder() *SystemPromptBuilder {
	cfg := &config.Config{
		UserInfo: &config.UserInfoConfig{
			AutoDetectUserInfo: false,
		},
	}
	return NewSystemPromptBuilder("/tmp", nil, nil, cfg)
}

func newTestSkillManagerWithSkill(t *testing.T) *skill.Manager {
	t.Helper()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: test-skill\ndescription: A test skill\n---\n# Instructions\nDo something.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := skill.NewManager(dir, dir, "", 0, 0, 0, false)
	if err := mgr.Discover(); err != nil {
		t.Fatalf("skill.Discover() failed: %v", err)
	}
	return mgr
}

func TestBuildBaseSystemPromptContainsToolSelectionPriority(t *testing.T) {
	spb := newTestSystemPromptBuilder()
	prompt := spb.BuildBaseSystemPrompt()
	if !strings.Contains(prompt, "Tool Selection Priority") {
		t.Error("BuildBaseSystemPrompt() should contain 'Tool Selection Priority' section")
	}
	// Verify correct tool names are used
	for _, toolName := range []string{"run_shell_command", "edit_file"} {
		if !strings.Contains(prompt, toolName) {
			t.Errorf("Tool Selection Priority should reference real tool %q", toolName)
		}
	}
	// Verify removed or non-existent tool names are not referenced
	for _, badName := range []string{"search_file_content", "glob", "list_directory", "search_files", "grep_files", "patch_file"} {
		if strings.Contains(prompt, badName) {
			t.Errorf("Tool Selection Priority must not reference non-existent tool %q", badName)
		}
	}
}

func TestBuildBaseSystemPromptContainsGitSafetyProtocol(t *testing.T) {
	spb := newTestSystemPromptBuilder()
	prompt := spb.BuildBaseSystemPrompt()
	if !strings.Contains(prompt, "Git Safety Protocol") {
		t.Error("BuildBaseSystemPrompt() should contain 'Git Safety Protocol' section")
	}
}

func TestBuildBaseSystemPromptContainsProfessionalObjectivity(t *testing.T) {
	spb := newTestSystemPromptBuilder()
	prompt := spb.BuildBaseSystemPrompt()
	if !strings.Contains(prompt, "Professional Objectivity") {
		t.Error("BuildBaseSystemPrompt() should contain 'Professional Objectivity' section")
	}
}

func TestBuildBaseSystemPrompt_IncludesProjectType(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spb := NewSystemPromptBuilder(dir, nil, nil, &config.Config{})

	prompt := spb.BuildBaseSystemPrompt()
	if !strings.Contains(prompt, "Project type: go") {
		t.Fatalf("expected project type context in prompt, got:\n%s", prompt)
	}
}

func TestBuildBaseSystemPrompt_GitContextOptional(t *testing.T) {
	dir := t.TempDir()
	spb := NewSystemPromptBuilder(dir, nil, nil, &config.Config{})

	prompt := spb.BuildBaseSystemPrompt()
	if !strings.Contains(prompt, "**Working Directory**: "+dir) {
		t.Fatalf("expected working directory in prompt, got:\n%s", prompt)
	}
}

func TestBuildEnhancedSystemPromptContainsBlockingRequirement(t *testing.T) {
	spb := newTestSystemPromptBuilder()
	sm := newTestSkillManagerWithSkill(t)
	spb.SetSkillManager(sm)

	prompt := spb.BuildEnhancedSystemPrompt(nil, nil)
	if !strings.Contains(prompt, "BLOCKING REQUIREMENT") {
		t.Error("BuildEnhancedSystemPrompt() should contain 'BLOCKING REQUIREMENT' in skills section")
	}
}

func TestBuildEnhancedSystemPromptContainsMustUseManageSkill(t *testing.T) {
	spb := newTestSystemPromptBuilder()
	prompt := spb.BuildEnhancedSystemPrompt(nil, nil)
	if !strings.Contains(prompt, "MUST use") {
		t.Error("BuildEnhancedSystemPrompt() should contain 'MUST use' in Configuration Management section")
	}
}

// ---- Preload / caching tests ----

// newFastSystemPromptBuilder builds an SPB that skips external tool detection,
// so tests exercise only the caching / async mechanism without shelling out.
func newFastSystemPromptBuilder() *SystemPromptBuilder {
	cfg := &config.Config{
		UserInfo: &config.UserInfoConfig{
			AutoDetectUserInfo: false,
			Timezone:           "UTC",
			OperatingSystem:    "Linux",
			Shell:              "/bin/bash",
			Editor:             "vim",
			Language:           "en",
			ProgrammingTools:   make(map[string]string),
		},
	}
	return NewSystemPromptBuilder("/tmp", nil, nil, cfg)
}

func TestPreloadUserInfo_CompletesWithoutBlocking(t *testing.T) {
	spb := newFastSystemPromptBuilder()
	spb.PreloadUserInfo()

	// userInfoReady should be closed once detection finishes.
	select {
	case <-spb.userInfoReady:
	// expected path
	case <-time.After(5 * time.Second):
		t.Fatal("PreloadUserInfo did not complete within 5s")
	}
}

func TestPreloadUserInfo_ResultCached(t *testing.T) {
	spb := newFastSystemPromptBuilder()
	spb.PreloadUserInfo()

	// Wait for completion.
	<-spb.userInfoReady

	if spb.cachedUserInfo == nil {
		t.Fatal("cachedUserInfo should not be nil after preload")
	}
}

func TestGetUserInfo_ReturnsPreloadedResult(t *testing.T) {
	spb := newFastSystemPromptBuilder()
	spb.PreloadUserInfo()

	info := spb.getUserInfo()
	if info == nil {
		t.Fatal("getUserInfo() returned nil")
	}
}

func TestGetUserInfo_WithoutPreload_FallsBackToSync(t *testing.T) {
	spb := newFastSystemPromptBuilder()
	// Do NOT call PreloadUserInfo – getUserInfo() should still work synchronously.
	info := spb.getUserInfo()
	if info == nil {
		t.Fatal("getUserInfo() returned nil without preload")
	}
}

func TestPreloadUserInfo_OnlyRunsOnce(t *testing.T) {
	spb := newFastSystemPromptBuilder()

	// Call PreloadUserInfo multiple times concurrently.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			spb.PreloadUserInfo()
		}()
	}
	wg.Wait()

	// The channel must be closed exactly once (double-close panics).
	// We just verify the result is still valid.
	select {
	case <-spb.userInfoReady:
	case <-time.After(5 * time.Second):
		t.Fatal("preload did not complete within 5s")
	}
	if spb.cachedUserInfo == nil {
		t.Fatal("cachedUserInfo should not be nil")
	}
}

func TestNewSystemPromptBuilder_HasInitialisedChannel(t *testing.T) {
	spb := newTestSystemPromptBuilder()
	if spb.userInfoReady == nil {
		t.Fatal("userInfoReady channel should be initialised by NewSystemPromptBuilder")
	}
}

// TestGetUserInfo_NilUserInfoConfig verifies that getUserInfo never returns nil
// even when cfg.UserInfo is nil (buildDefaultUserInfo is the fallback).
func TestGetUserInfo_NilUserInfoConfig(t *testing.T) {
	cfg := &config.Config{
		UserInfo: nil, // explicitly nil – doDetectUserInfo must handle this safely
	}
	spb := NewSystemPromptBuilder("/tmp", nil, nil, cfg)
	// Inject a preloaded result to avoid actually shelling out during the test.
	spb.cachedUserInfo = &config.UserInfoConfig{OperatingSystem: "TestOS"}
	spb.preloadStarted.Store(true)
	go spb.loadUserInfo() // fast-path: cachedUserInfo already set, just closes channel
	info := spb.getUserInfo()
	if info == nil {
		t.Fatal("getUserInfo() should never return nil")
	}
	if info.OperatingSystem != "TestOS" {
		t.Fatalf("expected pre-populated OS, got %q", info.OperatingSystem)
	}
}

func TestGetUserInfo_ReturnsCachedValue(t *testing.T) {
	spb := newFastSystemPromptBuilder()
	spb.PreloadUserInfo()
	<-spb.userInfoReady

	// Call multiple times; should return the same cached pointer.
	info1 := spb.getUserInfo()
	info2 := spb.getUserInfo()
	if info1 != info2 {
		t.Fatal("getUserInfo() should return the same cached pointer on repeated calls")
	}
}

func TestBuildEnhancedSystemPromptContainsMemory(t *testing.T) {
	dir := t.TempDir()
	nanoDir := filepath.Join(dir, ".nano")
	if err := os.MkdirAll(nanoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write .nano/NANO.md – ProjectMemory always uses this path for session memory.
	content := "## 2024-01-01 00:00:00\n\nproject memory note\n"
	if err := os.WriteFile(filepath.Join(nanoDir, "NANO.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := memory.NewManager(dir, t.TempDir(), true)
	defer mgr.Close()

	cfg := &config.Config{UserInfo: &config.UserInfoConfig{AutoDetectUserInfo: false}}
	spb := NewSystemPromptBuilder(dir, nil, mgr, cfg)
	spb.cachedUserInfo = &config.UserInfoConfig{}
	spb.preloadStarted.Store(true)
	go spb.loadUserInfo()

	prompt := spb.BuildEnhancedSystemPrompt(context.Background(), nil)
	if !strings.Contains(prompt, "project memory note") {
		t.Errorf("expected project memory note in prompt, got length %d", len(prompt))
	}
}

func TestBuildEnhancedSystemPromptContainsInstructions(t *testing.T) {
	dir := t.TempDir()
	// Write NANO.md at project root – InstructionLoader picks it up automatically.
	if err := os.WriteFile(filepath.Join(dir, "NANO.md"), []byte("custom project instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{UserInfo: &config.UserInfoConfig{AutoDetectUserInfo: false}}
	// NewSystemPromptBuilder auto-initialises InstructionLoader from dir.
	spb := NewSystemPromptBuilder(dir, nil, nil, cfg)
	spb.cachedUserInfo = &config.UserInfoConfig{}
	spb.preloadStarted.Store(true)
	go spb.loadUserInfo()

	prompt := spb.BuildEnhancedSystemPrompt(context.Background(), nil)
	if !strings.Contains(prompt, "custom project instructions") {
		t.Errorf("expected instructions in prompt, got length %d", len(prompt))
	}
}

func TestBuildEnhancedSystemPromptContainsCacheBoundary(t *testing.T) {
	spb := newTestSystemPromptBuilder()
	prompt := spb.BuildEnhancedSystemPrompt(context.Background(), nil)
	if !strings.Contains(prompt, CacheBoundaryMarker) {
		t.Error("BuildEnhancedSystemPrompt() should contain the cache boundary marker")
	}
}

func TestBuildMemorySectionEmpty(t *testing.T) {
	spb := &SystemPromptBuilder{}
	result := spb.buildMemorySection()
	if result != "" {
		t.Errorf("expected empty string for nil memoryManager, got %q", result)
	}
}

func TestBuildInstructionsSectionEmpty(t *testing.T) {
	// A loader pointed at an empty dir should produce no instructions.
	workDir := t.TempDir()
	homeDir := t.TempDir()
	spb := NewSystemPromptBuilder(workDir, nil, nil, &config.Config{
		UserInfo: &config.UserInfoConfig{AutoDetectUserInfo: false},
	})
	spb.SetInstructionLoader(&InstructionLoader{
		workingDir: workDir,
		homeDir:    homeDir,
		cache:      make(map[string]string),
	})
	result := spb.buildInstructionsSection()
	if result != "" {
		t.Errorf("expected empty string when no NANO.md files exist, got %q", result)
	}
}

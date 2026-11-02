package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func newCacheTestSystemPromptBuilder(dir string) *SystemPromptBuilder {
	return NewSystemPromptBuilder(dir, nil, nil, &config.Config{
		UserInfo: &config.UserInfoConfig{
			Timezone:           "UTC",
			OperatingSystem:    "linux",
			Shell:              "/bin/sh",
			Editor:             "nano",
			Language:           "en",
			ProgrammingTools:   map[string]string{},
			AutoDetectUserInfo: false,
		},
	})
}

func TestBuildEnhancedSystemPrompt_CacheHit(t *testing.T) {
	dir := t.TempDir()
	spb := newCacheTestSystemPromptBuilder(dir)

	first := spb.BuildEnhancedSystemPrompt(context.Background(), nil)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := spb.BuildEnhancedSystemPrompt(context.Background(), nil)

	if first != second {
		t.Fatal("expected second prompt build to return cached prompt")
	}
	if strings.Contains(second, "Project type: go") {
		t.Fatal("expected cached prompt to hide later project type change")
	}
}

func TestInvalidatePromptCache_ClearsCache(t *testing.T) {
	dir := t.TempDir()
	spb := newCacheTestSystemPromptBuilder(dir)

	first := spb.BuildEnhancedSystemPrompt(context.Background(), nil)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spb.InvalidatePromptCache()
	second := spb.BuildEnhancedSystemPrompt(context.Background(), nil)

	if first == second {
		t.Fatal("expected invalidation to rebuild prompt")
	}
	if !strings.Contains(second, "Project type: go") {
		t.Fatal("expected rebuilt prompt to include updated project type")
	}
}

func TestPromptCacheKey_GoalsAffectKey(t *testing.T) {
	spb := newCacheTestSystemPromptBuilder(t.TempDir())

	one := spb.promptCacheKey(context.Background(), []string{"one"})
	two := spb.promptCacheKey(context.Background(), []string{"two"})
	if one == two {
		t.Fatal("expected different goals to produce different cache keys")
	}
}

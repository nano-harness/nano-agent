package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestSystemPromptSizeBaseline(t *testing.T) {
	plain := NewSystemPromptBuilder(t.TempDir(), nil, nil, &config.Config{IsSubAgent: true}).BuildEnhancedSystemPrompt(context.Background(), nil)
	one := newActiveSkillPromptBuilder(t, strings.Repeat("body\n", 100)).BuildEnhancedSystemPrompt(context.Background(), nil)
	two := newActiveSkillPromptBuilder(t, strings.Repeat("body\n", 200)).BuildEnhancedSystemPrompt(context.Background(), nil)
	t.Logf("system prompt rune counts: none=%d active=%d double_active=%d", len([]rune(plain)), len([]rune(one)), len([]rune(two)))
}

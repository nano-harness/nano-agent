package preprocessor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteAgentMention(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".nano", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reviewer.yaml"), []byte(`description: Review code
initial_prompt: Review the requested changes.
permission_mode: acceptEdits
color: "#ff00ff"
model: gpt-5-mini
fallbacks: [openai/gpt-4.1, moonshot/kimi-k2]
context_providers: [memory, skills]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rewritten, ok := RewriteAgentMention("@reviewer check pkg/agent", cwd)
	if !ok {
		t.Fatal("expected rewrite")
	}
	for _, want := range []string{`spawn_teammate`, `name="reviewer"`, `permission_mode="acceptEdits"`, `model="gpt-5-mini"`, `fallbacks=["openai/gpt-4.1", "moonshot/kimi-k2"]`, `context_providers="memory,skills"`, `check pkg/agent`} {
		if !strings.Contains(rewritten, want) {
			t.Fatalf("rewrite missing %q: %s", want, rewritten)
		}
	}
}

func TestAgentMentionStepNoProfile(t *testing.T) {
	req := &Request{UserInput: "@missing do work", WorkingDir: t.TempDir()}
	if err := AgentMentionStep().Apply(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if req.UserInput != "@missing do work" {
		t.Fatalf("unexpected rewrite: %q", req.UserInput)
	}
}

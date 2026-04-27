package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/swarm"
)

func TestSystemPromptTeammateAddendumReplacesIdentity(t *testing.T) {
	builder := NewSystemPromptBuilder("", nil, nil, &config.Config{})
	ctx := swarm.WithTeammate(context.Background(), &swarm.TeammateIdentity{
		AgentName: "coder",
		TeamName:  "alpha",
	})

	prompt := builder.buildInstructionsSectionWithContext(ctx)
	for _, want := range []string{"# You are a Teammate Agent", "coder", "alpha"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "{{.") {
		t.Fatalf("prompt contains unreplaced template marker:\n%s", prompt)
	}
}

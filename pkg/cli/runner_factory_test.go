package cli

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/swarm"
)

func TestConfigForTeammateAppliesIndependentModel(t *testing.T) {
	parent := &config.Config{
		Model:        "parent-model",
		EnabledTools: []string{"read_file", "write_file"},
		Memory:       &config.MemoryConfig{UserID: "user"},
		Skills:       &config.SkillsConfig{Enabled: true},
		OpenSpec:     &config.OpenSpecConfig{Enabled: true, InjectContext: true},
	}
	child := configForTeammate(parent, &swarm.TeammateIdentity{
		Model:            "gpt-5-mini",
		AllowedTools:     []string{"read_file"},
		ContextProviders: []string{"memory"},
	})

	if child == parent {
		t.Fatal("configForTeammate returned parent config")
	}
	if child.Model != "gpt-5-mini" {
		t.Fatalf("child Model = %q, want gpt-5-mini", child.Model)
	}
	if parent.Model != "parent-model" {
		t.Fatalf("parent Model mutated to %q", parent.Model)
	}
	if len(child.EnabledTools) != 1 || child.EnabledTools[0] != "read_file" {
		t.Fatalf("child EnabledTools = %#v, want [read_file]", child.EnabledTools)
	}
	if len(parent.EnabledTools) != 2 {
		t.Fatalf("parent EnabledTools mutated: %#v", parent.EnabledTools)
	}
	if child.Memory == nil {
		t.Fatal("child Memory = nil, want preserved")
	}
	if child.Skills == nil || child.Skills.Enabled {
		t.Fatalf("child Skills = %#v, want disabled", child.Skills)
	}
	if child.OpenSpec == nil || child.OpenSpec.InjectContext {
		t.Fatalf("child OpenSpec = %#v, want context injection disabled", child.OpenSpec)
	}
	if parent.Skills == nil || !parent.Skills.Enabled || parent.OpenSpec == nil || !parent.OpenSpec.InjectContext {
		t.Fatalf("parent context providers mutated: skills=%#v openspec=%#v", parent.Skills, parent.OpenSpec)
	}
}

func TestConfigForTeammateAppliesIndependentFallbacks(t *testing.T) {
	parent := &config.Config{
		Model:     "deepseek/deepseek-chat",
		Fallbacks: []string{"openai/gpt-4.1"},
		ModelRouting: &config.ModelRoutingConfig{Fallbacks: []config.ModelRouteConfig{{
			Name: "legacy", Model: "legacy-model",
		}}},
	}
	child := configForTeammate(parent, &swarm.TeammateIdentity{
		Fallbacks: []string{"moonshot/kimi-k2"},
	})
	if child == parent {
		t.Fatal("configForTeammate returned parent config")
	}
	if len(child.Fallbacks) != 1 || child.Fallbacks[0] != "moonshot/kimi-k2" {
		t.Fatalf("child Fallbacks = %#v, want [moonshot/kimi-k2]", child.Fallbacks)
	}
	if child.ModelRouting == nil || len(child.ModelRouting.Fallbacks) != 0 {
		t.Fatalf("child ModelRouting.Fallbacks = %#v, want empty", child.ModelRouting)
	}
	if len(parent.Fallbacks) != 1 || parent.Fallbacks[0] != "openai/gpt-4.1" || len(parent.ModelRouting.Fallbacks) != 1 {
		t.Fatalf("parent mutated: %+v", parent)
	}
}

func TestConfigForTeammateInheritsProvidersBlock(t *testing.T) {
	parent := &config.Config{
		Model: "deepseek/deepseek-chat",
		ProvidersBlock: map[string]config.ProviderBlock{
			"deepseek": {APIKey: "deepseek-key"},
			"openai":   {APIKey: "openai-key"},
		},
	}
	child := configForTeammate(parent, &swarm.TeammateIdentity{Model: "openai/gpt-4o"})
	primary, _, err := llm.ResolveRoutes(child)
	if err != nil {
		t.Fatalf("ResolveRoutes() error = %v", err)
	}
	if primary.ProviderID != "openai" || primary.APIKey != "openai-key" {
		t.Fatalf("primary = %+v, want openai route with inherited provider key", primary)
	}
}

func TestConfigForTeammateNoOverridesPreservesParent(t *testing.T) {
	parent := &config.Config{
		Model:     "deepseek/deepseek-chat",
		Fallbacks: []string{"openai/gpt-4.1"},
		ProvidersBlock: map[string]config.ProviderBlock{
			"deepseek": {APIKey: "deepseek-key"},
			"openai":   {APIKey: "openai-key"},
		},
	}
	child := configForTeammate(parent, &swarm.TeammateIdentity{})
	if child != parent {
		t.Fatal("configForTeammate copied config without overrides")
	}
}

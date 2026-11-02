package agentprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerLoadsProjectAgentProfiles(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".nano", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reviewer.md"), []byte(`---
description: Review code
permission_mode: acceptEdits
allowed_tools: [read_file]
model: gpt-5-mini
fallbacks: [openai/gpt-4.1, moonshot/kimi-k2]
context_providers: [memory, skills]
---
Review $ARGUMENTS carefully.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(cwd)
	profile, ok := mgr.Find("reviewer")
	if !ok {
		t.Fatal("expected reviewer profile")
	}
	if profile.Description != "Review code" || profile.PermissionMode != "acceptEdits" {
		t.Fatalf("unexpected profile metadata: %+v", profile)
	}
	if profile.Model != "gpt-5-mini" {
		t.Fatalf("Model = %q, want gpt-5-mini", profile.Model)
	}
	if len(profile.Fallbacks) != 2 || profile.Fallbacks[0] != "openai/gpt-4.1" || profile.Fallbacks[1] != "moonshot/kimi-k2" {
		t.Fatalf("Fallbacks = %#v, want [openai/gpt-4.1 moonshot/kimi-k2]", profile.Fallbacks)
	}
	if len(profile.ContextProviders) != 2 || profile.ContextProviders[0] != "memory" || profile.ContextProviders[1] != "skills" {
		t.Fatalf("ContextProviders = %#v, want [memory skills]", profile.ContextProviders)
	}
	if profile.InitialPrompt != "Review $ARGUMENTS carefully." {
		t.Fatalf("InitialPrompt = %q", profile.InitialPrompt)
	}
	if len(mgr.List()) != 1 {
		t.Fatalf("List returned %d profiles, want 1", len(mgr.List()))
	}
}

func TestManagerLoadsMarkdownProfileWithEmptyFrontmatter(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".nano", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "writer.md"), []byte("---\n---\nWrite docs."), 0o644); err != nil {
		t.Fatal(err)
	}

	profile, ok := NewManager(cwd).Find("writer")
	if !ok {
		t.Fatal("expected writer profile")
	}
	if profile.InitialPrompt != "Write docs." {
		t.Fatalf("InitialPrompt = %q", profile.InitialPrompt)
	}
}

func TestReadProfileHandlesOnlyClosingDelimiter(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".nano", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "minimal.md"), []byte("---\n---"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile, ok := NewManager(cwd).Find("minimal")
	if !ok {
		t.Fatal("expected minimal profile")
	}
	if profile.InitialPrompt != "" {
		t.Fatalf("InitialPrompt = %q, want empty", profile.InitialPrompt)
	}
}

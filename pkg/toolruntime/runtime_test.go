package toolruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/middleware"
)

type fakeTool struct {
	name     string
	category interfaces.ToolCategory
	result   *interfaces.ToolResult
	err      error
	called   bool
}

func (t *fakeTool) Name() string { return t.name }

func (t *fakeTool) Description() string { return "fake tool" }

func (t *fakeTool) Schema() *interfaces.ToolSchema {
	return interfaces.CreateSchema("fake", nil, nil)
}

func (t *fakeTool) Execute(context.Context, map[string]interface{}) (*interfaces.ToolResult, error) {
	t.called = true
	return t.result, t.err
}

func (t *fakeTool) RequiresConfirmation() bool { return false }

func (t *fakeTool) Category() interfaces.ToolCategory { return t.category }

func (t *fakeTool) ConcurrencySafe() bool { return true }

type recordingMiddleware struct {
	called bool
}

func (m *recordingMiddleware) Name() string { return "recording" }

func (m *recordingMiddleware) Execute(ctx context.Context, tool interfaces.Tool, params map[string]interface{}, next middleware.MiddlewareFunc) (*interfaces.ToolResult, error) {
	m.called = true
	params["middleware"] = true
	return next(ctx, tool, params)
}

type recordingNormalizer struct {
	toolName string
	called   bool
}

func (n *recordingNormalizer) Normalize(toolName string, result *interfaces.ToolResult, err error) (*interfaces.ToolResult, error) {
	n.called = true
	n.toolName = toolName
	return result, err
}

func TestRegistryManagesTools(t *testing.T) {
	registry := NewRegistry()
	tool := &fakeTool{name: "fake", category: interfaces.CategoryDevelopment}

	if err := registry.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.Register(tool); err == nil {
		t.Fatal("expected duplicate registration error")
	}

	got, ok := registry.Get("fake")
	if !ok || got != tool {
		t.Fatalf("expected registered tool, got %#v", got)
	}
	if tools := registry.ListByCategory(interfaces.CategoryDevelopment); len(tools) != 1 || tools[0] != tool {
		t.Fatalf("expected category listing to contain tool, got %#v", tools)
	}
	if schemas := registry.Schemas(); schemas["fake"] == nil {
		t.Fatalf("expected schema for fake tool, got %#v", schemas)
	}
	if _, err := registry.Execute(context.Background(), "fake", nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !tool.called {
		t.Fatal("expected direct registry execution to call tool")
	}

	if err := registry.Unregister("fake"); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if _, ok := registry.Get("fake"); ok {
		t.Fatal("expected tool to be removed")
	}
	if err := registry.Unregister("fake"); err == nil {
		t.Fatal("expected unregister missing tool error")
	}
}

func TestRuntimeExecutesThroughMiddlewareAndNormalizer(t *testing.T) {
	registry := NewRegistry()
	tool := &fakeTool{
		name:   "fake",
		result: &interfaces.ToolResult{Success: true, LLMContent: "ok", UserContent: "ok"},
	}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}

	mw := &recordingMiddleware{}
	normalizer := &recordingNormalizer{}
	runtime := NewRuntime(registry, middleware.NewChain(mw), normalizer)

	params := map[string]interface{}{}
	result, err := runtime.Execute(context.Background(), "fake", params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result != tool.result {
		t.Fatalf("expected original result, got %#v", result)
	}
	if !tool.called || !mw.called || !normalizer.called {
		t.Fatalf("expected tool, middleware, and normalizer to be called")
	}
	if normalizer.toolName != "fake" {
		t.Fatalf("expected normalizer tool name fake, got %q", normalizer.toolName)
	}
	if params["middleware"] != true {
		t.Fatalf("expected middleware mutation, got %#v", params)
	}
}

func TestRuntimePreservesToolErrors(t *testing.T) {
	registry := NewRegistry()
	expectedErr := errors.New("boom")
	tool := &fakeTool{name: "fake", err: expectedErr}
	if err := registry.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := NewRuntime(registry, nil, nil).Execute(context.Background(), "fake", nil)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected tool error, got result=%#v err=%v", result, err)
	}
}

func TestRuntimeReturnsMissingToolError(t *testing.T) {
	_, err := NewRuntime(NewRegistry(), nil, nil).Execute(context.Background(), "missing", nil)
	if err == nil {
		t.Fatal("expected missing tool error")
	}
}

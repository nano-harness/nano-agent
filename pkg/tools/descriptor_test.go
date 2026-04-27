package tools

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

type descriptorTestTool struct {
	name            string
	category        interfaces.ToolCategory
	confirm         bool
	concurrencySafe bool
}

func (t descriptorTestTool) Name() string                   { return t.name }
func (t descriptorTestTool) Description() string            { return "test" }
func (t descriptorTestTool) Schema() *interfaces.ToolSchema { return &interfaces.ToolSchema{} }
func (t descriptorTestTool) RequiresConfirmation() bool     { return t.confirm }
func (t descriptorTestTool) Category() interfaces.ToolCategory {
	return t.category
}
func (t descriptorTestTool) ConcurrencySafe() bool { return t.concurrencySafe }
func (t descriptorTestTool) Execute(context.Context, map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{Success: true}, nil
}

type customDescriptorTool struct {
	descriptorTestTool
}

func (t customDescriptorTool) Descriptor() ToolDescriptor {
	return ToolDescriptor{
		Name:                 t.Name(),
		Category:             interfaces.CategoryWeb,
		MutatesFS:            true,
		RequiresConfirmation: true,
		SupportsSandbox:      false,
		ConcurrencySafe:      false,
	}
}

func TestDescriptorForDerivesCompatibilityMetadata(t *testing.T) {
	descriptor := DescriptorFor(descriptorTestTool{
		name:            "edit",
		category:        interfaces.CategoryFileSystem,
		confirm:         true,
		concurrencySafe: false,
	})

	if descriptor.Name != "edit" {
		t.Fatalf("unexpected name: %q", descriptor.Name)
	}
	if !descriptor.MutatesFS {
		t.Fatal("expected filesystem mutating descriptor")
	}
	if !descriptor.SupportsSandbox {
		t.Fatal("expected filesystem tool to support sandbox")
	}
	if !descriptor.RequiresConfirmation {
		t.Fatal("expected confirmation flag")
	}
}

func TestDescriptorForUsesProviderMetadata(t *testing.T) {
	descriptor := DescriptorFor(customDescriptorTool{
		descriptorTestTool: descriptorTestTool{name: "custom", category: interfaces.CategorySearch, concurrencySafe: true},
	})

	if descriptor.Category != interfaces.CategoryWeb {
		t.Fatalf("expected provider category, got %q", descriptor.Category)
	}
	if !descriptor.MutatesFS {
		t.Fatal("expected provider mutates_fs")
	}
}

func TestToolboxDescriptors(t *testing.T) {
	tb := &Toolbox{
		registry:   NewDefaultToolRegistry(),
		workingDir: t.TempDir(),
	}
	if err := tb.Register(descriptorTestTool{name: "grep", category: interfaces.CategorySearch, concurrencySafe: true}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	descriptors := tb.Descriptors()
	if len(descriptors) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(descriptors))
	}
	if descriptors[0].Name != "grep" || !descriptors[0].SupportsSandbox {
		t.Fatalf("unexpected descriptor: %#v", descriptors[0])
	}
}

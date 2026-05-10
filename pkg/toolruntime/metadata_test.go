package toolruntime

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

type metadataTestTool struct {
	name            string
	category        interfaces.ToolCategory
	confirm         bool
	concurrencySafe bool
}

func (t metadataTestTool) Name() string                   { return t.name }
func (t metadataTestTool) Description() string            { return "test" }
func (t metadataTestTool) Schema() *interfaces.ToolSchema { return &interfaces.ToolSchema{} }
func (t metadataTestTool) RequiresConfirmation() bool     { return t.confirm }
func (t metadataTestTool) Category() interfaces.ToolCategory {
	return t.category
}
func (t metadataTestTool) ConcurrencySafe() bool { return t.concurrencySafe }
func (t metadataTestTool) Execute(context.Context, map[string]interface{}) (*interfaces.ToolResult, error) {
	return &interfaces.ToolResult{Success: true}, nil
}

type customMetadataTool struct {
	metadataTestTool
}

func (t customMetadataTool) Descriptor() ToolDescriptor {
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
	descriptor := DescriptorFor(metadataTestTool{
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
	descriptor := DescriptorFor(customMetadataTool{
		metadataTestTool: metadataTestTool{name: "custom", category: interfaces.CategorySearch, concurrencySafe: true},
	})

	if descriptor.Category != interfaces.CategoryWeb {
		t.Fatalf("expected provider category, got %q", descriptor.Category)
	}
	if !descriptor.MutatesFS {
		t.Fatal("expected provider mutates_fs")
	}
}

func TestCatalogDescriptors(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(metadataTestTool{name: "grep", category: interfaces.CategorySearch, concurrencySafe: true}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	descriptors := NewCatalog(registry).Descriptors()
	if len(descriptors) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(descriptors))
	}
	if descriptors[0].Name != "grep" || !descriptors[0].SupportsSandbox {
		t.Fatalf("unexpected descriptor: %#v", descriptors[0])
	}
}

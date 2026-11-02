package toolruntime

import "github.com/nano-harness/nano-agent/pkg/interfaces"

// Catalog exposes metadata for registered tools without taking ownership of
// registration or execution.
type Catalog struct {
	registry interfaces.ToolRegistry
}

// NewCatalog creates a metadata catalog over an existing registry.
func NewCatalog(registry interfaces.ToolRegistry) Catalog {
	return Catalog{registry: registry}
}

// Descriptors returns typed descriptors for all registered tools.
func (c Catalog) Descriptors() []ToolDescriptor {
	return DescriptorsFor(c.registry.List())
}

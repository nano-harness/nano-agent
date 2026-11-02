package tools

import (
	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/toolruntime"
)

// ToolDescriptor is re-exported for backward compatibility; ToolRuntime owns
// tool metadata going forward.
type ToolDescriptor = toolruntime.ToolDescriptor

// DescriptorProvider is re-exported for backward compatibility.
type DescriptorProvider = toolruntime.DescriptorProvider

// DescriptorFor returns a typed descriptor for a tool.
func DescriptorFor(tool interfaces.Tool) ToolDescriptor {
	return toolruntime.DescriptorFor(tool)
}

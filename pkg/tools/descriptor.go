package tools

import "github.com/nano-harness/nano-agent/pkg/interfaces"

// ToolDescriptor captures stable registration metadata for a tool without
// forcing every existing tool implementation to grow new methods at once.
type ToolDescriptor struct {
	Name                 string                  `json:"name"`
	Category             interfaces.ToolCategory `json:"category"`
	MutatesFS            bool                    `json:"mutates_fs"`
	RequiresConfirmation bool                    `json:"requires_confirmation"`
	SupportsSandbox      bool                    `json:"supports_sandbox"`
	ConcurrencySafe      bool                    `json:"concurrency_safe"`
}

// DescriptorProvider can be implemented by tools that have more precise
// metadata than the compatibility descriptor derived from interfaces.Tool.
type DescriptorProvider interface {
	Descriptor() ToolDescriptor
}

// DescriptorFor returns a typed descriptor for a tool.
func DescriptorFor(tool interfaces.Tool) ToolDescriptor {
	if provider, ok := tool.(DescriptorProvider); ok {
		return provider.Descriptor()
	}

	category := tool.Category()
	concurrencySafe := tool.ConcurrencySafe()
	return ToolDescriptor{
		Name:                 tool.Name(),
		Category:             category,
		MutatesFS:            defaultMutatesFS(category, concurrencySafe),
		RequiresConfirmation: tool.RequiresConfirmation(),
		SupportsSandbox:      defaultSupportsSandbox(category),
		ConcurrencySafe:      concurrencySafe,
	}
}

func defaultMutatesFS(category interfaces.ToolCategory, concurrencySafe bool) bool {
	switch category {
	case interfaces.CategoryFileSystem, interfaces.CategoryShell, interfaces.CategoryGit,
		interfaces.CategoryBuild, interfaces.CategoryTest, interfaces.CategoryLint,
		interfaces.CategoryFormat, interfaces.CategoryDocker, interfaces.CategoryKubernetes:
		return !concurrencySafe
	default:
		return false
	}
}

func defaultSupportsSandbox(category interfaces.ToolCategory) bool {
	switch category {
	case interfaces.CategoryFileSystem, interfaces.CategoryShell, interfaces.CategorySearch:
		return true
	default:
		return false
	}
}

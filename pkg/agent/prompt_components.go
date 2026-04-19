package agent

import (
	"sort"
	"strings"
)

// CacheBoundaryMarker is inserted between cacheable and non-cacheable prompt sections.
// It signals to caching-aware LLM providers where the stable prefix ends.
const CacheBoundaryMarker = "\n\n<!-- __SYSTEM_PROMPT_DYNAMIC_BOUNDARY__ -->\n\n"

// PromptComponent represents a modular section of the system prompt.
type PromptComponent struct {
	// Name identifies this component (for logging/debugging).
	Name string
	// Priority controls ordering: lower values appear first.
	Priority int
	// Condition, if non-nil, is evaluated at Build time; the component is skipped when false.
	Condition func() bool
	// Builder produces the component's string content.
	Builder func() string
	// Cacheable marks sections whose content rarely changes across turns.
	// Cacheable sections are assembled before the CacheBoundaryMarker.
	Cacheable bool
	// MaxTokens is a soft hint for truncation (not enforced by the assembler itself).
	MaxTokens int
}

// PromptAssembler assembles a system prompt from modular components.
type PromptAssembler struct {
	components []*PromptComponent
}

// NewPromptAssembler creates a new, empty PromptAssembler.
func NewPromptAssembler() *PromptAssembler {
	return &PromptAssembler{}
}

// AddComponent registers a component with the assembler.
func (pa *PromptAssembler) AddComponent(c *PromptComponent) {
	pa.components = append(pa.components, c)
}

// Build assembles the prompt from all enabled components sorted by priority.
// Cacheable components are placed before the CacheBoundaryMarker;
// non-cacheable (dynamic) components are placed after it.
func (pa *PromptAssembler) Build() string {
	// Separate into cacheable and non-cacheable, preserving priority order within each group.
	cacheable := make([]*PromptComponent, 0, len(pa.components))
	dynamic := make([]*PromptComponent, 0, len(pa.components))

	for _, c := range pa.components {
		if c.Condition != nil && !c.Condition() {
			continue
		}
		if c.Cacheable {
			cacheable = append(cacheable, c)
		} else {
			dynamic = append(dynamic, c)
		}
	}

	sort.SliceStable(cacheable, func(i, j int) bool {
		return cacheable[i].Priority < cacheable[j].Priority
	})
	sort.SliceStable(dynamic, func(i, j int) bool {
		return dynamic[i].Priority < dynamic[j].Priority
	})

	var cacheableParts []string
	for _, c := range cacheable {
		if content := c.Builder(); content != "" {
			cacheableParts = append(cacheableParts, content)
		}
	}

	var dynamicParts []string
	for _, c := range dynamic {
		if content := c.Builder(); content != "" {
			dynamicParts = append(dynamicParts, content)
		}
	}

	cacheableSection := strings.Join(cacheableParts, "\n\n")
	dynamicSection := strings.Join(dynamicParts, "\n\n")

	switch {
	case cacheableSection == "" && dynamicSection == "":
		return ""
	case cacheableSection == "":
		return dynamicSection
	case dynamicSection == "":
		return cacheableSection
	default:
		return cacheableSection + CacheBoundaryMarker + dynamicSection
	}
}

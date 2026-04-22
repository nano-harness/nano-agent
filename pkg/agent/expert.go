package agent

import (
	"fmt"
	"regexp"
	"sync"
)

// Expert represents a specialized agent definition aligned with Gemini CLI's LocalAgentDefinition.
// Unlike Gemini which supports aliases, nano-agent uses strict kebab-case names without aliases.
type Expert struct {
	// Name is the primary kebab-case identifier (e.g., "investigator", "help", "generalist")
	Name string

	// DisplayName is the human-readable name shown to users
	DisplayName string

	// Description explains what this expert does
	Description string

	// Source indicates where this expert was defined: "builtin", "project", "user", "yaml"
	Source string

	// InputSchema defines the expected input structure
	InputSchema *ExpertInputSchema

	// OutputName is the name of the output field (e.g., "report", "result")
	OutputName string

	// OutputDescription explains what the expert returns
	OutputDescription string

	// OutputSchemaJSON is the JSON schema for output validation (can be empty)
	OutputSchemaJSON string

	// SystemPrompt is the expert's system prompt (empty for generalist = reuse main prompt)
	SystemPrompt string

	// QueryTemplate is the template for rendering the user's input (e.g., "${objective}", "${question}", "${request}")
	QueryTemplate string

	// Model specifies which model to use (empty = inherit from parent)
	Model string

	// Temperature for LLM calls (0.0-1.0, 0 = not set)
	Temperature float64

	// MaxTurns is the maximum number of conversation turns
	MaxTurns int

	// MaxTimeMinutes is the maximum execution time in minutes
	MaxTimeMinutes int

	// AllowedTools lists which tools this expert can use (["*"] = all tools)
	AllowedTools []string
}

// ExpertInputSchema defines the input structure for an expert
type ExpertInputSchema struct {
	// Type is typically "object"
	Type string

	// Properties maps field names to their schemas
	Properties map[string]*ExpertPropertySchema

	// Required lists required field names
	Required []string
}

// ExpertPropertySchema defines a single input property
type ExpertPropertySchema struct {
	Type        string
	Description string
}

// ExpertRegistry manages the collection of available experts
type ExpertRegistry struct {
	mu      sync.RWMutex
	experts map[string]*Expert
}

// NewExpertRegistry creates a new expert registry
func NewExpertRegistry() *ExpertRegistry {
	return &ExpertRegistry{
		experts: make(map[string]*Expert),
	}
}

// Register adds an expert to the registry
// Returns error if an expert with the same name already exists
func (r *ExpertRegistry) Register(expert *Expert) error {
	if expert == nil {
		return fmt.Errorf("cannot register nil expert")
	}

	// Validate expert name (kebab-case: lowercase letters, digits, hyphens)
	if !isValidExpertName(expert.Name) {
		return fmt.Errorf("invalid expert name %q: must match ^[a-z][a-z0-9-]*$", expert.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.experts[expert.Name]; exists {
		return fmt.Errorf("expert %q already registered", expert.Name)
	}

	r.experts[expert.Name] = expert
	return nil
}

// Get retrieves an expert by name
func (r *ExpertRegistry) Get(name string) (*Expert, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	expert, ok := r.experts[name]
	return expert, ok
}

// List returns all registered experts sorted by name
func (r *ExpertRegistry) List() []*Expert {
	r.mu.RLock()
	defer r.mu.RUnlock()

	experts := make([]*Expert, 0, len(r.experts))
	for _, expert := range r.experts {
		experts = append(experts, expert)
	}

	// Sort by name for consistent ordering
	for i := 0; i < len(experts)-1; i++ {
		for j := i + 1; j < len(experts); j++ {
			if experts[i].Name > experts[j].Name {
				experts[i], experts[j] = experts[j], experts[i]
			}
		}
	}

	return experts
}

// Count returns the number of registered experts
func (r *ExpertRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.experts)
}

var expertNameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// isValidExpertName validates expert names: must be kebab-case (lowercase letters, digits, hyphens)
// Must start with a letter, not a digit or hyphen
func isValidExpertName(name string) bool {
	return expertNameRegex.MatchString(name)
}

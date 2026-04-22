//go:build e2e

package e2e

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ExpertTriggerSuite tests the expert trigger parsing system.
// This suite validates:
// - @expert-name pattern recognition and parsing
// - Expert trigger validation (registry membership check)
// - False positive detection (emails, git refs, npm scopes)
// - Input extraction and template rendering
type ExpertTriggerSuite struct {
	suite.Suite
	registry *agent.ExpertRegistry
}

func TestExpertTriggerSuite(t *testing.T) {
	suite.Run(t, new(ExpertTriggerSuite))
}

func (s *ExpertTriggerSuite) SetupTest() {
	// Create registry with test experts
	s.registry = agent.NewExpertRegistry()

	// Register test experts with different input schemas
	err := s.registry.Register(&agent.Expert{
		Name:          "investigator",
		DisplayName:   "Investigator",
		Description:   "Test investigator expert",
		Source:        "test",
		QueryTemplate: "Investigate: ${objective}",
		InputSchema: &agent.ExpertInputSchema{
			Type: "object",
			Properties: map[string]*agent.ExpertPropertySchema{
				"objective": {Type: "string", Description: "Investigation objective"},
			},
			Required: []string{"objective"},
		},
	})
	s.Require().NoError(err)

	err = s.registry.Register(&agent.Expert{
		Name:          "help",
		DisplayName:   "Help",
		Description:   "Test help expert",
		Source:        "test",
		QueryTemplate: "${question}",
		InputSchema: &agent.ExpertInputSchema{
			Type: "object",
			Properties: map[string]*agent.ExpertPropertySchema{
				"question": {Type: "string", Description: "User question"},
			},
			Required: []string{"question"},
		},
	})
	s.Require().NoError(err)

	err = s.registry.Register(&agent.Expert{
		Name:          "multi-word-expert",
		DisplayName:   "Multi Word Expert",
		Description:   "Test kebab-case expert",
		Source:        "test",
		QueryTemplate: "${request}",
		InputSchema: &agent.ExpertInputSchema{
			Type: "object",
			Properties: map[string]*agent.ExpertPropertySchema{
				"request": {Type: "string", Description: "User request"},
			},
			Required: []string{"request"},
		},
	})
	s.Require().NoError(err)
}

// TestParseExpertTrigger_BasicMatch verifies basic @expert-name pattern matching.
func (s *ExpertTriggerSuite) TestParseExpertTrigger_BasicMatch() {
	testCases := []struct {
		name           string
		message        string
		expectedExpert string
		expectedInput  string
		shouldMatch    bool
	}{
		{
			name:           "at start of message",
			message:        "@investigator find the bug",
			expectedExpert: "investigator",
			expectedInput:  "find the bug",
			shouldMatch:    true,
		},
		{
			name:           "after whitespace",
			message:        "Please @help me with this",
			expectedExpert: "help",
			expectedInput:  "me with this",
			shouldMatch:    true,
		},
		{
			name:           "multi-word expert name",
			message:        "@multi-word-expert do something",
			expectedExpert: "multi-word-expert",
			expectedInput:  "do something",
			shouldMatch:    true,
		},
		{
			name:           "no whitespace before @",
			message:        "test@investigator", // Should NOT match (no whitespace before @)
			expectedExpert: "",
			expectedInput:  "",
			shouldMatch:    false,
		},
		{
			name:           "empty input after trigger",
			message:        "@help",
			expectedExpert: "help",
			expectedInput:  "",
			shouldMatch:    true,
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			trigger := agent.ParseExpertTrigger(tc.message, s.registry)

			if !tc.shouldMatch {
				require.Nil(t, trigger, "Should not match trigger")
				return
			}

			require.NotNil(t, trigger, "Should match trigger")
			require.Equal(t, tc.expectedExpert, trigger.ExpertName)
			require.Equal(t, tc.expectedInput, trigger.RawInput)
		})
	}
}

// TestParseExpertTrigger_RegistryValidation verifies that only registered experts trigger.
func (s *ExpertTriggerSuite) TestParseExpertTrigger_RegistryValidation() {
	testCases := []struct {
		name        string
		message     string
		shouldMatch bool
	}{
		{
			name:        "registered expert",
			message:     "@investigator do work",
			shouldMatch: true,
		},
		{
			name:        "unregistered expert",
			message:     "@nonexistent do work",
			shouldMatch: false,
		},
		{
			name:        "valid format but not in registry",
			message:     "@valid-name-format do work",
			shouldMatch: false,
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			trigger := agent.ParseExpertTrigger(tc.message, s.registry)

			if tc.shouldMatch {
				require.NotNil(t, trigger, "Should match registered expert")
			} else {
				require.Nil(t, trigger, "Should not match unregistered expert")
			}
		})
	}
}

// TestParseExpertTrigger_InputExtraction verifies input extraction and mapping.
func (s *ExpertTriggerSuite) TestParseExpertTrigger_InputExtraction() {
	testCases := []struct {
		name               string
		message            string
		expertName         string
		expectedRawInput   string
		expectedInputField string
		expectedInputValue string
	}{
		{
			name:               "investigator with objective",
			message:            "@investigator analyze authentication flow",
			expertName:         "investigator",
			expectedRawInput:   "analyze authentication flow",
			expectedInputField: "objective", // First required field
			expectedInputValue: "analyze authentication flow",
		},
		{
			name:               "help with question",
			message:            "@help how do I run tests?",
			expertName:         "help",
			expectedRawInput:   "how do I run tests?",
			expectedInputField: "question",
			expectedInputValue: "how do I run tests?",
		},
		{
			name:               "multi-word-expert with request",
			message:            "@multi-word-expert generate report",
			expertName:         "multi-word-expert",
			expectedRawInput:   "generate report",
			expectedInputField: "request",
			expectedInputValue: "generate report",
		},
		{
			name:               "empty input",
			message:            "@help",
			expertName:         "help",
			expectedRawInput:   "",
			expectedInputField: "question",
			expectedInputValue: "",
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			trigger := agent.ParseExpertTrigger(tc.message, s.registry)

			require.NotNil(t, trigger)
			require.Equal(t, tc.expertName, trigger.ExpertName)
			require.Equal(t, tc.expectedRawInput, trigger.RawInput)

			// Verify Inputs map
			require.Contains(t, trigger.Inputs, tc.expectedInputField)
			require.Equal(t, tc.expectedInputValue, trigger.Inputs[tc.expectedInputField])
		})
	}
}

// TestParseExpertTrigger_FalsePositives verifies that common patterns don't trigger.
func (s *ExpertTriggerSuite) TestParseExpertTrigger_FalsePositives() {
	testCases := []struct {
		name          string
		message       string
		isFalsePos    bool
		shouldTrigger bool
	}{
		{
			name:          "email address",
			message:       "Contact user@example.com for help",
			isFalsePos:    true,
			shouldTrigger: false,
		},
		{
			name:          "git HEAD reference",
			message:       "Check out @HEAD for the latest",
			isFalsePos:    true,
			shouldTrigger: false,
		},
		{
			name:          "git upstream reference",
			message:       "Merge from @{upstream}",
			isFalsePos:    true,
			shouldTrigger: false,
		},
		{
			name:          "npm scoped package",
			message:       "Install @angular/core package",
			isFalsePos:    true,
			shouldTrigger: false,
		},
		{
			name:          "valid expert trigger",
			message:       "@help with this issue",
			isFalsePos:    false,
			shouldTrigger: true,
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			// Check false positive detection
			isFalsePos := agent.IsExpertTriggerFalsePositive(tc.message)
			require.Equal(t, tc.isFalsePos, isFalsePos,
				"False positive detection mismatch")

			// Check actual trigger parsing
			trigger := agent.ParseExpertTrigger(tc.message, s.registry)
			if tc.shouldTrigger {
				require.NotNil(t, trigger, "Should trigger expert")
			} else {
				require.Nil(t, trigger, "Should not trigger expert")
			}
		})
	}
}

// TestHasExpertTrigger_QuickCheck verifies the quick pattern check.
func (s *ExpertTriggerSuite) TestHasExpertTrigger_QuickCheck() {
	testCases := []struct {
		name        string
		message     string
		shouldMatch bool
	}{
		{
			name:        "valid @pattern",
			message:     "@help me",
			shouldMatch: true,
		},
		{
			name:        "valid @pattern at start",
			message:     "@investigator",
			shouldMatch: true,
		},
		{
			name:        "no @ symbol",
			message:     "help me please",
			shouldMatch: false,
		},
		{
			name:        "@ but uppercase (git ref)",
			message:     "@HEAD",
			shouldMatch: false, // Regex requires lowercase start
		},
		{
			name:        "@ with slash (npm)",
			message:     "@angular/core",
			shouldMatch: true, // Quick check doesn't filter npm scopes
		},
		{
			name:        "email",
			message:     "user@example.com",
			shouldMatch: false, // No whitespace before @
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			hasMatch := agent.HasExpertTrigger(tc.message)
			require.Equal(t, tc.shouldMatch, hasMatch)
		})
	}
}

// TestExpertNameValidation verifies expert name validation rules.
func (s *ExpertTriggerSuite) TestExpertNameValidation() {
	testCases := []struct {
		name      string
		expertDef *agent.Expert
		shouldErr bool
	}{
		{
			name: "valid lowercase",
			expertDef: &agent.Expert{
				Name:          "valid",
				DisplayName:   "Valid",
				Source:        "test",
				QueryTemplate: "${request}",
			},
			shouldErr: false,
		},
		{
			name: "valid kebab-case",
			expertDef: &agent.Expert{
				Name:          "valid-expert-name",
				DisplayName:   "Valid Expert",
				Source:        "test",
				QueryTemplate: "${request}",
			},
			shouldErr: false,
		},
		{
			name: "valid with numbers",
			expertDef: &agent.Expert{
				Name:          "expert123",
				DisplayName:   "Expert 123",
				Source:        "test",
				QueryTemplate: "${request}",
			},
			shouldErr: false,
		},
		{
			name: "invalid uppercase",
			expertDef: &agent.Expert{
				Name:          "Invalid",
				DisplayName:   "Invalid",
				Source:        "test",
				QueryTemplate: "${request}",
			},
			shouldErr: true,
		},
		{
			name: "invalid starts with digit",
			expertDef: &agent.Expert{
				Name:          "1expert",
				DisplayName:   "1 Expert",
				Source:        "test",
				QueryTemplate: "${request}",
			},
			shouldErr: true,
		},
		{
			name: "invalid starts with hyphen",
			expertDef: &agent.Expert{
				Name:          "-expert",
				DisplayName:   "Expert",
				Source:        "test",
				QueryTemplate: "${request}",
			},
			shouldErr: true,
		},
		{
			name: "invalid contains slash",
			expertDef: &agent.Expert{
				Name:          "expert/test",
				DisplayName:   "Expert Test",
				Source:        "test",
				QueryTemplate: "${request}",
			},
			shouldErr: true,
		},
		{
			name: "invalid contains underscore",
			expertDef: &agent.Expert{
				Name:          "expert_test",
				DisplayName:   "Expert Test",
				Source:        "test",
				QueryTemplate: "${request}",
			},
			shouldErr: true,
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			registry := agent.NewExpertRegistry()
			err := registry.Register(tc.expertDef)

			if tc.shouldErr {
				require.Error(t, err, "Should reject invalid expert name")
			} else {
				require.NoError(t, err, "Should accept valid expert name")
			}
		})
	}
}

// TestExpertRegistry_DuplicateRegistration verifies duplicate prevention.
func (s *ExpertTriggerSuite) TestExpertRegistry_DuplicateRegistration() {
	registry := agent.NewExpertRegistry()

	expert1 := &agent.Expert{
		Name:          "test",
		DisplayName:   "Test 1",
		Source:        "test",
		QueryTemplate: "${request}",
	}

	expert2 := &agent.Expert{
		Name:          "test", // Same name
		DisplayName:   "Test 2",
		Source:        "test",
		QueryTemplate: "${request}",
	}

	// First registration should succeed
	err := registry.Register(expert1)
	s.NoError(err)

	// Second registration with same name should fail
	err = registry.Register(expert2)
	s.Error(err, "Should prevent duplicate expert registration")
	s.Contains(err.Error(), "already registered")
}

// TestExpertRegistry_NilExpert verifies nil expert rejection.
func (s *ExpertTriggerSuite) TestExpertRegistry_NilExpert() {
	registry := agent.NewExpertRegistry()

	err := registry.Register(nil)
	s.Error(err, "Should reject nil expert")
	s.Contains(err.Error(), "cannot register nil expert")
}

// TestExpertRegistry_ListOrdering verifies that List() returns sorted results.
func (s *ExpertTriggerSuite) TestExpertRegistry_ListOrdering() {
	registry := agent.NewExpertRegistry()

	// Register in non-alphabetical order
	_ = registry.Register(&agent.Expert{Name: "zebra", DisplayName: "Zebra", Source: "test", QueryTemplate: "${request}"})
	_ = registry.Register(&agent.Expert{Name: "apple", DisplayName: "Apple", Source: "test", QueryTemplate: "${request}"})
	_ = registry.Register(&agent.Expert{Name: "banana", DisplayName: "Banana", Source: "test", QueryTemplate: "${request}"})

	experts := registry.List()
	s.Len(experts, 3)

	// Should be sorted alphabetically by name
	s.Equal("apple", experts[0].Name)
	s.Equal("banana", experts[1].Name)
	s.Equal("zebra", experts[2].Name)
}

// TestExpertRegistry_Count verifies count tracking.
func (s *ExpertTriggerSuite) TestExpertRegistry_Count() {
	registry := agent.NewExpertRegistry()
	s.Equal(0, registry.Count())

	_ = registry.Register(&agent.Expert{Name: "expert1", DisplayName: "Expert 1", Source: "test", QueryTemplate: "${request}"})
	s.Equal(1, registry.Count())

	_ = registry.Register(&agent.Expert{Name: "expert2", DisplayName: "Expert 2", Source: "test", QueryTemplate: "${request}"})
	s.Equal(2, registry.Count())

	// Duplicate registration doesn't increase count
	err := registry.Register(&agent.Expert{Name: "expert1", DisplayName: "Duplicate", Source: "test", QueryTemplate: "${request}"})
	s.Error(err)
	s.Equal(2, registry.Count())
}

package agent

import (
	"testing"
)

func TestExpertRegistry_Register(t *testing.T) {
	registry := NewExpertRegistry()

	expert := &Expert{
		Name:        "test-expert",
		DisplayName: "Test Expert",
		Description: "A test expert",
		Source:      "test",
	}

	err := registry.Register(expert)
	if err != nil {
		t.Fatalf("Failed to register expert: %v", err)
	}

	// Try to retrieve it
	retrieved, ok := registry.Get("test-expert")
	if !ok {
		t.Fatal("Expert not found after registration")
	}
	if retrieved.Name != "test-expert" {
		t.Errorf("Expected expert name 'test-expert', got %q", retrieved.Name)
	}
}

func TestExpertRegistry_DuplicateRegistration(t *testing.T) {
	registry := NewExpertRegistry()

	expert := &Expert{
		Name:        "duplicate",
		DisplayName: "Duplicate Expert",
		Description: "Test",
		Source:      "test",
	}

	err := registry.Register(expert)
	if err != nil {
		t.Fatalf("First registration failed: %v", err)
	}

	// Try to register again with same name
	err = registry.Register(expert)
	if err == nil {
		t.Fatal("Expected error when registering duplicate expert name")
	}
}

func TestExpertRegistry_InvalidName(t *testing.T) {
	registry := NewExpertRegistry()

	testCases := []struct {
		name string
		want bool // true if should fail
	}{
		{"valid-name", false},
		{"validname123", false},
		{"test-expert-123", false},
		{"InvalidCamelCase", true},   // uppercase not allowed
		{"invalid_snake", true},      // underscore not allowed
		{"123invalid", true},         // must start with letter
		{"-invalid", true},           // must start with letter
		{"invalid.name", true},       // dot not allowed
		{"invalid name", true},       // space not allowed
		{"", true},                   // empty not allowed
	}

	for _, tc := range testCases {
		expert := &Expert{
			Name:        tc.name,
			DisplayName: "Test",
			Description: "Test",
			Source:      "test",
		}

		err := registry.Register(expert)
		if tc.want && err == nil {
			t.Errorf("Expected error for invalid name %q, got nil", tc.name)
		}
		if !tc.want && err != nil {
			t.Errorf("Expected success for valid name %q, got error: %v", tc.name, err)
		}
	}
}

func TestExpertRegistry_List(t *testing.T) {
	registry := NewExpertRegistry()

	experts := []*Expert{
		{Name: "zebra", DisplayName: "Zebra", Description: "Z", Source: "test"},
		{Name: "alpha", DisplayName: "Alpha", Description: "A", Source: "test"},
		{Name: "beta", DisplayName: "Beta", Description: "B", Source: "test"},
	}

	for _, expert := range experts {
		if err := registry.Register(expert); err != nil {
			t.Fatalf("Failed to register %q: %v", expert.Name, err)
		}
	}

	list := registry.List()
	if len(list) != 3 {
		t.Fatalf("Expected 3 experts, got %d", len(list))
	}

	// Verify sorted order
	expected := []string{"alpha", "beta", "zebra"}
	for i, expert := range list {
		if expert.Name != expected[i] {
			t.Errorf("Expected expert %d to be %q, got %q", i, expected[i], expert.Name)
		}
	}
}

func TestExpertRegistry_Count(t *testing.T) {
	registry := NewExpertRegistry()

	if count := registry.Count(); count != 0 {
		t.Errorf("Expected count 0 for empty registry, got %d", count)
	}

	expert := &Expert{
		Name:        "test",
		DisplayName: "Test",
		Description: "Test",
		Source:      "test",
	}

	if err := registry.Register(expert); err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	if count := registry.Count(); count != 1 {
		t.Errorf("Expected count 1 after registration, got %d", count)
	}
}

func TestBuiltinExperts_Registration(t *testing.T) {
	registry := NewExpertRegistry()

	err := RegisterBuiltinExperts(registry)
	if err != nil {
		t.Fatalf("Failed to register builtin experts: %v", err)
	}

	// Verify all three builtin experts are registered
	expectedExperts := []string{"investigator", "help", "generalist"}
	for _, name := range expectedExperts {
		expert, ok := registry.Get(name)
		if !ok {
			t.Errorf("Expected builtin expert %q not found", name)
			continue
		}
		if expert.Source != "builtin" {
			t.Errorf("Expert %q should have source 'builtin', got %q", name, expert.Source)
		}
	}

	// Verify count
	if count := registry.Count(); count != 3 {
		t.Errorf("Expected 3 builtin experts, got %d", count)
	}
}

func TestParseExpertTrigger_Valid(t *testing.T) {
	registry := NewExpertRegistry()
	expert := &Expert{
		Name:        "test-expert",
		DisplayName: "Test Expert",
		Description: "Test",
		Source:      "test",
		InputSchema: &ExpertInputSchema{
			Type:     "object",
			Required: []string{"objective"},
		},
	}
	if err := registry.Register(expert); err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	testCases := []struct {
		input       string
		shouldMatch bool
		expertName  string
		rawInput    string
	}{
		{"@test-expert analyze this code", true, "test-expert", "analyze this code"},
		{"  @test-expert with leading spaces", true, "test-expert", "with leading spaces"},
		{"@test-expert", true, "test-expert", ""},
		{"user@example.com is my email", false, "", ""},
		{"@HEAD is a git ref", false, "", ""},
		{"@unknown-expert is not registered", false, "", ""},
		{"no trigger here", false, "", ""},
	}

	for _, tc := range testCases {
		trigger := ParseExpertTrigger(tc.input, registry)

		if tc.shouldMatch {
			if trigger == nil {
				t.Errorf("Expected match for %q, got nil", tc.input)
				continue
			}
			if trigger.ExpertName != tc.expertName {
				t.Errorf("Expected expert name %q, got %q", tc.expertName, trigger.ExpertName)
			}
			if trigger.RawInput != tc.rawInput {
				t.Errorf("Expected raw input %q, got %q", tc.rawInput, trigger.RawInput)
			}
		} else {
			if trigger != nil {
				t.Errorf("Expected no match for %q, got trigger for %q", tc.input, trigger.ExpertName)
			}
		}
	}
}

func TestToKebabCase(t *testing.T) {
	testCases := []struct {
		input string
		want  string
	}{
		{"coder", "coder"},
		{"myAgent", "my-agent"},
		{"MyAgent", "my-agent"},
		{"my_agent", "my-agent"},
		{"MY_AGENT", "my-agent"},
		{"alreadyKebab-case", "already-kebab-case"}, // hyphens preserved
		{"multipleWords", "multiple-words"},
		{"HTTPSConnection", "httpsconnection"},       // consecutive uppercase
		{"穿越小说家", ""},                            // non-ASCII
		{"", ""},
		{"a", "a"},
		{"_underscore", "underscore"},
		{"trailing_", "trailing"},
	}

	for _, tc := range testCases {
		got := toKebabCase(tc.input)
		if got != tc.want {
			t.Errorf("toKebabCase(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

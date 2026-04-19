package llm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

func TestReasoningIntegration(t *testing.T) {
	// Create test configuration with reasoning enabled
	cfg := &config.Config{
		Reasoning: &config.ReasoningConfig{
			Enabled:   true,
			Effort:    "medium",
			MaxTokens: 0,
			Exclude:   false,
		},
		Model:   "gpt-4",
		BaseURL: "https://api.openai.com/v1",
	}

	// Create client with test configuration
	client := &Client{
		model:  cfg.Model,
		config: cfg,
	}

	// Test messages (for future use)
	_ = []Message{
		{
			Role:    "system",
			Content: "You are a helpful assistant.",
		},
		{
			Role:    "user",
			Content: "What is 2+2?",
		},
	}

	// Event handler to capture reasoning
	var capturedEvents []event.StreamEvent
	onEvent := func(ev event.StreamEvent) {
		capturedEvents = append(capturedEvents, ev)
	}

	// Test that reasoning parameters are properly configured
	t.Run("ReasoningConfigurationTest", func(t *testing.T) {
		if client.config == nil {
			t.Fatal("Client config is nil")
		}

		if client.config.Reasoning == nil {
			t.Fatal("Reasoning config is nil")
		}

		if !client.config.Reasoning.Enabled {
			t.Error("Reasoning should be enabled")
		}

		if client.config.Reasoning.Effort != "medium" {
			t.Errorf("Expected effort 'medium', got '%s'", client.config.Reasoning.Effort)
		}
	})

	// Test event structure includes reasoning field
	t.Run("EventStructureTest", func(t *testing.T) {
		testEvent := event.StreamEvent{
			Type:      event.EventTypeContent,
			Content:   "Test content",
			Reasoning: "Test reasoning",
		}

		if testEvent.Reasoning != "Test reasoning" {
			t.Errorf("Expected reasoning 'Test reasoning', got '%s'", testEvent.Reasoning)
		}
	})

	// Test finalizeResponse with reasoning
	t.Run("FinalizeResponseTest", func(t *testing.T) {
		// Mock token stats
		tokenStats := &TokenStats{
			InputTokens:  10,
			OutputTokens: 20,
		}

		// Test finalizeResponse method signature
		err := client.finalizeResponse("test content", "test reasoning", []tools.ToolCall{}, onEvent, tokenStats)
		if err != nil {
			t.Errorf("finalizeResponse failed: %v", err)
		}

		// Check that content event was generated with reasoning
		var contentEvent *event.StreamEvent
		for _, ev := range capturedEvents {
			if ev.Type == event.EventTypeContent {
				contentEvent = &ev
				break
			}
		}

		if contentEvent == nil {
			t.Fatal("No content event was generated")
		}

		if contentEvent.Content != "test content" {
			t.Errorf("Expected content 'test content', got '%s'", contentEvent.Content)
		}

		if contentEvent.Reasoning != "test reasoning" {
			t.Errorf("Expected reasoning 'test reasoning', got '%s'", contentEvent.Reasoning)
		}
	})
}

func TestReasoningConfigDefaults(t *testing.T) {
	// Test default configuration
	defaultCfg := config.DefaultConfig()

	if defaultCfg.Reasoning == nil {
		t.Log("Default config has no reasoning configuration - this is expected")
		return
	}

	// If reasoning config exists, test its defaults
	if defaultCfg.Reasoning.Enabled {
		t.Log("Reasoning is enabled by default")
	} else {
		t.Log("Reasoning is disabled by default")
	}
}

func TestReasoningConfigLoading(t *testing.T) {
	// Create a temporary test config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test-config.yaml")

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	_ = os.Chdir(tempDir)

	originalReasoningEnabled := os.Getenv("NANO_REASONING_ENABLED")
	originalReasoningEffort := os.Getenv("NANO_REASONING_EFFORT")
	originalReasoningMaxTokens := os.Getenv("NANO_REASONING_MAX_TOKENS")
	originalReasoningExclude := os.Getenv("NANO_REASONING_EXCLUDE")
	defer func() {
		if originalReasoningEnabled != "" {
			_ = os.Setenv("NANO_REASONING_ENABLED", originalReasoningEnabled)
		} else {
			_ = os.Unsetenv("NANO_REASONING_ENABLED")
		}
		if originalReasoningEffort != "" {
			_ = os.Setenv("NANO_REASONING_EFFORT", originalReasoningEffort)
		} else {
			_ = os.Unsetenv("NANO_REASONING_EFFORT")
		}
		if originalReasoningMaxTokens != "" {
			_ = os.Setenv("NANO_REASONING_MAX_TOKENS", originalReasoningMaxTokens)
		} else {
			_ = os.Unsetenv("NANO_REASONING_MAX_TOKENS")
		}
		if originalReasoningExclude != "" {
			_ = os.Setenv("NANO_REASONING_EXCLUDE", originalReasoningExclude)
		} else {
			_ = os.Unsetenv("NANO_REASONING_EXCLUDE")
		}
	}()
	_ = os.Unsetenv("NANO_REASONING_ENABLED")
	_ = os.Unsetenv("NANO_REASONING_EFFORT")
	_ = os.Unsetenv("NANO_REASONING_MAX_TOKENS")
	_ = os.Unsetenv("NANO_REASONING_EXCLUDE")

	// Test config content with specific reasoning settings
	configContent := `
api_key: "test-key"
model: "test-model"
reasoning:
  enabled: true
  effort: "low"
  max_tokens: 0
  exclude: false
`

	// Write the test config to temporary file
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Test loading configuration from test config file
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Reasoning == nil {
		t.Fatal("Reasoning config should be loaded from file")
	}

	// Verify the values from the config file
	if !cfg.Reasoning.Enabled {
		t.Error("Reasoning should be enabled in the test config")
	}

	if cfg.Reasoning.Effort != "low" {
		t.Errorf("Expected effort 'low', got '%s'", cfg.Reasoning.Effort)
	}

	if cfg.Reasoning.MaxTokens != 0 {
		t.Errorf("Expected max_tokens 0, got %d", cfg.Reasoning.MaxTokens)
	}

	if cfg.Reasoning.Exclude {
		t.Error("Exclude should be false in the test config")
	}

	t.Logf("Successfully loaded reasoning config: enabled=%t, effort=%s, max_tokens=%d, exclude=%t",
		cfg.Reasoning.Enabled, cfg.Reasoning.Effort, cfg.Reasoning.MaxTokens, cfg.Reasoning.Exclude)
}

func TestNeedsReasoningContentInMessages(t *testing.T) {
	tests := []struct {
		name           string
		baseURL        string
		reasoningCfg   *config.ReasoningConfig
		expectedResult bool
	}{
		{
			name:    "Moonshot URL with reasoning enabled",
			baseURL: "https://api.moonshot.cn/v1",
			reasoningCfg: &config.ReasoningConfig{
				Enabled: true,
				Effort:  "medium",
			},
			expectedResult: true,
		},
		{
			name:    "DeepSeek URL with reasoning enabled",
			baseURL: "https://api.deepseek.com/v1",
			reasoningCfg: &config.ReasoningConfig{
				Enabled: true,
				Effort:  "high",
			},
			expectedResult: true,
		},
		{
			name:    "OpenAI URL with reasoning enabled",
			baseURL: "https://api.openai.com/v1",
			reasoningCfg: &config.ReasoningConfig{
				Enabled: true,
				Effort:  "medium",
			},
			expectedResult: false,
		},
		{
			name:    "Moonshot URL with reasoning disabled",
			baseURL: "https://api.moonshot.cn/v1",
			reasoningCfg: &config.ReasoningConfig{
				Enabled: false,
			},
			expectedResult: false,
		},
		{
			name:           "Unknown provider URL with reasoning enabled",
			baseURL:        "https://api.custom-provider.com/v1",
			reasoningCfg:   &config.ReasoningConfig{Enabled: true, Effort: "low"},
			expectedResult: false,
		},
		{
			name:    "Empty baseURL defaults to OpenAI with reasoning enabled",
			baseURL: "",
			reasoningCfg: &config.ReasoningConfig{
				Enabled: true,
				Effort:  "medium",
			},
			expectedResult: false,
		},
		{
			name:           "Reasoning not configured",
			baseURL:        "https://api.moonshot.cn/v1",
			reasoningCfg:   nil,
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				baseURL: tt.baseURL,
				config:  &config.Config{Reasoning: tt.reasoningCfg},
			}
			if tt.reasoningCfg == nil {
				client.config.Reasoning = nil
			}

			result := client.needsReasoningContentInMessages()
			if result != tt.expectedResult {
				t.Errorf("Expected %v, got %v for %s", tt.expectedResult, result, tt.name)
			}
		})
	}
}

func TestMaybeSetReasoningMessagesOverride_Moonshot(t *testing.T) {
	// Test with Moonshot URL and reasoning enabled
	client := &Client{
		baseURL: "https://api.moonshot.cn/v1",
		config: &config.Config{
			Reasoning: &config.ReasoningConfig{
				Enabled: true,
				Effort:  "medium",
			},
		},
	}

	messages := []Message{
		{
			Role:    "user",
			Content: "Hello",
		},
		{
			Role:      "assistant",
			Content:   "Hi, how can I help?",
			Reasoning: "User greeted me",
		},
		{
			Role:      "assistant",
			Content:   "Let me check that for you",
			ToolCalls: []tools.ToolCall{{ID: "call_123", Name: "search", Arguments: map[string]interface{}{"query": "test"}}},
			Reasoning: "Need to search for information",
		},
	}

	extraFields := make(map[string]interface{})
	client.maybeSetReasoningMessagesOverride(extraFields, messages)

	// Check that messages were overridden
	if _, ok := extraFields["messages"]; !ok {
		t.Error("Expected messages to be overridden in extraFields")
	}

	// Verify the structure
	rawMessages, ok := extraFields["messages"].([]map[string]interface{})
	if !ok {
		t.Fatal("messages should be a slice of maps")
	}

	if len(rawMessages) != len(messages) {
		t.Errorf("Expected %d messages, got %d", len(messages), len(rawMessages))
	}

	// Check that assistant messages have reasoning_content
	for i, msg := range rawMessages {
		if messages[i].Role == "assistant" {
			if _, hasReasoning := msg["reasoning_content"]; !hasReasoning {
				t.Errorf("Assistant message at index %d should have reasoning_content field", i)
			}
			// Verify reasoning content matches
			if reasoningContent, ok := msg["reasoning_content"].(string); ok {
				if reasoningContent != messages[i].Reasoning {
					t.Errorf("Expected reasoning_content '%s', got '%s'", messages[i].Reasoning, reasoningContent)
				}
			}
		} else {
			// Non-assistant messages should not have reasoning_content
			if _, hasReasoning := msg["reasoning_content"]; hasReasoning {
				t.Errorf("Non-assistant message at index %d should not have reasoning_content field", i)
			}
		}
	}

	// Test with empty reasoning (should still have the field with empty string)
	t.Run("EmptyReasoningContent", func(t *testing.T) {
		messagesWithEmptyReasoning := []Message{
			{
				Role:      "assistant",
				Content:   "Response",
				ToolCalls: []tools.ToolCall{{ID: "call_456", Name: "tool", Arguments: map[string]interface{}{}}},
				Reasoning: "", // Empty reasoning
			},
		}

		extraFields := make(map[string]interface{})
		client.maybeSetReasoningMessagesOverride(extraFields, messagesWithEmptyReasoning)

		rawMessages := extraFields["messages"].([]map[string]interface{})
		if len(rawMessages) > 0 {
			msg := rawMessages[0]
			if reasoningContent, ok := msg["reasoning_content"]; !ok {
				t.Error("Assistant message should have reasoning_content field even when empty")
			} else if reasoningContent != "" {
				t.Errorf("Expected empty reasoning_content, got '%v'", reasoningContent)
			}
		}
	})

	// Test that it doesn't override for OpenAI
	t.Run("NoOverrideForOpenAI", func(t *testing.T) {
		openaiClient := &Client{
			baseURL: "https://api.openai.com/v1",
			config: &config.Config{
				Reasoning: &config.ReasoningConfig{
					Enabled: true,
					Effort:  "medium",
				},
			},
		}

		extraFields := make(map[string]interface{})
		openaiClient.maybeSetReasoningMessagesOverride(extraFields, messages)

		if _, ok := extraFields["messages"]; ok {
			t.Error("Should not override messages for OpenAI URL")
		}
	})
}

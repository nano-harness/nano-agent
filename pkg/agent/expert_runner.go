package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// ExpertRunner executes expert tasks using the fork system
type ExpertRunner struct {
	registry    *ExpertRegistry
	forkManager *ForkManager
	parentSPB   *SystemPromptBuilder
}

// NewExpertRunner creates a new expert runner
func NewExpertRunner(registry *ExpertRegistry, forkManager *ForkManager, parentSPB *SystemPromptBuilder) *ExpertRunner {
	return &ExpertRunner{
		registry:    registry,
		forkManager: forkManager,
		parentSPB:   parentSPB,
	}
}

// ExpertResult contains the result of an expert execution
type ExpertResult struct {
	ExpertName  string
	DisplayName string
	Task        string
	Output      string
	OutputJSON  map[string]interface{} // Parsed output if JSON schema validation succeeds
	Error       error
	Duration    time.Duration
	TokensUsed  TokenUsageInfo
}

// Run executes an expert with the given inputs
func (er *ExpertRunner) Run(ctx context.Context, expertName string, inputs map[string]interface{}, eventCallback func(event.StreamEvent)) (*ExpertResult, error) {
	expert, ok := er.registry.Get(expertName)
	if !ok {
		return nil, fmt.Errorf("expert %q not found", expertName)
	}

	startTime := time.Now()

	// Emit expert started event
	if eventCallback != nil {
		eventCallback(event.StreamEvent{
			Type:      event.EventTypeExpertStarted,
			Timestamp: time.Now().Unix(),
			Metadata: map[string]interface{}{
				"expert_name":  expert.Name,
				"display_name": expert.DisplayName,
				"source":       expert.Source,
				"task":         inputs,
			},
		})
	}

	// Render query template with inputs
	query, err := er.renderTemplate(expert.QueryTemplate, inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to render query template: %w", err)
	}

	// Build system prompt
	var systemPrompt string
	if expert.SystemPrompt == "" && expert.Name == "generalist" {
		// Special case: generalist reuses parent's system prompt
		if er.parentSPB != nil {
			systemPrompt = er.parentSPB.BuildBaseSystemPrompt()
		}
	} else {
		systemPrompt = expert.SystemPrompt
	}

	// Build fork config
	forkConfig := ForkConfig{
		AgentType:    AgentTypeExecute, // Experts always use execute type
		Task:         query,
		SystemPrompt: systemPrompt,
		Timeout:      time.Duration(expert.MaxTimeMinutes) * time.Minute,
	}

	// Execute via fork manager
	result, err := er.forkManager.Fork(ctx, forkConfig)

	duration := time.Since(startTime)

	expertResult := &ExpertResult{
		ExpertName:  expert.Name,
		DisplayName: expert.DisplayName,
		Task:        query,
		Error:       err,
		Duration:    duration,
	}

	if result != nil {
		expertResult.Output = result.Output
		expertResult.TokensUsed = result.TokensUsed

		// Try to parse output as JSON if OutputSchemaJSON is provided
		if expert.OutputSchemaJSON != "" && result.Output != "" {
			var outputData map[string]interface{}
			decoder := json.NewDecoder(strings.NewReader(result.Output))
			decoder.UseNumber() // Preserve numbers as json.Number instead of float64
			if jsonErr := decoder.Decode(&outputData); jsonErr == nil {
				expertResult.OutputJSON = outputData
			} else {
				logger.Warnf("Failed to parse expert output as JSON: %v", jsonErr)
			}
		}
	}

	// Emit expert finished event
	if eventCallback != nil {
		finishedEvent := event.StreamEvent{
			Type:      event.EventTypeExpertFinished,
			Timestamp: time.Now().Unix(),
			Metadata: map[string]interface{}{
				"expert_name":  expert.Name,
				"display_name": expert.DisplayName,
				"source":       expert.Source,
				"duration_ms":  duration.Milliseconds(),
				"success":      err == nil,
			},
		}

		if result != nil {
			finishedEvent.Metadata["tokens_used"] = result.TokensUsed.TotalTokens
			finishedEvent.Metadata["input_tokens"] = result.TokensUsed.InputTokens
			finishedEvent.Metadata["output_tokens"] = result.TokensUsed.OutputTokens
		}

		if err != nil {
			finishedEvent.Error = err.Error()
		}

		eventCallback(finishedEvent)
	}

	return expertResult, err
}

// renderTemplate replaces ${var} placeholders with values from inputs
func (er *ExpertRunner) renderTemplate(template string, inputs map[string]interface{}) (string, error) {
	result := template

	// Simple template variable replacement: ${var}
	for key, value := range inputs {
		placeholder := fmt.Sprintf("${%s}", key)
		valueStr := fmt.Sprintf("%v", value)
		result = strings.ReplaceAll(result, placeholder, valueStr)
	}

	return result, nil
}

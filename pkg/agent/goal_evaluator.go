package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

type GoalEvalResult struct {
	Met                   bool
	Reason                string
	TokensUsed            int
	EvaluatorParseFailed  bool // True when JSON parsing failed
}

type GoalEvaluator struct {
	client llm.StreamClient
}

func NewGoalEvaluator(client llm.StreamClient) *GoalEvaluator {
	return &GoalEvaluator{client: client}
}

func (e *GoalEvaluator) Evaluate(ctx context.Context, condition string, messages []llm.Message) (*GoalEvalResult, error) {
	if e == nil || e.client == nil {
		return &GoalEvalResult{Met: false, Reason: "goal evaluator is not configured"}, nil
	}
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return &GoalEvalResult{Met: false, Reason: "goal condition is empty"}, nil
	}

	evalMessages := []llm.Message{
		{
			Role: "system",
			Content: "You are a goal completion evaluator. Given the conversation transcript and the goal condition below, determine if the condition has been met.\n\n" +
				"Goal condition: " + condition + "\n\n" +
				"Respond in JSON: {\"met\": true/false, \"reason\": \"brief explanation\"}\n" +
				"Only judge based on what is visible in the conversation. Do not assume anything not stated.",
		},
		{
			Role:    "user",
			Content: "Conversation transcript:\n" + formatGoalTranscript(messages),
		},
	}

	var content strings.Builder
	var streamContent string
	tokensUsed := 0
	err := e.client.StreamCompletionWithoutReasoning(ctx, evalMessages, func(ev event.StreamEvent) {
		switch ev.Type {
		case event.EventTypeContent:
			content.WriteString(ev.Content)
		case event.EventTypeStreamContent:
			streamContent = ev.Content
		case event.EventTypeTokenStats:
			if ev.TokenStats != nil {
				tokensUsed = ev.TokenStats.TotalTokens
				if tokensUsed == 0 {
					tokensUsed = ev.TokenStats.InputTokens + ev.TokenStats.OutputTokens
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(content.String())
	if raw == "" {
		raw = strings.TrimSpace(streamContent)
	}
	var parsed struct {
		Met    bool   `json:"met"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(extractGoalEvalJSON(raw)), &parsed); err != nil {
		// Include raw response snippet in reason for diagnosis
		snippet := raw
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return &GoalEvalResult{
			Met:                  false,
			Reason:               fmt.Sprintf("Evaluator returned invalid JSON (raw: %s)", snippet),
			TokensUsed:           tokensUsed,
			EvaluatorParseFailed: true,
		}, nil
	}
	if strings.TrimSpace(parsed.Reason) == "" {
		parsed.Reason = "No reason provided"
	}
	return &GoalEvalResult{Met: parsed.Met, Reason: parsed.Reason, TokensUsed: tokensUsed}, nil
}

func formatGoalTranscript(messages []llm.Message) string {
	var b strings.Builder
	for _, msg := range messages {
		if msg.Role == "system" {
			continue
		}
		role := msg.Role
		if role == "" {
			role = "unknown"
		}
		fmt.Fprintf(&b, "[%s] %s\n", role, msg.Content)
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				fmt.Fprintf(&b, "[assistant tool_call] %s %v\n", tc.Name, tc.Arguments)
			}
		}
		if msg.ToolCallID != "" {
			fmt.Fprintf(&b, "[tool_result %s] %s\n", msg.ToolCallID, msg.Content)
		}
	}
	return strings.TrimSpace(b.String())
}

func extractGoalEvalJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return raw
}

// SatisfactionEvalResult contains user satisfaction evaluation result
type SatisfactionEvalResult struct {
	Satisfied             bool
	Score                 float64 // 0.0-1.0 satisfaction score
	Reason                string
	TokensUsed            int
	EvaluatorParseFailed  bool // True when JSON parsing failed
}

// EvaluateSatisfaction evaluates whether the user's request has been satisfied
// based on the conversation history. Returns a satisfaction score between 0.0 and 1.0.
func (e *GoalEvaluator) EvaluateSatisfaction(ctx context.Context, userInput string, messages []llm.Message) (*SatisfactionEvalResult, error) {
	if e == nil || e.client == nil {
		return &SatisfactionEvalResult{Satisfied: false, Score: 0.0, Reason: "satisfaction evaluator is not configured"}, nil
	}
	if strings.TrimSpace(userInput) == "" {
		return &SatisfactionEvalResult{Satisfied: false, Score: 0.0, Reason: "user input is empty"}, nil
	}

	evalMessages := []llm.Message{
		{
			Role: "system",
			Content: "You are a user satisfaction evaluator. Given the user's original request and the conversation transcript, determine if the user's needs have been satisfied.\n\n" +
				"User's original request: " + userInput + "\n\n" +
				"Evaluate:\n" +
				"1. Has the user's request been fully addressed?\n" +
				"2. Are there any remaining blockers or unresolved issues?\n" +
				"3. Would the user likely be satisfied with the current state?\n\n" +
				"Respond in JSON: {\"satisfied\": true/false, \"score\": 0.0-1.0, \"reason\": \"brief explanation\"}\n" +
				"Score interpretation: 0.0-0.3 = unsatisfied, 0.3-0.7 = partially satisfied, 0.7-0.95 = mostly satisfied, 0.95-1.0 = fully satisfied\n" +
				"Only judge based on what is visible in the conversation. Do not assume anything not stated.",
		},
		{
			Role:    "user",
			Content: "Conversation transcript:\n" + formatGoalTranscript(messages),
		},
	}

	var content strings.Builder
	var streamContent string
	tokensUsed := 0
	err := e.client.StreamCompletionWithoutReasoning(ctx, evalMessages, func(ev event.StreamEvent) {
		switch ev.Type {
		case event.EventTypeContent:
			content.WriteString(ev.Content)
		case event.EventTypeStreamContent:
			streamContent = ev.Content
		case event.EventTypeTokenStats:
			if ev.TokenStats != nil {
				tokensUsed = ev.TokenStats.TotalTokens
				if tokensUsed == 0 {
					tokensUsed = ev.TokenStats.InputTokens + ev.TokenStats.OutputTokens
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(content.String())
	if raw == "" {
		raw = strings.TrimSpace(streamContent)
	}
	var parsed struct {
		Satisfied bool    `json:"satisfied"`
		Score     float64 `json:"score"`
		Reason    string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(extractGoalEvalJSON(raw)), &parsed); err != nil {
		// Include raw response snippet in reason for diagnosis
		snippet := raw
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return &SatisfactionEvalResult{
			Satisfied:            false,
			Score:                0.0,
			Reason:               fmt.Sprintf("Evaluator returned invalid JSON (raw: %s)", snippet),
			TokensUsed:           tokensUsed,
			EvaluatorParseFailed: true,
		}, nil
	}
	if strings.TrimSpace(parsed.Reason) == "" {
		parsed.Reason = "No reason provided"
	}
	// Ensure score is in valid range
	if parsed.Score < 0.0 {
		parsed.Score = 0.0
	} else if parsed.Score > 1.0 {
		parsed.Score = 1.0
	}
	return &SatisfactionEvalResult{
		Satisfied:  parsed.Satisfied,
		Score:      parsed.Score,
		Reason:     parsed.Reason,
		TokensUsed: tokensUsed,
	}, nil
}

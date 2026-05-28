package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

func (c *Client) thinkingStatusMessage() string {
	if c.config == nil || c.config.Reasoning == nil || !c.config.Reasoning.IsEffectivelyEnabled() {
		return "正在思考..."
	}
	if c.config.Reasoning.MaxTokens > 0 {
		return fmt.Sprintf("正在思考（推理令牌限制: %d）...", c.config.Reasoning.MaxTokens)
	}
	if c.config.Reasoning.Effort != "" {
		return fmt.Sprintf("正在思考（推理强度: %s）...", c.config.Reasoning.Effort)
	}
	return "正在思考（推理模式）..."
}

func (c *Client) applyReasoningRequestOptions(extraFields map[string]interface{}, tokenStats *TokenStats) bool {
	if c.config == nil || c.config.Reasoning == nil || !c.config.Reasoning.IsEffectivelyEnabled() {
		logger.Debugf("Reasoning tokens disabled or not configured")
		return false
	}

	logger.Debugf("Reasoning tokens enabled for model %s", c.model)

	reasoningEnabled := false
	if c.config.Reasoning.MaxTokens > 0 {
		extraFields["reasoning"] = map[string]interface{}{
			"max_tokens": c.config.Reasoning.MaxTokens,
		}
		logger.Debugf("Using reasoning max_tokens: %d", c.config.Reasoning.MaxTokens)
		reasoningEnabled = true
	} else if c.config.Reasoning.Effort != "" {
		extraFields["reasoning"] = map[string]interface{}{
			"effort": c.config.Reasoning.Effort,
		}
		logger.Debugf("Using reasoning effort level: %s", c.config.Reasoning.Effort)
		reasoningEnabled = true
	} else {
		logger.Warnf("推理功能已启用但未指定effort级别或max_tokens，将使用默认配置")
	}

	if c.config.Reasoning.Exclude {
		extraFields["reasoning_exclude"] = true
		logger.Debugf("Reasoning tokens will be excluded from response")
	}

	if reasoningEnabled && tokenStats != nil {
		tokenStats.SetReasoningEnabled(true, c.config.Reasoning.Effort)
	}

	return reasoningEnabled
}

func shouldFallbackWithoutReasoning(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "invalid api key") {
		return false
	}
	if strings.Contains(msg, "403") || strings.Contains(msg, "forbidden") {
		return false
	}
	if strings.Contains(msg, "429") && (strings.Contains(msg, "insufficient_quota") ||
		strings.Contains(msg, "exceeded your current quota") ||
		strings.Contains(msg, "billing") ||
		strings.Contains(msg, "token-limit")) {
		return false
	}
	return true
}

// needsReasoningContentInMessages checks if the current API provider requires
// reasoning_content field in assistant messages when reasoning/thinking is enabled.
func (c *Client) needsReasoningContentInMessages() bool {
	if c.config == nil || c.config.Reasoning == nil || !c.config.Reasoning.IsEffectivelyEnabled() {
		return false
	}
	provider := c.provider
	if provider.BaseURL == "" {
		provider = NewProviderInfo(c.baseURL)
	}
	return provider.RequiresReasoningContentInMessages()
}

func (c *Client) maybeSetReasoningMessagesOverride(extraFields map[string]interface{}, messages []Message) {
	if extraFields == nil {
		return
	}
	if !c.needsReasoningContentInMessages() {
		return
	}

	openaiMessages := c.convertMessages(messages)
	rawMessages := make([]map[string]interface{}, 0, len(openaiMessages))

	for idx, msg := range openaiMessages {
		msgJSON, err := json.Marshal(msg)
		if err != nil {
			logger.Warnf("Failed to marshal message for reasoning override: %v", err)
			continue
		}

		var msgMap map[string]interface{}
		if err := json.Unmarshal(msgJSON, &msgMap); err != nil {
			logger.Warnf("Failed to unmarshal message for reasoning override: %v", err)
			continue
		}

		if role, ok := msgMap["role"].(string); ok && role == "assistant" {
			reasoning := ""
			if idx < len(messages) && messages[idx].Role == "assistant" {
				if len(messages[idx].ReasoningBlocks) > 0 {
					reasoning = BlocksToText(messages[idx].ReasoningBlocks)
				} else {
					reasoning = messages[idx].Reasoning
				}
			}
			msgMap["reasoning_content"] = reasoning
		}

		rawMessages = append(rawMessages, msgMap)
	}

	if len(rawMessages) > 0 {
		extraFields["messages"] = rawMessages
	}
}

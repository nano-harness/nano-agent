package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// LLMClassifier uses an LLM to classify tool invocations for risk assessment.
type LLMClassifier struct {
	Client       llm.LLMClient
	Model        string
	SystemPrompt string
	Timeout_     time.Duration
}

// NewLLMClassifier creates a new LLM-based classifier.
func NewLLMClassifier(apiKey, baseURL, model string, timeout time.Duration) *LLMClassifier {
	client := llm.NewClient(apiKey, baseURL, model, nil)
	return &LLMClassifier{
		Client:       client,
		Model:        model,
		SystemPrompt: defaultClassifierSystemPrompt,
		Timeout_:     timeout,
	}
}

// Classify implements the Classifier interface.
func (c *LLMClassifier) Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResult, error) {
	if c == nil || c.Client == nil {
		return nil, fmt.Errorf("classifier not initialized")
	}

	// Avoid prompt injection via tool_results (untrusted tool output) being fed
	// back into a classifier prompt.
	params := req.Params
	if params != nil {
		if _, ok := params["tool_results"]; ok {
			copied := make(map[string]interface{}, len(params))
			for k, v := range params {
				if k == "tool_results" {
					continue
				}
				copied[k] = v
			}
			params = copied
		}
	}

	// Build the classification request as structured JSON
	requestJSON, err := json.Marshal(map[string]interface{}{
		"tool_name": req.ToolName,
		"params":    params,
		"work_dir":  req.WorkDir,
		"perm_mode": string(req.PermMode),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	prompt := fmt.Sprintf("%s\n\nClassify this tool invocation:\n\n%s\n\nRespond with JSON only (no markdown fences):",
		c.SystemPrompt, string(requestJSON))

	// Use GenerateContent which returns the text response
	responseText, err := c.Client.GenerateContent(ctx, prompt)
	if err != nil {
		logger.Warnf("LLM classifier error: %v", err)
		return nil, err
	}

	if responseText == "" {
		return nil, fmt.Errorf("empty response from LLM classifier")
	}

	// Strip markdown code fences if present
	content := stripMarkdownFences(strings.TrimSpace(responseText))

	// Parse JSON response
	var result struct {
		ShouldBlock bool    `json:"should_block"`
		Reason      string  `json:"reason"`
		Explanation string  `json:"explanation"`
		Confidence  float64 `json:"confidence"`
		Stage       string  `json:"stage"`
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		logger.Warnf("Failed to parse classifier response: %v\nContent: %s", err, content)
		return nil, fmt.Errorf("failed to parse classifier response: %w", err)
	}

	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = strings.TrimSpace(result.Explanation)
	}

	logger.Debugf("Classifier decision for %s: should_block=%v, confidence=%.2f, reason=%s",
		req.ToolName, result.ShouldBlock, result.Confidence, reason)

	return &ClassifyResult{
		ShouldBlock: result.ShouldBlock,
		Reason:      reason,
		Confidence:  result.Confidence,
		Stage:       result.Stage,
		CachedHit:   false,
	}, nil
}

// Timeout implements the Classifier interface.
func (c *LLMClassifier) Timeout() time.Duration {
	if c.Timeout_ <= 0 {
		return 5 * time.Second
	}
	return c.Timeout_
}

// stripMarkdownFences removes markdown code fences from JSON responses.
func stripMarkdownFences(content string) string {
	content = strings.TrimSpace(content)

	// Remove opening fence
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
	}

	// Remove closing fence
	if strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(content, "```")
	}

	return strings.TrimSpace(content)
}

const defaultClassifierSystemPrompt = `You are a security classifier for code agent tool invocations. Your job is to assess the risk of a tool call and decide whether it should be blocked (require explicit user confirmation) or auto-approved.

## Low-Risk Tools (auto-approve)
These are read-only operations with minimal security impact:
- read_file, list_directory, search_files, file_grep, glob_files
- codebase_search, search_code, view_code
- git status, git log, git diff, git show (read-only git commands)
- web_search, web_fetch (reading public data)

## High-Risk Tools (require confirmation)
These modify state or can cause damage:
- write_file, edit_file, delete_file, patch_file
- run_shell_command, bash (especially with destructive commands)
- git commit, git push, git reset (state-changing git commands)

## Path-Based Risk Assessment
- Operations within the working directory (work_dir) are generally safer
- Operations on paths outside the working directory should be blocked
- Operations on sensitive files (.env, credentials.json, id_rsa, etc.) should be blocked
- Network access (fetching external URLs, making API calls) should be evaluated carefully

## Response Format
You must respond with valid JSON only (no markdown fences, no explanatory text):

{
  "should_block": true/false,
  "reason": "Brief explanation of the decision",
  "confidence": 0.0-1.0,
  "stage": "stage1"
}

## Examples

Input: {"tool_name": "read_file", "params": {"file_path": "/repo/src/main.go"}, "work_dir": "/repo"}
Output: {"should_block": false, "reason": "read-only file access within work directory", "confidence": 0.95, "stage": "stage1"}

Input: {"tool_name": "write_file", "params": {"file_path": "/repo/config.yaml", "content": "..."}, "work_dir": "/repo"}
Output: {"should_block": false, "reason": "write to config file within work directory", "confidence": 0.85, "stage": "stage1"}

Input: {"tool_name": "run_shell_command", "params": {"command": "rm -rf /"}}
Output: {"should_block": true, "reason": "destructive command affecting system root", "confidence": 1.0, "stage": "stage1"}

Input: {"tool_name": "read_file", "params": {"file_path": "/etc/passwd"}}
Output: {"should_block": true, "reason": "accessing sensitive system file outside work directory", "confidence": 0.98, "stage": "stage1"}

Input: {"tool_name": "run_shell_command", "params": {"command": "git status"}, "work_dir": "/repo"}
Output: {"should_block": false, "reason": "read-only git command", "confidence": 0.95, "stage": "stage1"}

Be conservative: when uncertain, prefer should_block=true with lower confidence. The system will fall back to safer defaults on errors.`

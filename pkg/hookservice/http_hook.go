package hookservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// executeHTTPHook posts the canonical hook input to a configured URL and
// converts the JSON response body into a Decision.
//
// Security rules enforced here:
//   - URL must match cfg.URLAllowlist (host glob match), if non-empty.
//   - Header values are sanitized: CR/LF/NUL are stripped to defeat header
//     injection.
//   - Redirects are NOT followed automatically.
//   - The response body is bounded by cfg.MaxResponseKB (default 64KB).
func (s *Service) executeHTTPHook(ctx context.Context, h *Hook, event Event, toolName string, params map[string]interface{}, inputJSON string) (*Decision, error) {
	cfg := h.HTTPConfig
	if cfg == nil || strings.TrimSpace(cfg.URL) == "" {
		return s.decisionForFailure(h, "http hook missing URL"), nil
	}
	if !isURLAllowed(cfg.URL, cfg.URLAllowlist) {
		return s.decisionForFailure(h, "http hook URL not in allowlist"), nil
	}

	method := strings.ToUpper(strings.TrimSpace(cfg.Method))
	if method == "" {
		method = http.MethodPost
	}
	timeout := s.options.Timeout
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	hookInput := Input{
		Event:               event,
		HookEventName:       hookEventName(event),
		SessionID:           stringParam(params, "session_id"),
		TranscriptPath:      stringParam(params, "transcript_path"),
		Cwd:                 firstNonEmpty(stringParam(params, "cwd"), s.options.WorkingDir),
		StopHookActive:      boolParam(params, "stop_hook_active"),
		Iteration:           intParam(params, "iteration"),
		ToolName:            toolName,
		Params:              params,
		WorkingDir:          s.options.WorkingDir,
		EnvAllowlist:        append([]string(nil), cfg.AllowedEnvVars...),
		SandboxEnabled:      false,
		ResourceTimeoutMs:   timeout.Milliseconds(),
		LegacyToolInputJSON: inputJSON,
	}
	body, err := json.Marshal(hookInput)
	if err != nil {
		return s.decisionForFailure(h, fmt.Sprintf("encode payload: %v", err)), nil
	}

	req, err := http.NewRequestWithContext(hookCtx, method, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return s.decisionForFailure(h, fmt.Sprintf("build request: %v", err)), nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "nano-agent-hook/1")
	for k, v := range cfg.Headers {
		req.Header.Set(sanitizeHeader(k), sanitizeHeader(v))
	}

	client := s.options.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return s.decisionForFailure(h, fmt.Sprintf("request: %v", err)), nil
	}
	defer resp.Body.Close()

	maxBytes := int64(64 * 1024)
	if cfg.MaxResponseKB > 0 {
		maxBytes = int64(cfg.MaxResponseKB) * 1024
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return s.decisionForFailure(h, fmt.Sprintf("read response: %v", err)), nil
	}

	if resp.StatusCode >= 400 {
		return s.decisionForFailure(h, fmt.Sprintf("http status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))), nil
	}

	if decision, ok := s.decisionFromStructuredOutput(h, string(raw)); ok {
		return decision, nil
	}
	return &Decision{Action: ActionAllow, Reason: "hook " + h.Name + " allowed (http " + resp.Status + ")"}, nil
}

// isURLAllowed returns true when target's host (case-insensitive) matches one
// of the entries in allowlist, or when allowlist is empty (allow-all).
// Wildcard prefix "*." is supported (e.g. "*.example.com").
func isURLAllowed(target string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, entry := range allowlist {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == host {
			return true
		}
		if strings.HasPrefix(entry, "*.") {
			suffix := entry[1:] // includes leading '.'
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		}
	}
	return false
}

// sanitizeHeader strips CR/LF/NUL bytes that could otherwise be used for
// header-injection attacks.
func sanitizeHeader(value string) string {
	if value == "" {
		return value
	}
	r := strings.NewReplacer("\r", "", "\n", "", "\x00", "")
	return r.Replace(value)
}

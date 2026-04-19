package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/nano-harness/nano-agent/pkg/event"
)

type daemonStreamResult struct {
	response      *daemon.ExecuteResponse
	lastSeq       int64
	lastError     string
	completionOK  *bool
	sawStreaming  bool
	resultBuilder strings.Builder
}

func newDaemonStreamResult(sessionID, runID string) *daemonStreamResult {
	return &daemonStreamResult{
		response: &daemon.ExecuteResponse{
			Success:   true,
			SessionID: sessionID,
			RunID:     runID,
			Status:    "running",
		},
	}
}

func parseTokenStatsFromAny(v interface{}) *event.TokenStats {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var stats event.TokenStats
	if err := json.Unmarshal(b, &stats); err != nil {
		return nil
	}
	return &stats
}

func boolFromAny(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	default:
		return false
	}
}

func stringFromAny(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func executeDaemonCommandStreaming(client *daemon.Client, command, sessionID string, timeout int, includeSteps bool) (*daemon.ExecuteResponse, error) {
	timeout = daemon.NormalizeTaskTimeoutSeconds(timeout)
	startResp, err := client.ExecuteInSession(command, sessionID, timeout, false, true)
	if err != nil {
		return nil, err
	}
	if !startResp.Success {
		return startResp, nil
	}
	result := newDaemonStreamResult(startResp.SessionID, startResp.RunID)
	streamCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+daemon.ClientHTTPGraceSeconds)*time.Second)
	defer cancel()
	_, lastSeq, err := client.SubscribeSessionWithResume(streamCtx, daemon.SubscribeOptions{
		SessionID: startResp.SessionID,
		RunID:     startResp.RunID,
		SinceSeq:  0,
	}, func(msg map[string]interface{}) {
		if seq := daemonSeq(msg); seq > result.lastSeq {
			result.lastSeq = seq
		}
		msgType := strings.TrimSpace(stringFromAny(msg["type"]))
		switch msgType {
		case "stream_content":
			content := stringFromAny(msg["content"])
			if content != "" {
				result.sawStreaming = true
				result.resultBuilder.WriteString(content)
				fmt.Print(content)
			}
		case "content":
			content := stringFromAny(msg["content"])
			if content != "" && !result.sawStreaming {
				result.resultBuilder.WriteString(content)
				fmt.Print(content)
			}
		case "error":
			result.lastError = stringFromAny(msg["error"])
		case "completion":
			ok := boolFromAny(msg["success"])
			result.completionOK = &ok
			result.response.Completed = true
			result.response.Status = stringFromAny(msg["status"])
			if result.response.Status == "" {
				if boolFromAny(msg["success"]) {
					result.response.Status = "completed"
				} else {
					result.response.Status = "error"
				}
			}
			result.response.TokenStats = parseTokenStatsFromAny(msg["token_stats"])
			if !boolFromAny(msg["success"]) {
				if result.lastError == "" {
					result.lastError = stringFromAny(msg["error"])
				}
			}
		}
		if includeSteps {
			ev := parseStreamEventFromMessage(msg)
			if ev != nil {
				switch ev.Type {
				case event.EventTypeTokenStats, event.EventTypeDebug, event.EventTypeSatisfactionEval:
				default:
					result.response.Steps = append(result.response.Steps, *ev)
				}
			}
		}
	})
	result.lastSeq = lastSeq
	if err != nil {
		return nil, err
	}
	resultText := result.resultBuilder.String()
	if result.lastError != "" {
		result.response.Success = false
		result.response.Error = result.lastError
		result.response.Result = resultText
		return result.response, nil
	}
	if strings.TrimSpace(result.response.Status) == "" {
		result.response.Status = "completed"
	}
	result.response.Success = true
	if result.completionOK != nil && !*result.completionOK && strings.TrimSpace(result.lastError) != "" {
		result.response.Success = false
	}
	result.response.Result = resultText
	return result.response, nil
}

func parseStreamEventFromMessage(msg map[string]interface{}) *event.StreamEvent {
	msgType := strings.TrimSpace(stringFromAny(msg["type"]))
	if msgType == "" || msgType == "session_start" || msgType == "completion" || msgType == "status" {
		return nil
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return nil
	}
	var ev event.StreamEvent
	if err := json.Unmarshal(b, &ev); err != nil {
		return nil
	}
	if ev.Type == "" {
		return nil
	}
	return &ev
}

func daemonSeq(msg map[string]interface{}) int64 {
	if v, ok := msg["seq"].(float64); ok {
		return int64(v)
	}
	if v, ok := msg["last_seq"].(float64); ok {
		return int64(v)
	}
	return 0
}

package slash

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

type EventStoreReader interface {
	Since(seq int64, filter func(event.StreamEvent) bool) []event.StreamEvent
}

func BuildDoctorReporter(cfg *config.Config) func() string {
	return func() string {
		activeCfg := cfg
		if activeCfg == nil {
			activeCfg = config.Get()
		}
		var b strings.Builder
		fmt.Fprintf(&b, "config_loaded: %t\n", activeCfg != nil)
		if activeCfg != nil {
			if activeCfg.Sandbox != nil {
				fmt.Fprintf(&b, "sandbox_backend: %s\n", activeCfg.Sandbox.Backend)
				fmt.Fprintf(&b, "sandbox_enabled: %t\n", activeCfg.Sandbox.Enabled)
			} else {
				b.WriteString("sandbox_backend: <not configured>\nsandbox_enabled: <nil>\n")
			}
			if activeCfg.Middleware != nil {
				fmt.Fprintf(&b, "audit_log: %s\n", activeCfg.Middleware.AuditLogPath)
			} else {
				b.WriteString("audit_log: \n")
			}
		}
		b.WriteString("observability_api: /api/v1/events, /api/v1/audit")
		return b.String()
	}
}

func BuildEventsQuerier(store EventStoreReader) func(args string) string {
	return func(args string) string {
		return queryEvents(store, args, false)
	}
}

func BuildAuditQuerier(store EventStoreReader) func(args string) string {
	return func(args string) string {
		return queryEvents(store, args, true)
	}
}

func queryEvents(store EventStoreReader, args string, auditOnly bool) string {
	if store == nil {
		return "⚠️  事件存储未连接。"
	}
	query := parseEventArgs(args)
	filter := func(ev event.StreamEvent) bool {
		if query.sessionID != "" && ev.SessionID != query.sessionID {
			return false
		}
		if query.runID != "" && ev.RunID != query.runID {
			return false
		}
		if query.eventType != "" && string(ev.Type) != query.eventType {
			return false
		}
		if query.sandboxOnly && !sandbox.IsSandboxEventType(string(ev.Type)) {
			return false
		}
		if auditOnly && !isAuditEvent(ev) {
			return false
		}
		return true
	}
	events := store.Since(query.sinceSeq, filter)
	if query.limit > 0 && len(events) > query.limit {
		events = events[len(events)-query.limit:]
	}
	if len(events) == 0 {
		if auditOnly {
			return "ℹ️  暂无审计事件。"
		}
		return "ℹ️  暂无事件。"
	}
	var b strings.Builder
	title := "Events"
	if auditOnly {
		title = "Audit events"
	}
	fmt.Fprintf(&b, "%s (%d):\n", title, len(events))
	for _, ev := range events {
		fmt.Fprintf(&b, "  - #%d %d %s source=%s session=%s", ev.Seq, ev.Timestamp, ev.Type, ev.Source, ev.SessionID)
		if ev.Content != "" {
			fmt.Fprintf(&b, " content=%q", truncateEventText(ev.Content, 120))
		}
		if ev.Error != "" {
			fmt.Fprintf(&b, " error=%q", truncateEventText(ev.Error, 120))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

type eventArgs struct {
	sessionID   string
	runID       string
	eventType   string
	sandboxOnly bool
	sinceSeq    int64
	limit       int
}

func parseEventArgs(args string) eventArgs {
	out := eventArgs{limit: 20}
	fields := strings.Fields(args)
	for i := 0; i < len(fields); i++ {
		key := fields[i]
		next := func() string {
			if i+1 >= len(fields) {
				return ""
			}
			i++
			return fields[i]
		}
		switch {
		case key == "--sandbox":
			out.sandboxOnly = true
		case key == "--session-id" || key == "--session":
			out.sessionID = next()
		case strings.HasPrefix(key, "--session-id="):
			out.sessionID = strings.TrimPrefix(key, "--session-id=")
		case key == "--run-id":
			out.runID = next()
		case strings.HasPrefix(key, "--run-id="):
			out.runID = strings.TrimPrefix(key, "--run-id=")
		case key == "--type":
			out.eventType = next()
		case strings.HasPrefix(key, "--type="):
			out.eventType = strings.TrimPrefix(key, "--type=")
		case key == "--since-seq" || key == "--since":
			out.sinceSeq = parseInt64(next(), out.sinceSeq)
		case strings.HasPrefix(key, "--since-seq="):
			out.sinceSeq = parseInt64(strings.TrimPrefix(key, "--since-seq="), out.sinceSeq)
		case key == "--limit":
			out.limit = parseInt(next(), out.limit)
		case strings.HasPrefix(key, "--limit="):
			out.limit = parseInt(strings.TrimPrefix(key, "--limit="), out.limit)
		}
	}
	return out
}

func isAuditEvent(ev event.StreamEvent) bool {
	if sandbox.IsSandboxEventType(string(ev.Type)) || ev.Type == event.EventTypeError {
		return true
	}
	if ev.Type == event.EventTypeWaitingForUser {
		kind, _ := ev.Metadata["kind"].(string)
		return strings.Contains(kind, "approval")
	}
	if ev.Type == event.EventTypeToolResult || ev.Type == event.EventTypeToolCall {
		if kind, _ := ev.Metadata["kind"].(string); strings.Contains(kind, "approval") || strings.Contains(kind, "permission") {
			return true
		}
		if decision, ok := ev.Metadata["decision"]; ok && decision != nil {
			return true
		}
	}
	return false
}

func parseInt(value string, fallback int) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func parseInt64(value string, fallback int64) int64 {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func truncateEventText(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

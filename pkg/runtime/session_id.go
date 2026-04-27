package runtime

import (
	"fmt"
	"os"
	"strings"
)

// BuildLeadSessionID returns a stable-format lead session ID.
func BuildLeadSessionID(team, source string) string {
	if team == "" {
		team = "default"
	}
	if source == "" {
		source = "chat"
	}
	return fmt.Sprintf("lead-%s-%s-%d", sanitizeSessionPart(team), sanitizeSessionPart(source), os.Getpid())
}

// BuildTeammateSessionID returns a stable-format teammate session ID.
func BuildTeammateSessionID(team, agentName string) string {
	if team == "" {
		team = "default"
	}
	if agentName == "" {
		agentName = "teammate"
	}
	return fmt.Sprintf("teammate-%s-%s-%d", sanitizeSessionPart(team), sanitizeSessionPart(agentName), os.Getpid())
}

// ParseSessionID parses lead/teammate session IDs.
func ParseSessionID(id string) (kind, team, agent string, ok bool) {
	parts := strings.Split(id, "-")
	if len(parts) < 4 {
		return "", "", "", false
	}
	switch parts[0] {
	case "lead":
		return "lead", parts[1], parts[2], true
	case "teammate":
		return "teammate", parts[1], parts[2], true
	default:
		return "", "", "", false
	}
}

func sanitizeSessionPart(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	if s == "" {
		return "default"
	}
	return s
}

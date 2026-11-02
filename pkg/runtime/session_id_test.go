package runtime

import (
	"strings"
	"testing"
)

func TestBuildAndParseSessionIDs(t *testing.T) {
	leadID := BuildLeadSessionID("alpha", "chat")
	if !strings.HasPrefix(leadID, "lead-alpha-chat-") {
		t.Fatalf("lead session ID = %q", leadID)
	}
	kind, team, source, ok := ParseSessionID(leadID)
	if !ok || kind != "lead" || team != "alpha" || source != "chat" {
		t.Fatalf("ParseSessionID(%q) = %q,%q,%q,%v", leadID, kind, team, source, ok)
	}

	teammateID := BuildTeammateSessionID("alpha", "coder")
	if !strings.HasPrefix(teammateID, "teammate-alpha-coder-") {
		t.Fatalf("teammate session ID = %q", teammateID)
	}
	kind, team, agent, ok := ParseSessionID(teammateID)
	if !ok || kind != "teammate" || team != "alpha" || agent != "coder" {
		t.Fatalf("ParseSessionID(%q) = %q,%q,%q,%v", teammateID, kind, team, agent, ok)
	}

	if _, _, _, ok := ParseSessionID("legacy-session"); ok {
		t.Fatal("legacy session ID parsed as swarm session")
	}
}

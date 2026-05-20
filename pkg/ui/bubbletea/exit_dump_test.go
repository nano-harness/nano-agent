package bubbletea

import (
	"strings"
	"testing"
)

// TestDumpHistoryUsesContentNotRendered verifies that the dump output
// includes the verbatim Content field of every message and does not
// substitute the (potentially collapsed) Rendered cache.
func TestDumpHistoryUsesContentNotRendered(t *testing.T) {
	store := NewMessageStore()
	full := "line1\nline2\nline3\nline4\nline5\nline6\nline7"
	fm := NewFormattedMessage("u-1", "user", full)
	fm.SetRendered("[COLLAPSED-CACHE]")
	fm.Collapsed = true
	store.AddMessage(fm)

	dump := dumpFormattedMessages(store)
	if !strings.Contains(dump, "line7") {
		t.Fatalf("dump must include full Content; got:\n%s", dump)
	}
	if strings.Contains(dump, "COLLAPSED-CACHE") {
		t.Fatalf("dump must not use rendered cache; got:\n%s", dump)
	}
}

// TestDumpHistoryIncludesAllRoles ensures user, assistant, tool and
// thinking messages are all represented.
func TestDumpHistoryIncludesAllRoles(t *testing.T) {
	store := NewMessageStore()
	store.AddMessage(NewFormattedMessage("u", "user", "hello"))
	store.AddMessage(NewFormattedMessage("a", "assistant", "world"))
	store.AddMessage(NewFormattedMessage("t", "thinking", "deep thoughts"))
	tm := NewFormattedMessage("tool-1", "tool", "summary line")
	tm.Metadata["tool_name"] = "ls"
	tm.Metadata["tool_status"] = "success"
	tm.Metadata["tool_params"] = map[string]interface{}{"path": "/tmp"}
	tm.Metadata["tool_result"] = "a\nb\nc"
	store.AddMessage(tm)

	dump := dumpFormattedMessages(store)
	for _, want := range []string{"hello", "world", "deep thoughts", "ls", "success", "path: /tmp", "a\nb\nc"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("dump missing %q; got:\n%s", want, dump)
		}
	}
}

// TestDumpToolIgnoresCollapsedState ensures that even when a tool
// message is collapsed in the UI, its full result is dumped on exit.
func TestDumpToolIgnoresCollapsedState(t *testing.T) {
	store := NewMessageStore()
	tm := NewFormattedMessage("tool-1", "tool", "summary")
	tm.Collapsed = true
	tm.Metadata["tool_name"] = "grep"
	tm.Metadata["tool_status"] = "success"
	tm.Metadata["tool_result"] = "r1\nr2\nr3\nr4\nr5\nr6\nr7\nr8"
	store.AddMessage(tm)

	dump := dumpFormattedMessages(store)
	if !strings.Contains(dump, "r8") {
		t.Fatalf("dump must include full tool result regardless of Collapsed; got:\n%s", dump)
	}
	if strings.Contains(dump, "more lines") {
		t.Fatalf("dump must not include collapsed marker; got:\n%s", dump)
	}
}

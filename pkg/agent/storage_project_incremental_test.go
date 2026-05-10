package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

func TestProjectSessionStorageIncrementalAppendAndResume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workingDir := t.TempDir()
	storage, err := NewProjectSessionStorage(workingDir)
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}

	session := NewSessionWithID("incremental")
	session.SetConversationHistory([]llm.Message{{Role: "user", Content: "one"}})
	if err := storage.SaveSession(session); err != nil {
		t.Fatalf("first save: %v", err)
	}
	path := filepath.Join(home, ".nano", "projects", encodeProjectPathWithHash(workingDir), "sessions", "incremental.jsonl")
	firstBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}

	session.SetConversationHistory([]llm.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
	})
	if err := storage.SaveSession(session); err != nil {
		t.Fatalf("second save: %v", err)
	}
	secondBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(secondBytes) <= len(firstBytes) {
		t.Fatal("expected second save to append to journal")
	}

	lastSeq, err := storage.GetLastSeq("incremental")
	if err != nil {
		t.Fatalf("last seq: %v", err)
	}
	if lastSeq != 2 {
		t.Fatalf("expected last seq 2, got %d", lastSeq)
	}
	events, err := storage.LoadEventsFromSeq("incremental", 1)
	if err != nil {
		t.Fatalf("load events from seq: %v", err)
	}
	if len(events) != 1 || events[0].Content != "two" {
		t.Fatalf("unexpected resume events: %#v", events)
	}
}

func TestProjectSessionStorageCompactionMarkerTruncatesLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	storage, err := NewProjectSessionStorage(t.TempDir())
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	session := NewSessionWithID("compact")
	session.SetConversationHistory([]llm.Message{
		{Role: "user", Content: "before"},
		{Role: "assistant", Content: "summary"},
	})
	if err := storage.SaveSession(session); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := storage.WriteCheckpoint("compact", CompactionMarker{LastSeqBeforeCompact: 2, SummaryHash: "x"}); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if err := storage.AppendSessionEvent("compact", SessionEvent{Type: "user_message", Role: "user", Content: "after", Seq: 4}); err != nil {
		t.Fatalf("append: %v", err)
	}
	loaded, err := storage.LoadSession("compact")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	history := loaded.GetConversationHistory()
	if len(history) != 2 || history[0].Content != "summary" || history[1].Content != "after" {
		t.Fatalf("expected summary plus post-marker message, got %#v", history)
	}
}

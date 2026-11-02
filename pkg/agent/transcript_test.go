package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

func TestTranscriptWriterAppend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writer, err := NewTranscriptWriter("session_test")
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}
	defer writer.Close()
	entry := transcriptEntryForMessage("session_test", "/work", "turn1", 1, "user", llm.Message{Role: "user", Content: "hello"})
	if err := writer.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	data, err := os.ReadFile(writer.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"type":"user"`) || !strings.Contains(content, `"ralphIteration":1`) {
		t.Fatalf("unexpected transcript content: %s", content)
	}
}

func TestTranscriptWriterSanitizesSessionPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writer, err := NewTranscriptWriter("../escape")
	if err != nil {
		t.Fatalf("NewTranscriptWriter: %v", err)
	}
	defer writer.Close()
	if !strings.Contains(writer.Path(), ".nano-agent/sessions/.._escape/transcript.jsonl") {
		t.Fatalf("unexpected sanitized path: %s", writer.Path())
	}
}

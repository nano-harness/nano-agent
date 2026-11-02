package preprocessor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileMentionStep_TextFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FileMentionMessages("read #note.txt", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected tool call/result pair, got %d messages", len(result.Messages))
	}
	if result.Messages[0].Role != "assistant" || len(result.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected assistant tool call, got %+v", result.Messages[0])
	}
	if result.Messages[0].ToolCalls[0].Name != "read_file" {
		t.Fatalf("expected read_file call, got %+v", result.Messages[0].ToolCalls[0])
	}
	if result.Messages[1].Role != "tool" || !strings.Contains(result.Messages[1].Content, "hello") {
		t.Fatalf("expected tool result with file content, got %+v", result.Messages[1])
	}
}

func TestFileMentionStep_BinaryRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := FileMentionMessages("read #bin.dat", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 || !strings.Contains(result.Messages[1].Content, "binary files are not supported") {
		t.Fatalf("expected binary rejection, got %+v", result.Messages)
	}
}

func TestFileMentionStep_ImageMultimodal(t *testing.T) {
	dir := t.TempDir()
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	if err := os.WriteFile(filepath.Join(dir, "img.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := FileMentionMessages("see #img.png", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Images) != 1 || result.Images[0].MimeType != "image/png" {
		t.Fatalf("expected one png multimodal image, got %+v", result.Images)
	}
	if !strings.Contains(result.Messages[1].Content, "attached as multimodal content") {
		t.Fatalf("expected image tool result note, got %q", result.Messages[1].Content)
	}
}

func TestFileMentionStep_OversizeRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(MaxFileSizeBytes, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := FileMentionMessages("read #large.txt", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 || !strings.Contains(result.Messages[1].Content, "file too large") {
		t.Fatalf("expected oversize rejection, got %+v", result.Messages)
	}
}

func TestFileMentionStep_LineTruncated(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < MaxFileLines+5; i++ {
		b.WriteString("line\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := FileMentionMessages("read #long.txt", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Messages[1].Content, "File truncated after 2000 lines") {
		t.Fatalf("expected truncation marker, got %q", result.Messages[1].Content)
	}
}

package filesystem

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// buildSimplePDF writes a minimal but valid-enough PDF that contains a single
// FlateDecode-compressed content stream with two text show operations.
func buildSimplePDF(t *testing.T, dir string) string {
	t.Helper()
	body := "BT /F1 12 Tf 72 720 Td (Hello PDF) Tj 0 -14 Td (Second Line) Tj ET"
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var doc bytes.Buffer
	doc.WriteString("%PDF-1.4\n")
	doc.WriteString(fmt.Sprintf("4 0 obj << /Length %d /Filter /FlateDecode >>\n", compressed.Len()))
	doc.WriteString("stream\n")
	doc.Write(compressed.Bytes())
	doc.WriteString("\nendstream\nendobj\n")
	doc.WriteString("trailer << >>\n%%EOF\n")
	path := filepath.Join(dir, "sample.pdf")
	if err := os.WriteFile(path, doc.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDecodePDFTextFallback(t *testing.T) {
	dir := t.TempDir()
	path := buildSimplePDF(t, dir)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := decodePDFTextFallback(data)
	if text == "" {
		t.Fatal("expected fallback to extract some text")
	}
	for _, want := range []string{"Hello PDF", "Second Line"} {
		if !bytes.Contains([]byte(text), []byte(want)) {
			t.Fatalf("expected %q in extracted text, got %q", want, text)
		}
	}
}

func TestReadPDFToolExecute(t *testing.T) {
	dir := t.TempDir()
	path := buildSimplePDF(t, dir)
	tool := NewReadPDFTool(dir, nil)
	res, err := tool.Execute(context.Background(), map[string]interface{}{"file_path": path})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %v: %v", res.Success, res.Error)
	}
	if !bytes.Contains([]byte(res.UserContent), []byte("Hello PDF")) {
		t.Fatalf("UserContent missing extracted text: %q", res.UserContent)
	}
}

func TestReadPDFToolRejectsNonPDF(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "x.txt")
	_ = os.WriteFile(bad, []byte("hi"), 0o644)
	tool := NewReadPDFTool(dir, nil)
	res, _ := tool.Execute(context.Background(), map[string]interface{}{"file_path": bad})
	if res.Success {
		t.Fatal("expected failure for non-PDF input")
	}
}

func TestUnescapePDFLiteral(t *testing.T) {
	got := unescapePDFLiteral(`Hi\nWorld\\\(`)
	want := "Hi\nWorld\\("
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

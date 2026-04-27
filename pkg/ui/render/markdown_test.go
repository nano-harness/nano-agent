package render

import (
	"strings"
	"testing"
)

func TestMarkdown(t *testing.T) {
	out, err := Markdown("# Title\n\n```go\nfmt.Println(\"hi\")\n```", MarkdownOptions{Width: 80})
	if err != nil {
		t.Fatalf("Markdown returned error: %v", err)
	}
	if !strings.Contains(out, "Title") {
		t.Fatalf("rendered output should contain heading, got %q", out)
	}
	if !strings.Contains(out, "fmt") || !strings.Contains(out, "Println") {
		t.Fatalf("rendered output should contain code, got %q", out)
	}
}

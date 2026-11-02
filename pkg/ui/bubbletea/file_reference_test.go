package bubbletea

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFileReferences_Basic(t *testing.T) {
	cases := []struct {
		input string
		want  []FileReference
	}{
		{"hello @README.md world", []FileReference{{Raw: "@README.md", Path: "README.md"}}},
		{"see @docs/guide.md for details", []FileReference{{Raw: "@docs/guide.md", Path: "docs/guide.md"}}},
		{"@a.go and @b.py", []FileReference{
			{Raw: "@a.go", Path: "a.go"},
			{Raw: "@b.py", Path: "b.py"},
		}},
		{"sentence ending @README.md.", []FileReference{{Raw: "@README.md", Path: "README.md"}}},
		{"no references here", nil},
		{"email user@example.com", nil},
	}
	for _, tc := range cases {
		got := ParseFileReferences(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("input=%q: got %d refs, want %d (%+v)", tc.input, len(got), len(tc.want), got)
			continue
		}
		for i := range got {
			if got[i].Path != tc.want[i].Path {
				t.Errorf("input=%q ref[%d]: got path %q want %q", tc.input, i, got[i].Path, tc.want[i].Path)
			}
		}
	}
}

func TestParseFileReferences_LineRange(t *testing.T) {
	got := ParseFileReferences("inspect @src/main.go:10-20 here")
	if len(got) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(got))
	}
	r := got[0]
	if r.Path != "src/main.go" || r.StartLine != 10 || r.EndLine != 20 {
		t.Errorf("unexpected: %+v", r)
	}
}

func TestParseFileReferences_SingleLine(t *testing.T) {
	got := ParseFileReferences("@x.txt:5")
	if len(got) != 1 || got[0].StartLine != 5 || got[0].EndLine != 5 {
		t.Errorf("expected single-line ref, got %+v", got)
	}
}

func TestExpandFileReferences_Whole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("# Title\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ExpandFileReferences("see @note.md please", dir)
	if !strings.Contains(out, "# Title") || !strings.Contains(out, "hello") {
		t.Errorf("expansion missing content: %q", out)
	}
	if !strings.Contains(out, "@note.md") {
		t.Errorf("expansion should keep header: %q", out)
	}
	if !strings.Contains(out, "```markdown") {
		t.Errorf("expansion should label fence: %q", out)
	}
}

func TestExpandFileReferences_Range(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src.go")
	body := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ExpandFileReferences("look at @src.go:2-4", dir)
	if !strings.Contains(out, "line2") || !strings.Contains(out, "line3") || !strings.Contains(out, "line4") {
		t.Errorf("missing range content: %q", out)
	}
	if strings.Contains(out, "line1") || strings.Contains(out, "line5") {
		t.Errorf("range expansion leaked outside [2,4]: %q", out)
	}
}

func TestExpandFileReferences_MissingFileAnnotated(t *testing.T) {
	dir := t.TempDir()
	out := ExpandFileReferences("look at @missing.md", dir)
	if !strings.Contains(out, "@missing.md") {
		t.Errorf("missing reference should remain in output: %q", out)
	}
	if !strings.Contains(out, "[file reference errors]") {
		t.Errorf("expected error annotation, got %q", out)
	}
}

func TestExpandFileReferences_LargeFileAnnotated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxFileReferenceBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ExpandFileReferences("look at @large.txt", dir)
	if !strings.Contains(out, "@large.txt") {
		t.Errorf("large reference should remain in output: %q", out)
	}
	if !strings.Contains(out, "file is too large") {
		t.Errorf("expected size error annotation, got %q", out)
	}
}

func TestSliceLines_OutOfRange(t *testing.T) {
	body := "a\nb\nc\n"
	if got := sliceLines(body, 10, 20); got != "" {
		t.Errorf("out-of-range start should yield empty, got %q", got)
	}
	if got := sliceLines(body, 1, 100); got != "a\nb\nc\n" {
		t.Errorf("out-of-range end should clamp, got %q", got)
	}
}

package filesearch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndex_GitignoreFiltered(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\nbuild/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "artifact.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := NewIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Crawl(); err != nil {
		t.Fatal(err)
	}

	got := idx.Files()
	if len(got) != 2 || got[0] != ".gitignore" || got[1] != "visible.txt" {
		t.Fatalf("unexpected files: %#v", got)
	}
}

func TestIndex_FuzzySearch(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"go.mod", "pkg/ui/model.go", "README.md"} {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := NewIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Crawl(); err != nil {
		t.Fatal(err)
	}

	got := idx.Search("gom", 2)
	if len(got) == 0 || got[0] != "go.mod" {
		t.Fatalf("expected go.mod first, got %#v", got)
	}
}

package filesearch

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
	"github.com/sahilm/fuzzy"
)

var ignoredDirs = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	"node_modules": {},
}

// Index holds a searchable snapshot of files below a workspace root.
type Index struct {
	root    string
	files   []string
	matcher *ignore.GitIgnore
}

// NewIndex creates a file index rooted at root and loads root/.gitignore when present.
func NewIndex(root string) (*Index, error) {
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	idx := &Index{root: absRoot}
	gitignorePath := filepath.Join(absRoot, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		matcher, err := ignore.CompileIgnoreFile(gitignorePath)
		if err != nil {
			return nil, err
		}
		idx.matcher = matcher
	}
	return idx, nil
}

// Root returns the absolute workspace root used by the index.
func (idx *Index) Root() string {
	if idx == nil {
		return ""
	}
	return idx.root
}

// Files returns a copy of the indexed relative file paths.
func (idx *Index) Files() []string {
	if idx == nil {
		return nil
	}
	out := make([]string, len(idx.files))
	copy(out, idx.files)
	return out
}

// Crawl refreshes the index by walking the workspace root.
func (idx *Index) Crawl() error {
	if idx == nil {
		return nil
	}
	files := make([]string, 0)
	err := filepath.WalkDir(idx.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(idx.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if _, ok := ignoredDirs[rel]; ok {
				return filepath.SkipDir
			}
			if idx.ignored(rel + "/") {
				return filepath.SkipDir
			}
			return nil
		}
		if idx.ignored(rel) {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	idx.files = files
	return nil
}

// Search returns up to limit fuzzy matches for query.
func (idx *Index) Search(query string, limit int) []string {
	if idx == nil || limit <= 0 {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		n := min(limit, len(idx.files))
		return append([]string(nil), idx.files[:n]...)
	}
	matches := fuzzy.Find(query, idx.files)
	n := min(limit, len(matches))
	results := make([]string, 0, n)
	for i := 0; i < n; i++ {
		results = append(results, matches[i].Str)
	}
	return results
}

func (idx *Index) ignored(rel string) bool {
	if idx.matcher == nil {
		return false
	}
	return idx.matcher.MatchesPath(rel)
}

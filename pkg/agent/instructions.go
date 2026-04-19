package agent

// InstructionLoader manages hierarchical NANO.md instruction file loading.
// Modeled after Claude Code's CLAUDE.md system:
//
//	Layer 1 (global):    ~/.nano/NANO.md
//	Layer 2 (project):   <project_root>/NANO.md or <project_root>/.nano/NANO.md
//	Layer 3 (directory): <subdir>/NANO.md (on-demand)
//	Layer 4 (local):     <project_root>/NANO.local.md (gitignored)
//
// Rules:
//
//	.nano/rules/*.md  (unconditional: loaded at session start when no "paths" frontmatter)
//	.nano/rules/*.md  (conditional: loaded when paths match, via YAML frontmatter)
import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxImportDepth = 3

// InstructionLoader manages hierarchical NANO.md instruction loading.
type InstructionLoader struct {
	workingDir string
	homeDir    string
	cache      map[string]string
}

// NewInstructionLoader creates a new InstructionLoader rooted at workingDir.
func NewInstructionLoader(workingDir string) *InstructionLoader {
	homeDir, _ := os.UserHomeDir()
	return &InstructionLoader{
		workingDir: workingDir,
		homeDir:    homeDir,
		cache:      make(map[string]string),
	}
}

// LoadAll loads and concatenates all instruction layers:
// global (~/.nano/NANO.md), project (NANO.md or .nano/NANO.md),
// and local (NANO.local.md).
func (il *InstructionLoader) LoadAll() string {
	var parts []string

	// Layer 1: global
	if il.homeDir != "" {
		if content := il.readFile(filepath.Join(il.homeDir, ".nano", "NANO.md")); content != "" {
			parts = append(parts, content)
		}
	}

	// Layer 2: project root NANO.md, then fall back to .nano/NANO.md
	projectFile := filepath.Join(il.workingDir, "NANO.md")
	if _, err := os.Stat(projectFile); err == nil {
		if content := il.readFile(projectFile); content != "" {
			parts = append(parts, content)
		}
	} else {
		if content := il.readFile(filepath.Join(il.workingDir, ".nano", "NANO.md")); content != "" {
			parts = append(parts, content)
		}
	}

	// Layer 4: local overrides (gitignored)
	if content := il.readFile(filepath.Join(il.workingDir, "NANO.local.md")); content != "" {
		parts = append(parts, content)
	}

	return strings.Join(parts, "\n\n")
}

// LoadForDirectory loads NANO.md from a specific directory (Layer 3, on-demand).
func (il *InstructionLoader) LoadForDirectory(dir string) string {
	return il.readFile(filepath.Join(dir, "NANO.md"))
}

// ruleFrontmatter holds parsed YAML frontmatter from a rule file.
type ruleFrontmatter struct {
	hasFrontmatter bool
	paths          []string
}

// parseFrontmatter parses the YAML frontmatter from a rule file.
// Only extracts the "paths:" key; no external YAML library is used.
func parseFrontmatter(content string) ruleFrontmatter {
	if !strings.HasPrefix(content, "---\n") {
		return ruleFrontmatter{}
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return ruleFrontmatter{}
	}
	block := content[4 : 4+end]

	fm := ruleFrontmatter{hasFrontmatter: true}
	lines := strings.Split(block, "\n")
	inPaths := false
	for _, line := range lines {
		if strings.HasPrefix(line, "paths:") {
			inPaths = true
			// inline value: paths: [a, b] or paths: "foo"
			rest := strings.TrimSpace(strings.TrimPrefix(line, "paths:"))
			if rest != "" && rest != "[]" {
				rest = strings.Trim(rest, "[]")
				for _, p := range strings.Split(rest, ",") {
					p = strings.TrimSpace(p)
					p = strings.Trim(p, `"'`)
					if p != "" {
						fm.paths = append(fm.paths, p)
					}
				}
				inPaths = false
			}
			continue
		}
		if inPaths {
			// list item: "  - pattern"
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") {
				p := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
				p = strings.Trim(p, `"'`)
				if p != "" {
					fm.paths = append(fm.paths, p)
				}
			} else if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				// new key – end of paths block
				inPaths = false
			}
		}
	}
	return fm
}

// matchesAnyPath reports whether any of the activeFilePaths matches any pattern.
func matchesAnyPath(patterns []string, activeFilePaths []string) bool {
	for _, pattern := range patterns {
		for _, fp := range activeFilePaths {
			matched, err := filepath.Match(pattern, fp)
			if err == nil && matched {
				return true
			}
			// Also match on base name or partial suffix
			if strings.HasSuffix(fp, pattern) || strings.HasSuffix(filepath.Base(fp), pattern) {
				return true
			}
		}
	}
	return false
}

// LoadRules loads .nano/rules/*.md files.
// Files without "paths" YAML frontmatter are always loaded.
// Files with "paths" frontmatter are only loaded when activeFilePaths match.
func (il *InstructionLoader) LoadRules(activeFilePaths []string) string {
	rulesDir := filepath.Join(il.workingDir, ".nano", "rules")
	entries, err := os.ReadDir(rulesDir)
	if os.IsNotExist(err) || err != nil {
		return ""
	}

	var parts []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(rulesDir, entry.Name())
		content := il.readFile(path)
		if content == "" {
			continue
		}
		fm := parseFrontmatter(content)
		if fm.hasFrontmatter && len(fm.paths) > 0 {
			// Conditional rule: only include if a path matches
			if !matchesAnyPath(fm.paths, activeFilePaths) {
				continue
			}
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}

var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// stripHTMLComments removes <!-- ... --> comment blocks from content.
func (il *InstructionLoader) stripHTMLComments(content string) string {
	return htmlCommentRe.ReplaceAllString(content, "")
}

// resolveImports processes @import "path" or @import 'path' directives in content.
// Imports are resolved relative to the working directory. Max recursion depth is 3.
// Absolute paths and paths that escape the project root (via ..) are rejected for security.
func (il *InstructionLoader) resolveImports(content string, depth int) string {
	if depth >= maxImportDepth {
		return content
	}
	lines := strings.Split(content, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@import ") {
			rest := strings.TrimPrefix(trimmed, "@import ")
			rest = strings.TrimSpace(rest)
			rest = strings.Trim(rest, `"'`)
			if rest != "" && !filepath.IsAbs(rest) {
				importPath := filepath.Join(il.workingDir, rest)
				// Security: reject imports that escape the project root.
				cleanPath := filepath.Clean(importPath)
				cleanRoot := filepath.Clean(il.workingDir)
				if strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) || cleanPath == cleanRoot {
					imported := il.readFileRaw(cleanPath)
					if imported != "" {
						// Apply the same processing pipeline as top-level files.
						imported = il.stripHTMLComments(imported)
						resolved := il.resolveImports(imported, depth+1)
						result = append(result, resolved)
						continue
					}
				}
			}
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// readFile reads a file, strips HTML comments, resolves imports, and caches the result.
func (il *InstructionLoader) readFile(path string) string {
	if cached, ok := il.cache[path]; ok {
		return cached
	}
	raw := il.readFileRaw(path)
	if raw == "" {
		il.cache[path] = ""
		return ""
	}
	content := il.stripHTMLComments(raw)
	content = il.resolveImports(content, 0)
	content = strings.TrimSpace(content)
	il.cache[path] = content
	return content
}

// readFileRaw reads a file without processing, returns "" on error.
func (il *InstructionLoader) readFileRaw(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

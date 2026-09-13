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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

const maxImportDepth = 3

// DefaultInstructionFileMaxBytes caps the size of a single instruction file
// (NANO.md / rules) that is inlined into the system prompt. Industry practice
// (Claude Code ~32KiB, OpenClaw ~12K chars, Hermes 25K chars) silently
// truncates oversized instruction files; nano-agent instead truncates with an
// explicit in-context notice so the model knows content was dropped.
const DefaultInstructionFileMaxBytes = 32 * 1024

// InstructionLoader manages hierarchical NANO.md instruction loading.
type InstructionLoader struct {
	workingDir   string
	homeDir      string
	cache        map[string]string
	maxFileBytes int
}

// NewInstructionLoader creates a new InstructionLoader rooted at workingDir.
func NewInstructionLoader(workingDir string) *InstructionLoader {
	return NewInstructionLoaderWithLimit(workingDir, DefaultInstructionFileMaxBytes)
}

// NewInstructionLoaderWithLimit creates an InstructionLoader with an explicit
// per-file byte budget. Values <= 0 fall back to DefaultInstructionFileMaxBytes.
func NewInstructionLoaderWithLimit(workingDir string, maxFileBytes int) *InstructionLoader {
	if maxFileBytes <= 0 {
		maxFileBytes = DefaultInstructionFileMaxBytes
	}
	homeDir, _ := os.UserHomeDir()
	return &InstructionLoader{
		workingDir:   workingDir,
		homeDir:      homeDir,
		cache:        make(map[string]string),
		maxFileBytes: maxFileBytes,
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
	content = il.enforceFileBudget(path, content)
	il.cache[path] = content
	return content
}

// enforceFileBudget truncates content exceeding the per-file byte budget,
// keeping the head (~80%) and tail (~15%) with an explicit omission marker so
// the model knows instructions were dropped, instead of silently truncating.
func (il *InstructionLoader) enforceFileBudget(path, content string) string {
	if il.maxFileBytes <= 0 || len(content) <= il.maxFileBytes {
		return content
	}
	total := len(content)
	headBytes := il.maxFileBytes * 4 / 5
	tailBytes := il.maxFileBytes / 7
	if headBytes+tailBytes >= total {
		headBytes = total
		tailBytes = 0
	}
	head := content[:headBytes]
	if idx := strings.LastIndexByte(head, '\n'); idx > 0 {
		head = head[:idx]
		headBytes = idx
	}
	if tailBytes > 0 {
		tail := content[total-tailBytes:]
		if idx := strings.IndexByte(tail, '\n'); idx >= 0 && idx+1 < len(tail) {
			tail = tail[idx+1:]
			tailBytes = len(tail)
		}
		logger.Warnf("Instruction file %s exceeds budget (%d bytes > %d); truncated with notice", path, total, il.maxFileBytes)
		return fmt.Sprintf("%s\n\n[... instruction file truncated: showing head %d + tail %d of %d bytes (limit %d); shorten the file or raise instruction_file_max_bytes ...]\n\n%s",
			head, headBytes, tailBytes, total, il.maxFileBytes, tail)
	}
	logger.Warnf("Instruction file %s exceeds budget (%d bytes > %d); truncated with notice", path, total, il.maxFileBytes)
	return fmt.Sprintf("%s\n\n[... instruction file truncated: showing head %d of %d bytes (limit %d); shorten the file or raise instruction_file_max_bytes ...]",
		head, headBytes, total, il.maxFileBytes)
}

// readFileRaw reads a file without processing, returns "" on error.
func (il *InstructionLoader) readFileRaw(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

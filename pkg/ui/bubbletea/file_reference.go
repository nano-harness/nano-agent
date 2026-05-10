package bubbletea

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// FileReference describes a single `@path` (or `@path:start-end`) reference
// found in a user's input. It is the unit consumed by ExpandFileReferences
// when a message is committed.
type FileReference struct {
	// Raw is the verbatim token as it appeared in the input, including the
	// leading "@". This is what gets replaced when expansion is performed.
	Raw string
	// Path is the resolved filesystem path (may still be relative to the
	// caller's working directory).
	Path string
	// StartLine and EndLine are 1-based, inclusive line numbers when the
	// reference contains a range like "@README.md:10-20". A zero StartLine
	// means "no range; include the whole file".
	StartLine int
	EndLine   int
}

const maxFileReferenceBytes = 1 << 20 // 1 MiB keeps prompt expansion bounded.

// fileRefPattern matches `@<path>(:<start>(-<end>)?)?`.
//
// Path is greedy over non-whitespace characters except trailing punctuation
// commonly found in prose (`.,;:!?`) which is stripped by the caller. The
// optional range follows after a single colon and uses 1-based decimal line
// numbers.
//
// Capture groups:
//  1. path token (without leading @)
//  2. optional start line
//  3. optional end line
//
// Examples that match:
//
//	@README.md
//	@docs/guide.md
//	@./relative/path.txt
//	@/abs/path
//	@src/main.go:42
//	@src/main.go:10-20
var fileRefPattern = regexp.MustCompile(`@([^\s@]+?)(?::(\d+)(?:-(\d+))?)?(?:[\s,;]|$)`) // captures: 1=path, 2=start line, 3=end line

// ParseFileReferences scans the input string and returns every well-formed
// `@file` reference it contains. References are returned in the order they
// appear so callers can do a simple sequential replacement when expanding.
//
// Trailing prose punctuation (`.`, `,`, `;`, `:`, `!`, `?`) is trimmed from
// the resolved path so a sentence like "see @README.md." correctly yields
// path "README.md".
func ParseFileReferences(input string) []FileReference {
	var refs []FileReference
	if input == "" {
		return refs
	}
	matches := fileRefPattern.FindAllStringSubmatchIndex(input, -1)
	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1]
		// Reject inline matches like "user@example.com" where the `@` is
		// preceded by a non-whitespace byte. Go's regexp engine has no
		// look-behind, so we enforce this constraint post-hoc.
		if fullStart > 0 {
			prev := input[fullStart-1]
			if prev != ' ' && prev != '\t' && prev != '\n' && prev != '\r' && prev != '(' && prev != '[' {
				continue
			}
		}
		// FullEnd may include the trailing whitespace/punctuation, so we
		// re-derive the actual `@...` slice using the named capture indices.
		raw := input[fullStart:fullEnd]
		// Strip trailing whitespace / separator that the regex consumed.
		raw = strings.TrimRight(raw, " \t\r\n,;")
		path := input[m[2]:m[3]]
		path = strings.TrimRight(path, ".,;:!?")
		if path == "" {
			continue
		}
		ref := FileReference{Raw: raw, Path: path}
		if m[4] >= 0 {
			ref.StartLine, _ = strconv.Atoi(input[m[4]:m[5]])
			if m[6] >= 0 {
				ref.EndLine, _ = strconv.Atoi(input[m[6]:m[7]])
			} else {
				ref.EndLine = ref.StartLine
			}
		}
		refs = append(refs, ref)
	}
	return refs
}

// ExpandFileReferences replaces every `@file` reference in input with a
// fenced code block containing the referenced file's content, anchored at
// the requested line range when present.
//
// Files that cannot be read are left as a plain `@path` token in the output
// and an error annotation is appended after the message body so the user can
// see what went wrong. cwd resolves relative paths.
//
// The expansion keeps the original `@path` token visible at the expansion
// site (as a header) to preserve readability of the user's intent in the
// transcript.
func ExpandFileReferences(input, cwd string) string {
	refs := ParseFileReferences(input)
	if len(refs) == 0 {
		return input
	}
	var (
		out      strings.Builder
		errors   []string
		consumed int
	)
	// Re-find each reference index to avoid double-replacing identical raws.
	for _, ref := range refs {
		idx := strings.Index(input[consumed:], ref.Raw)
		if idx < 0 {
			continue
		}
		idx += consumed
		out.WriteString(input[consumed:idx])
		consumed = idx + len(ref.Raw)

		expanded, err := loadFileSlice(cwd, ref)
		if err != nil {
			errors = append(errors, fmt.Sprintf("- %s: %v", ref.Raw, err))
			out.WriteString(ref.Raw)
			continue
		}
		out.WriteString(expanded)
	}
	out.WriteString(input[consumed:])

	if len(errors) > 0 {
		out.WriteString("\n\n[file reference errors]\n")
		out.WriteString(strings.Join(errors, "\n"))
	}
	return out.String()
}

// loadFileSlice reads the file referenced by ref, optionally narrowing it to
// the requested 1-based line range, and renders it as a markdown fenced code
// block with a small header so the agent sees both the path and the content.
func loadFileSlice(cwd string, ref FileReference) (string, error) {
	abs := ref.Path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("is a directory")
	}
	if info.Size() > maxFileReferenceBytes {
		return "", fmt.Errorf("file is too large (%d bytes > %d)", info.Size(), maxFileReferenceBytes)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	body := string(data)
	header := fmt.Sprintf("@%s", ref.Path)
	if ref.StartLine > 0 {
		header += fmt.Sprintf(":%d", ref.StartLine)
		if ref.EndLine > ref.StartLine {
			header += fmt.Sprintf("-%d", ref.EndLine)
		}
		body = sliceLines(body, ref.StartLine, ref.EndLine)
	}
	lang := languageFromExt(filepath.Ext(ref.Path))
	return fmt.Sprintf("\n\n--- %s ---\n```%s\n%s\n```\n", header, lang, strings.TrimRight(body, "\n")), nil
}

// sliceLines returns the inclusive 1-based line range [start, end] from s.
// Out-of-range start/end values are clamped to the file length so callers
// don't need to pre-validate ranges against file size.
func sliceLines(s string, start, end int) string {
	if start <= 0 {
		start = 1
	}
	lines := strings.Split(s, "\n")
	if start > len(lines) {
		return ""
	}
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if end < start {
		end = start
	}
	return strings.Join(lines[start-1:end], "\n")
}

// languageFromExt is a tiny extension → markdown language label map used to
// produce nicer fenced code blocks. Unknown extensions yield an empty label.
func languageFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".sh", ".bash":
		return "bash"
	case ".md", ".markdown":
		return "markdown"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".html":
		return "html"
	case ".css":
		return "css"
	}
	return ""
}

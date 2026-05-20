package middleware

import (
	"os"
	"path/filepath"
	"strings"
)

var pathTakingCommands = map[string]bool{
	"cat": true, "head": true, "tail": true,
	"less": true, "more": true,
	"grep": true, "rg": true, "ag": true, "awk": true, "sed": true,
	"find": true, "locate": true,
	"wc": true, "file": true, "stat": true,
	"tree": true, "du": true, "diff": true, "cmp": true,
	"go": true, "python": true, "python3": true, "node": true,
}

// IsPathWithinWorkdir reports whether candidate (absolute or relative to
// workdir) resolves to workdir itself or below it. Empty candidate means there
// is no path argument and returns true.
func IsPathWithinWorkdir(workdir, candidate string) bool {
	if candidate == "" {
		return true
	}
	if strings.TrimSpace(workdir) == "" {
		return false
	}

	root := resolvePathBestEffort(workdir, "")
	target := resolvePathBestEffort(candidate, root)
	if root == "" || target == "" {
		return false
	}
	if target == root {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func resolvePathBestEffort(path, base string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		if base == "" {
			if absBase, err := filepath.Abs("."); err == nil {
				base = absBase
			}
		}
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}

	prefix := path
	var suffix []string
	for {
		if info, err := os.Lstat(prefix); err == nil {
			if resolved, err := filepath.EvalSymlinks(prefix); err == nil {
				if !info.IsDir() && len(suffix) == 0 {
					return filepath.Clean(resolved)
				}
				// Reverse suffix slice inline
				reversed := make([]string, len(suffix))
				for i := range suffix {
					reversed[len(suffix)-1-i] = suffix[i]
				}
				parts := append([]string{resolved}, reversed...)
				return filepath.Clean(filepath.Join(parts...))
			}
			break
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			break
		}
		suffix = append(suffix, filepath.Base(prefix))
		prefix = parent
	}
	return path
}

// ExtractShellPathArgs returns the subset of statement arguments that look like
// filesystem paths.
func ExtractShellPathArgs(stmt Statement) []string {
	cmd := cmdBase(stmt.Command)
	var paths []string
	for _, arg := range stmt.Args {
		if arg == "" || strings.HasPrefix(arg, "-") || isEnvAssignment(arg) {
			continue
		}
		if filepath.IsAbs(arg) || arg == "." || arg == ".." ||
			strings.ContainsRune(arg, os.PathSeparator) ||
			pathTakingCommands[cmd] {
			paths = append(paths, arg)
		}
	}
	return paths
}

func isEnvAssignment(arg string) bool {
	idx := strings.IndexByte(arg, '=')
	if idx <= 0 {
		return false
	}
	key := arg[:idx]
	for i, r := range key {
		if !isValidEnvVarNameRune(i, r) {
			return false
		}
	}
	return true
}

func isValidEnvVarNameRune(index int, r rune) bool {
	return r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || index > 0 && r >= '0' && r <= '9'
}

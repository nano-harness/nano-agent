package agent

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/nano-harness/nano-agent/pkg/filesearch"
)

const workspaceScanGitTimeout = 2 * time.Second

// scanWorkspaceFiles returns project files using a tiered strategy:
//  1. git ls-files (fastest, .gitignore-aware) when root is a git repo
//  2. filesearch.Index fallback, which already handles .gitignore and ignored dirs
func scanWorkspaceFiles(ctx context.Context, root string, maxFiles int) ([]string, error) {
	if maxFiles <= 0 {
		return []string{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	if hasGitRepo(absRoot) {
		files, err := gitLsFiles(ctx, absRoot, maxFiles)
		if err == nil {
			return files, nil
		}
	}

	return filesearchIndexFiles(ctx, absRoot, maxFiles)
}

// hasGitRepo reports whether root contains a usable .git directory or file.
func hasGitRepo(root string) bool {
	if root == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

// gitLsFiles runs git ls-files -z with a short timeout and returns up to maxFiles entries.
func gitLsFiles(ctx context.Context, root string, maxFiles int) ([]string, error) {
	if maxFiles <= 0 {
		return []string{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, workspaceScanGitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(output, []byte{0})
	files := make([]string, 0, min(maxFiles, len(parts)))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		files = append(files, filepath.ToSlash(string(part)))
		if len(files) >= maxFiles {
			break
		}
	}
	return files, nil
}

func filesearchIndexFiles(ctx context.Context, root string, maxFiles int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		files []string
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		idx, err := filesearch.NewIndex(root)
		if err != nil {
			ch <- result{err: err}
			return
		}
		if err := idx.Crawl(); err != nil {
			ch <- result{err: err}
			return
		}
		files := idx.Files()
		sort.Strings(files)
		if len(files) > maxFiles {
			files = files[:maxFiles]
		}
		ch <- result{files: files}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.files, res.err
	}
}

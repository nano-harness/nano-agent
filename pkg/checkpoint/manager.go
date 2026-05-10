package checkpoint

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Options configures the FSManager.
type Options struct {
	WorkingDir   string
	BackupRoot   string        // default: ~/.nano/checkpoints
	MaxCount     int           // 0 → 50
	MaxSizeBytes int64         // 0 → 1 GiB
	RetentionAge time.Duration // 0 → 7d
	GitDisable   bool
	Now          func() time.Time
}

// FSManager is the default Manager implementation.
type FSManager struct {
	mu      sync.Mutex
	opts    Options
	indexFn string
}

// NewFSManager constructs a Manager with the given options. WorkingDir is
// required; missing BackupRoot defaults to ~/.nano/checkpoints.
func NewFSManager(opts Options) (*FSManager, error) {
	if strings.TrimSpace(opts.WorkingDir) == "" {
		return nil, errors.New("checkpoint: working dir required")
	}
	if opts.BackupRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("home dir: %w", err)
		}
		opts.BackupRoot = filepath.Join(home, ".nano", "checkpoints")
	}
	if opts.MaxCount <= 0 {
		opts.MaxCount = 50
	}
	if opts.MaxSizeBytes <= 0 {
		opts.MaxSizeBytes = 1 << 30
	}
	if opts.RetentionAge <= 0 {
		opts.RetentionAge = 7 * 24 * time.Hour
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if err := os.MkdirAll(opts.BackupRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create backup root: %w", err)
	}
	return &FSManager{
		opts:    opts,
		indexFn: filepath.Join(opts.BackupRoot, "index.json"),
	}, nil
}

// Snapshot captures the current state and returns the new checkpoint record.
func (m *FSManager) Snapshot(reason, tool string) (*Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := newID()
	cp := &Checkpoint{
		ID:         id,
		CreatedAt:  m.opts.Now().UTC(),
		Reason:     reason,
		Tool:       tool,
		WorkingDir: m.opts.WorkingDir,
	}

	if !m.opts.GitDisable && isGitRepo(m.opts.WorkingDir) {
		stash, err := gitStashCreate(m.opts.WorkingDir)
		if err == nil && stash != "" {
			cp.Strategy = StrategyGitStash
			cp.GitStash = stash
			if err := m.appendIndex(cp); err != nil {
				return nil, err
			}
			return cp, m.enforceRetention()
		}
		// fall through to file-copy on git failure
	}

	dest := filepath.Join(m.opts.BackupRoot, id)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	size, err := copyTree(m.opts.WorkingDir, dest)
	if err != nil {
		_ = os.RemoveAll(dest)
		return nil, err
	}
	cp.Strategy = StrategyFileCopy
	cp.BackupPath = dest
	cp.SizeBytes = size
	if err := m.appendIndex(cp); err != nil {
		return nil, err
	}
	return cp, m.enforceRetention()
}

// List returns checkpoints sorted newest-first.
func (m *FSManager) List() ([]*Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readIndex()
}

// Restore reverts the working tree to the named checkpoint. It is the caller's
// responsibility to confirm with the user beforehand.
func (m *FSManager) Restore(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cps, err := m.readIndex()
	if err != nil {
		return err
	}
	var match *Checkpoint
	for _, cp := range cps {
		if cp.ID == id {
			match = cp
			break
		}
	}
	if match == nil {
		return ErrNotFound
	}
	switch match.Strategy {
	case StrategyGitStash:
		return gitStashApply(m.opts.WorkingDir, match.GitStash)
	case StrategyFileCopy:
		return restoreTree(match.BackupPath, m.opts.WorkingDir)
	default:
		return fmt.Errorf("checkpoint: unknown strategy %q", match.Strategy)
	}
}

// Delete removes the checkpoint with the given ID and frees its storage.
func (m *FSManager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cps, err := m.readIndex()
	if err != nil {
		return err
	}
	keep := cps[:0]
	for _, cp := range cps {
		if cp.ID == id {
			if cp.Strategy == StrategyFileCopy && cp.BackupPath != "" {
				_ = os.RemoveAll(cp.BackupPath)
			}
			continue
		}
		keep = append(keep, cp)
	}
	return m.writeIndex(keep)
}

// Cleanup applies the retention policy.
func (m *FSManager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enforceRetention()
}

// --- internals ---------------------------------------------------------

func (m *FSManager) appendIndex(cp *Checkpoint) error {
	cps, err := m.readIndex()
	if err != nil {
		return err
	}
	cps = append([]*Checkpoint{cp}, cps...)
	return m.writeIndex(cps)
}

func (m *FSManager) readIndex() ([]*Checkpoint, error) {
	data, err := os.ReadFile(m.indexFn)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cps []*Checkpoint
	if err := json.Unmarshal(data, &cps); err != nil {
		return nil, err
	}
	sort.Slice(cps, func(i, j int) bool { return cps[i].CreatedAt.After(cps[j].CreatedAt) })
	return cps, nil
}

func (m *FSManager) writeIndex(cps []*Checkpoint) error {
	tmp := m.indexFn + ".tmp"
	data, err := json.MarshalIndent(cps, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.indexFn)
}

func (m *FSManager) enforceRetention() error {
	cps, err := m.readIndex()
	if err != nil {
		return err
	}
	cutoff := m.opts.Now().Add(-m.opts.RetentionAge)
	keep := cps[:0]
	var totalSize int64
	for _, cp := range cps {
		if cp.CreatedAt.Before(cutoff) {
			if cp.Strategy == StrategyFileCopy && cp.BackupPath != "" {
				_ = os.RemoveAll(cp.BackupPath)
			}
			continue
		}
		if len(keep) >= m.opts.MaxCount {
			if cp.Strategy == StrategyFileCopy && cp.BackupPath != "" {
				_ = os.RemoveAll(cp.BackupPath)
			}
			continue
		}
		if totalSize+cp.SizeBytes > m.opts.MaxSizeBytes && len(keep) > 0 {
			if cp.Strategy == StrategyFileCopy && cp.BackupPath != "" {
				_ = os.RemoveAll(cp.BackupPath)
			}
			continue
		}
		totalSize += cp.SizeBytes
		keep = append(keep, cp)
	}
	return m.writeIndex(keep)
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("cp-%d", time.Now().UnixNano())
	}
	return "cp-" + hex.EncodeToString(b[:])
}

// --- git helpers -------------------------------------------------------

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func gitStashCreate(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "stash", "create")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitStashApply(dir, sha string) error {
	if sha == "" {
		return errors.New("checkpoint: empty stash sha")
	}
	cmd := exec.Command("git", "-C", dir, "stash", "apply", sha)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git stash apply: %v: %s", err, out)
	}
	return nil
}

// --- file-copy helpers -------------------------------------------------

// copyTree recursively copies src to dst, skipping common heavy directories
// such as .git and node_modules. Returns total bytes written.
func copyTree(src, dst string) (int64, error) {
	var total int64
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		base := filepath.Base(path)
		if path != src && (base == ".git" || base == "node_modules" || base == ".nano") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		n, err := copyFile(path, target, info.Mode())
		total += n
		return err
	})
	return total, err
}

func restoreTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		_, err = copyFile(path, target, info.Mode())
		return err
	})
}

func copyFile(src, dst string, mode os.FileMode) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	return io.Copy(out, in)
}

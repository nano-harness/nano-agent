package filesystem

import (
	"path/filepath"
	"sync"
)

// ReadFileState tracks which files have been read by the ReadFileTool in the current session.
// It is shared between ReadFileTool and EditTool so that edit operations can refuse to edit
// files that the model has not explicitly read first, preventing edits based on stale memory.
type ReadFileState struct {
	mu    sync.RWMutex
	files map[string]struct{} // keyed by symlink-resolved absolute path
}

// NewReadFileState creates a new, empty ReadFileState.
func NewReadFileState() *ReadFileState {
	return &ReadFileState{
		files: make(map[string]struct{}),
	}
}

// normalizePath resolves symlinks in absPath for consistent keying.
// Falls back to filepath.Clean when EvalSymlinks fails (e.g. file not yet created).
func normalizePath(absPath string) string {
	if real, err := filepath.EvalSymlinks(absPath); err == nil {
		return real
	}
	return filepath.Clean(absPath)
}

// Mark records that the file at absPath has been successfully read.
func (s *ReadFileState) Mark(absPath string) {
	key := normalizePath(absPath)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[key] = struct{}{}
}

// HasRead reports whether absPath has been read in this session.
func (s *ReadFileState) HasRead(absPath string) bool {
	key := normalizePath(absPath)
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.files[key]
	return ok
}

// Forget removes absPath and any tracked descendants. This is used after filesystem
// mutations so the agent must re-read the affected path before performing another edit.
func (s *ReadFileState) Forget(absPath string) {
	key := normalizePath(absPath)
	prefix := key + string(filepath.Separator)

	s.mu.Lock()
	defer s.mu.Unlock()
	for tracked := range s.files {
		if tracked == key || len(tracked) > len(prefix) && tracked[:len(prefix)] == prefix {
			delete(s.files, tracked)
		}
	}
}

// Reset clears all recorded reads (useful for tests or new sessions).
func (s *ReadFileState) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files = make(map[string]struct{})
}

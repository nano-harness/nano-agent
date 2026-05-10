//go:build unix

package cli

import (
	"os"
	"syscall"
)

// inodeOf returns a stable identifier for the underlying file. On Unix this
// is the inode number from stat(2); used by tailFollow to detect rotation.
func inodeOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st != nil {
		return uint64(st.Ino)
	}
	// Fall back to a value derived from name+size+modtime so non-Unix style
	// FileInfo implementations still allow rotation detection (best effort).
	return uint64(fi.Size()) ^ uint64(fi.ModTime().UnixNano())
}

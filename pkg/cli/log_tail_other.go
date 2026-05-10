//go:build !unix

package cli

import "os"

// inodeOf returns a best-effort identifier for the underlying file on
// non-Unix platforms. Rotation detection on these platforms relies on size
// and modification time only.
func inodeOf(fi os.FileInfo) uint64 {
	return uint64(fi.Size()) ^ uint64(fi.ModTime().UnixNano())
}

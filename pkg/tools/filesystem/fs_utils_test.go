package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidatePathCommon_AbsolutePathContainment verifies that absolute paths
// outside the working directory are rejected unless explicitly whitelisted.
func TestValidatePathCommon_AbsolutePathContainment(t *testing.T) {
	workDir := t.TempDir()

	tests := []struct {
		name         string
		path         string
		allowedPaths []string
		wantErr      bool
	}{
		{
			name:    "relative path within workdir is allowed",
			path:    "file.txt",
			wantErr: false,
		},
		{
			name:    "absolute path within workdir is allowed",
			path:    filepath.Join(workDir, "file.txt"),
			wantErr: false,
		},
		{
			name:    "absolute path outside workdir is rejected",
			path:    "/tmp/outside.txt",
			wantErr: true,
		},
		{
			name:    "absolute path to sensitive file is rejected",
			path:    "/etc/passwd",
			wantErr: true,
		},
		{
			name:         "absolute path outside workdir allowed via allowedPaths",
			path:         "/tmp/outside.txt",
			allowedPaths: []string{"/tmp"},
			wantErr:      false,
		},
		{
			name:    "relative traversal outside workdir is rejected",
			path:    "../escape.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validatePathCommon(workDir, tt.path, tt.allowedPaths)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePathCommon(%q) error = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestValidatePathCommon_SymlinkEscape verifies that a symlink inside the
// working directory pointing outside is rejected.
func TestValidatePathCommon_SymlinkEscape(t *testing.T) {
	workDir := t.TempDir()
	outside := t.TempDir()

	// Create a file in the outside directory.
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside the working directory pointing to the outside dir.
	link := filepath.Join(workDir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks not supported:", err)
	}

	// Accessing escape/secret.txt should be rejected even though the path
	// starts inside workDir.
	_, err := validatePathCommon(workDir, "escape/secret.txt", nil)
	if err == nil {
		t.Error("expected error for symlink escaping workdir, got nil")
	}
}

// TestValidatePathCommon_AllowedPathsSymlinkResolution verifies that the
// allowed path lookup resolves symlinks in the allowedPaths entries (e.g.
// /tmp → /private/tmp on macOS) so the match still works.
func TestValidatePathCommon_AllowedPathsSymlinkResolution(t *testing.T) {
	allowedDir := t.TempDir()

	// Create a temporary allowed file.
	allowedFile := filepath.Join(allowedDir, "allowed.txt")
	if err := os.WriteFile(allowedFile, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	workDir := t.TempDir()

	// allowedPaths contains the potentially-symlinked temp dir path; after
	// resolution it should match the resolved allowedFile path.
	_, err := validatePathCommon(workDir, allowedFile, []string{allowedDir})
	if err != nil {
		t.Errorf("expected allowed path to succeed, got: %v", err)
	}
}

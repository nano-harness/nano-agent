package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/version"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

const (
	// ossBase is the public root of the OSS release mirror.
	ossBase         = "https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano"
	ossLatestURL    = ossBase + "/latest/version.txt"
	ossDownloadBase = ossBase + "/releases"
	httpTimeout     = 30 * time.Second
)

// NewUpdateCommand creates the `nano update` subcommand.
func NewUpdateCommand() *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update nano to the latest version",
		Long: `Check for a newer version of nano and update the binary in-place.

The latest version tag is resolved from the OSS release mirror. The new binary
is downloaded from the same mirror and its SHA256 checksum is verified before
installing to ~/.local/bin/nano (no root/sudo required).`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUpdate(checkOnly)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check for a newer version without downloading")
	return cmd
}

// runUpdate performs the version check and, unless checkOnly is set, downloads
// and replaces the current binary.
func runUpdate(checkOnly bool) error {
	// Self-update is not supported on Windows: the release workflow does not
	// produce Windows binaries and os.Rename over a running .exe is not allowed.
	if runtime.GOOS == "windows" {
		return fmt.Errorf("self-update is not supported on Windows; download the latest release manually from https://github.com/nano-harness/nano-agent/releases")
	}

	current := version.Version

	fmt.Printf("Current version: %s\n", current)
	fmt.Println("Checking for updates...")

	latest, err := fetchLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to fetch latest version: %w", err)
	}
	fmt.Printf("Latest version:  %s\n\n", latest)

	if !isNewer(current, latest) {
		color.Green("✅ Already up to date.")
		return nil
	}

	color.Yellow("⬆️  New version available: %s → %s", current, latest)
	fmt.Println()

	if checkOnly {
		fmt.Printf("Run `nano update` (without --check) to install %s.\n", latest)
		return nil
	}

	// Determine download URL for this platform.
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	artifactName := fmt.Sprintf("nano-%s-%s", goos, goarch)
	downloadURL := fmt.Sprintf("%s/%s/%s", ossDownloadBase, latest, artifactName)
	checksumURL := downloadURL + ".sha256"

	fmt.Printf("Downloading %s ...\n", downloadURL)

	newBinary, err := downloadToTemp(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = os.Remove(newBinary) }()

	// Verify SHA256 checksum before replacing the binary.
	fmt.Println("Verifying checksum...")
	if err := verifyChecksum(newBinary, checksumURL); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Determine installation path: always write to ~/.local/bin/nano so that
	// the update never requires elevated privileges.
	destPath, err := installPath()
	if err != nil {
		return fmt.Errorf("could not determine install path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}

	fmt.Printf("Installing to %s ...\n", destPath)
	if err := replaceBinary(newBinary, destPath); err != nil {
		return fmt.Errorf("failed to install binary: %w", err)
	}

	color.Green("\n✅ nano updated to %s successfully.", latest)
	return nil
}

// fetchLatestVersion reads the latest version tag from the OSS release mirror
// (nano/latest/version.txt).  Using OSS avoids a dependency on the GitHub API.
func fetchLatestVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ossLatestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "nano/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return "", fmt.Errorf("version endpoint returned %s (failed to read body: %w)", resp.Status, readErr)
		}
		snippet := strings.TrimSpace(string(body))
		if snippet == "" {
			return "", fmt.Errorf("version endpoint returned %s", resp.Status)
		}
		return "", fmt.Errorf("version endpoint returned %s: %s", resp.Status, snippet)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", fmt.Errorf("failed to read version: %w", err)
	}
	tag := strings.TrimSpace(string(raw))
	if tag == "" {
		return "", fmt.Errorf("version.txt is empty")
	}
	// Basic sanity check: version tags must start with 'v' and contain at least
	// one dot (e.g. "v1.2.3").  This catches obviously malformed content early.
	if !strings.HasPrefix(tag, "v") || !strings.Contains(tag, ".") {
		return "", fmt.Errorf("version.txt contains an unexpected value: %q", tag)
	}
	return tag, nil
}

// downloadToTemp downloads url into a temporary file and returns its path.
// The caller is responsible for removing the file.
func downloadToTemp(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download server returned HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "nano-update-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = tmp.Close() }()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	return tmp.Name(), nil
}

// verifyChecksum downloads the expected SHA256 checksum from checksumURL and
// compares it against the local file at path.
func verifyChecksum(path, checksumURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build checksum request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch checksum: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksum server returned HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return fmt.Errorf("failed to read checksum: %w", err)
	}
	// The file may be in "hash  filename" format (sha256sum output) or just a bare hash.
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return fmt.Errorf("checksum file is empty or contains only whitespace")
	}
	expectedHash := strings.ToLower(fields[0])

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open binary for hashing: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to hash binary: %w", err)
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}

// replaceBinary atomically replaces dst with the file at src.
// It first copies src into a temporary file in the same directory as dst
// (ensuring the rename is on the same filesystem), fsyncs, then renames into
// place.  This avoids a corrupted binary if the process is interrupted.
func replaceBinary(src, dst string) error {
	if err := os.Chmod(src, 0o755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Write to a temp file in the same directory as dst so the eventual
	// os.Rename is guaranteed to be on the same filesystem.
	dstDir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dstDir, ".nano-update-*")
	if err != nil {
		return fmt.Errorf("failed to create staging file: %w", err)
	}
	tmpPath := tmp.Name()
	// Clean up the staging file on any error path.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	in, err := os.Open(src)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to open source binary: %w", err)
	}
	defer func() { _ = in.Close() }()

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write staging file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to sync staging file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close staging file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("failed to chmod staging file: %w", err)
	}

	// Atomic rename: replaces dst in-place.
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("failed to rename staging file to destination: %w", err)
	}
	success = true
	return nil
}

// installPath returns the canonical install location for the nano binary.
// On all supported platforms (Linux and macOS) this is ~/.local/bin/nano,
// which is writable by the current user without sudo.
func installPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "bin", "nano"), nil
}

// isNewer returns true when latestTag represents a version newer than
// currentTag.  Both tags may optionally carry a leading "v".
//
// Comparison is done numerically on the three semver components (MAJOR.MINOR.PATCH).
// Non-semver strings (e.g. "dev", commit hashes) are treated as "not a release"
// so the function always returns true in that case, prompting an upgrade.
func isNewer(currentTag, latestTag string) bool {
	c := parseSemver(currentTag)
	l := parseSemver(latestTag)
	if c == nil || l == nil {
		// If either cannot be parsed, assume an update is beneficial.
		return true
	}
	for i := range 3 {
		if l[i] > c[i] {
			return true
		}
		if l[i] < c[i] {
			return false
		}
	}
	return false // identical
}

// parseSemver extracts [major, minor, patch] from a string like "v1.2.3" or
// "1.2.3".  Returns nil if the string does not match.
func parseSemver(tag string) []int {
	s := strings.TrimPrefix(tag, "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		// Strip any pre-release suffix (e.g. "3-beta.1" → "3")
		p = strings.SplitN(p, "-", 2)[0]
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil {
			return nil
		}
		nums[i] = n
	}
	return nums
}

package skill

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// SourceType categorises the origin of a skill to be installed.
type SourceType int

const (
	// SourceHTTPFile is a plain SKILL.md file served over HTTP/HTTPS.
	SourceHTTPFile SourceType = iota
	// SourceHTTPArchive is a ZIP or TAR.GZ archive served over HTTP/HTTPS.
	SourceHTTPArchive
	// SourceLocalFile is a local SKILL.md file path.
	SourceLocalFile
	// SourceLocalDir is a local directory containing SKILL.md.
	SourceLocalDir
	// SourceLocalArchive is a local ZIP or TAR.GZ archive.
	SourceLocalArchive
)

const (
	// maxArchiveSize is the maximum size for an archive to be extracted (4 MB).
	maxArchiveSize int64 = 4 * 1024 * 1024
	// maxExtractedFiles is the maximum number of files allowed in an archive.
	maxExtractedFiles = 100
)

// Installer handles multi-source skill installation.
type Installer struct {
	personalDir  string
	maxSkillSize int64
	httpTimeout  time.Duration
}

// NewInstaller creates a new Installer.
func NewInstaller(personalDir string, maxSkillSize int64) *Installer {
	if maxSkillSize <= 0 {
		maxSkillSize = DefaultMaxSkillSize
	}
	return &Installer{
		personalDir:  personalDir,
		maxSkillSize: maxSkillSize,
		httpTimeout:  InstallHTTPTimeout,
	}
}

// Install installs a skill from source (URL, local path, or archive).
// It returns the installed Skill and its raw SKILL.md bytes.
func (inst *Installer) Install(ctx context.Context, source string) (*Skill, []byte, error) {
	if inst.personalDir == "" {
		return nil, nil, fmt.Errorf("personal skills directory is not configured")
	}
	if source == "" {
		return nil, nil, fmt.Errorf("source cannot be empty")
	}

	srcType := inst.detectSourceType(source)

	var skillContent []byte
	var skillName string
	var err error

	switch srcType {
	case SourceHTTPFile:
		skillContent, err = inst.downloadFile(ctx, source)
		if err != nil {
			return nil, nil, fmt.Errorf("download skill: %w", err)
		}
	case SourceHTTPArchive:
		skillName, err = inst.installFromHTTPArchive(ctx, source)
		if err != nil {
			return nil, nil, fmt.Errorf("install from HTTP archive: %w", err)
		}
		// Re-read the installed SKILL.md for the return value
		skillContent, err = os.ReadFile(filepath.Join(inst.personalDir, skillName, "SKILL.md"))
		if err != nil {
			return nil, nil, fmt.Errorf("read installed skill: %w", err)
		}
	case SourceLocalFile:
		skillContent, err = inst.readLocalFile(source)
		if err != nil {
			return nil, nil, fmt.Errorf("read local skill file: %w", err)
		}
	case SourceLocalDir:
		skillName, err = inst.installFromLocalDir(source)
		if err != nil {
			return nil, nil, fmt.Errorf("install from local dir: %w", err)
		}
		skillContent, err = os.ReadFile(filepath.Join(inst.personalDir, skillName, "SKILL.md"))
		if err != nil {
			return nil, nil, fmt.Errorf("read installed skill: %w", err)
		}
	case SourceLocalArchive:
		skillName, err = inst.installFromLocalArchive(source)
		if err != nil {
			return nil, nil, fmt.Errorf("install from local archive: %w", err)
		}
		skillContent, err = os.ReadFile(filepath.Join(inst.personalDir, skillName, "SKILL.md"))
		if err != nil {
			return nil, nil, fmt.Errorf("read installed skill: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported source type")
	}

	// For direct-file installs we still need to parse/save
	if srcType == SourceHTTPFile || srcType == SourceLocalFile {
		sk, err := parseSkillContent(string(skillContent), source)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid skill content: %w", err)
		}
		if err := inst.writeSkillFile(sk.Name, skillContent); err != nil {
			return nil, nil, fmt.Errorf("write skill file: %w", err)
		}
		logger.Infof("Installed skill %q from %s", sk.Name, source)
		return sk, skillContent, nil
	}

	// For archive installs, skillName was set above
	sk, err := parseSkillContent(string(skillContent), source)
	if err != nil {
		return nil, nil, fmt.Errorf("parse installed skill: %w", err)
	}
	logger.Infof("Installed skill %q from archive %s", sk.Name, source)
	return sk, skillContent, nil
}

// detectSourceType determines how to install a skill from the given source string.
func (inst *Installer) detectSourceType(source string) SourceType {
	lower := strings.ToLower(source)

	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
			return SourceHTTPArchive
		}
		return SourceHTTPFile
	}

	// Local paths
	if strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return SourceLocalArchive
	}

	// Check if it's a directory
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		return SourceLocalDir
	}

	return SourceLocalFile
}

// downloadFile downloads a URL and returns its bytes (bounded by maxInstallResponseBytes).
// The caller-provided URL is intentional: skill installation is a user-initiated action
// where the user explicitly specifies the source URL.  The timeout and size limits below
// are the primary mitigations against abuse.
func (inst *Installer) downloadFile(ctx context.Context, rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: inst.httpTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}

	if resp.ContentLength > maxInstallResponseBytes {
		return nil, fmt.Errorf("response too large (%d bytes, max %d)", resp.ContentLength, maxInstallResponseBytes)
	}

	lr := io.LimitReader(resp.Body, maxInstallResponseBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > maxInstallResponseBytes {
		return nil, fmt.Errorf("skill file too large (max %d bytes)", maxInstallResponseBytes)
	}
	return data, nil
}

// readLocalFile reads a local file (bounded by maxSkillSize).
func (inst *Installer) readLocalFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}
	if info.Size() > inst.maxSkillSize {
		return nil, fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), inst.maxSkillSize)
	}
	return os.ReadFile(path)
}

// installFromLocalDir copies SKILL.md (and all other files) from dirPath into
// the personal skills directory.  Returns the skill name.
func (inst *Installer) installFromLocalDir(dirPath string) (string, error) {
	skillMDPath := filepath.Join(dirPath, "SKILL.md")
	data, err := inst.readLocalFile(skillMDPath)
	if err != nil {
		return "", fmt.Errorf("read SKILL.md from dir: %w", err)
	}

	sk, err := parseSkillContent(string(data), dirPath)
	if err != nil {
		return "", fmt.Errorf("parse SKILL.md: %w", err)
	}

	destDir := filepath.Join(inst.personalDir, sk.Name)
	if err := inst.copyDir(dirPath, destDir); err != nil {
		return "", fmt.Errorf("copy skill directory: %w", err)
	}

	return sk.Name, nil
}

// installFromHTTPArchive downloads an archive from URL and extracts into personal dir.
func (inst *Installer) installFromHTTPArchive(ctx context.Context, rawURL string) (string, error) {
	data, err := inst.downloadFile(ctx, rawURL)
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxArchiveSize {
		return "", fmt.Errorf("archive too large (%d bytes, max %d)", len(data), maxArchiveSize)
	}
	return inst.extractArchive(rawURL, data)
}

// installFromLocalArchive reads and extracts a local archive.
func (inst *Installer) installFromLocalArchive(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %q: %w", path, err)
	}
	if info.Size() > maxArchiveSize {
		return "", fmt.Errorf("archive too large (%d bytes, max %d)", info.Size(), maxArchiveSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read archive %q: %w", path, err)
	}

	return inst.extractArchive(path, data)
}

// extractArchive detects the archive type and delegates.
func (inst *Installer) extractArchive(source string, data []byte) (string, error) {
	lower := strings.ToLower(source)
	if strings.HasSuffix(lower, ".zip") {
		return inst.extractZipToPersonal(data)
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return inst.extractTarGzToPersonal(data)
	}
	return "", fmt.Errorf("unsupported archive format (must be .zip or .tar.gz)")
}

// extractZipToPersonal extracts a ZIP archive to a temp dir, validates SKILL.md,
// then moves it into the personal skills directory.
func (inst *Installer) extractZipToPersonal(data []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "nano-skill-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := inst.extractZip(data, tmpDir); err != nil {
		return "", err
	}

	return inst.installFromTempDir(tmpDir)
}

// extractTarGzToPersonal extracts a TAR.GZ archive to a temp dir, validates
// SKILL.md, then moves it into the personal skills directory.
func (inst *Installer) extractTarGzToPersonal(data []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "nano-skill-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := inst.extractTarGz(data, tmpDir); err != nil {
		return "", err
	}

	return inst.installFromTempDir(tmpDir)
}

// installFromTempDir looks for SKILL.md in tempDir (possibly in a subdirectory)
// and installs into the personal dir.
func (inst *Installer) installFromTempDir(tempDir string) (string, error) {
	// Try direct SKILL.md
	skillMDPath, err := findSkillMD(tempDir)
	if err != nil {
		return "", fmt.Errorf("find SKILL.md in archive: %w", err)
	}

	skillDir := filepath.Dir(skillMDPath)
	data, err := os.ReadFile(skillMDPath)
	if err != nil {
		return "", fmt.Errorf("read SKILL.md: %w", err)
	}

	sk, err := parseSkillContent(string(data), skillMDPath)
	if err != nil {
		return "", fmt.Errorf("parse SKILL.md: %w", err)
	}

	destDir := filepath.Join(inst.personalDir, sk.Name)
	if err := inst.copyDir(skillDir, destDir); err != nil {
		return "", fmt.Errorf("copy extracted skill: %w", err)
	}

	return sk.Name, nil
}

// extractZip extracts a ZIP archive into destDir (flat, path-safe).
func (inst *Installer) extractZip(data []byte, destDir string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	if len(r.File) > maxExtractedFiles {
		return fmt.Errorf("archive contains too many files (%d, max %d)", len(r.File), maxExtractedFiles)
	}

	for _, f := range r.File {
		if err := inst.extractZipFile(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func (inst *Installer) extractZipFile(f *zip.File, destDir string) error {
	// Sanitize path to prevent zip-slip (path traversal).
	cleanName := filepath.Clean(f.Name)
	destPath := filepath.Join(destDir, cleanName)
	rel, err := filepath.Rel(filepath.Clean(destDir), destPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("unsafe path in archive: %q", f.Name)
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(destPath, 0o755)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %q: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	lr := io.LimitReader(rc, inst.maxSkillSize+1)
	content, err := io.ReadAll(lr)
	if err != nil {
		return fmt.Errorf("read zip entry %q: %w", f.Name, err)
	}
	if int64(len(content)) > inst.maxSkillSize {
		return fmt.Errorf("file %q in archive exceeds size limit", f.Name)
	}

	return os.WriteFile(destPath, content, 0o644)
}

// extractTarGz extracts a TAR.GZ archive into destDir (path-safe).
func (inst *Installer) extractTarGz(data []byte, destDir string) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	fileCount := 0

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		fileCount++
		if fileCount > maxExtractedFiles {
			return fmt.Errorf("archive contains too many files (max %d)", maxExtractedFiles)
		}

		cleanName := filepath.Clean(hdr.Name)
		destPath := filepath.Join(destDir, cleanName)
		// Verify the destination is inside destDir to prevent tar-slip.
		rel, relErr := filepath.Rel(filepath.Clean(destDir), destPath)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("unsafe path in archive: %q", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			lr := io.LimitReader(tr, inst.maxSkillSize+1)
			content, err := io.ReadAll(lr)
			if err != nil {
				return fmt.Errorf("read tar entry %q: %w", hdr.Name, err)
			}
			if int64(len(content)) > inst.maxSkillSize {
				return fmt.Errorf("file %q in archive exceeds size limit", hdr.Name)
			}
			if err := os.WriteFile(destPath, content, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// findSkillMD finds the first SKILL.md file inside baseDir (up to 2 levels deep).
func findSkillMD(baseDir string) (string, error) {
	// Check directly
	direct := filepath.Join(baseDir, "SKILL.md")
	if _, err := os.Stat(direct); err == nil {
		return direct, nil
	}

	// Check one level of subdirectories
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", fmt.Errorf("read dir %q: %w", baseDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			candidate := filepath.Join(baseDir, e.Name(), "SKILL.md")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("no SKILL.md found in archive")
}

// writeSkillFile writes SKILL.md content for the named skill into personalDir.
func (inst *Installer) writeSkillFile(name string, data []byte) error {
	skillDir := filepath.Join(inst.personalDir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	return os.WriteFile(filepath.Join(skillDir, "SKILL.md"), data, 0o644)
}

// copyFile copies a single file from src to dst using io.Copy to avoid loading
// the entire file into memory.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %q: %w", src, err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		_ = srcFile.Close()
		return fmt.Errorf("create %q: %w", dst, err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		_ = srcFile.Close()
		return fmt.Errorf("copy %q to %q: %w", src, dst, err)
	}
	if err := dstFile.Close(); err != nil {
		_ = srcFile.Close()
		return fmt.Errorf("close %q: %w", dst, err)
	}
	if err := srcFile.Close(); err != nil {
		return fmt.Errorf("close %q: %w", src, err)
	}
	return nil
}

// copyDir recursively copies src into dst (overwriting existing files).
// Files are copied using io.Copy to avoid loading large files fully into memory.
func (inst *Installer) copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read dir %q: %w", src, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := inst.copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

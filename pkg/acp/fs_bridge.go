package acp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// FSBridge bridges nano-agent filesystem tools to ACP fs/* operations
type FSBridge struct {
	acpSessionID    string
	transport       *Transport
	workdir         string
	fsMode          FSMode
	clientHasFSCaps bool
}

// NewFSBridge creates a new filesystem bridge
func NewFSBridge(acpSessionID string, transport *Transport, workdir string, fsMode FSMode, clientHasFSCaps bool) *FSBridge {
	return &FSBridge{
		acpSessionID:    acpSessionID,
		transport:       transport,
		workdir:         workdir,
		fsMode:          fsMode,
		clientHasFSCaps: clientHasFSCaps,
	}
}

// ReadFile implements ACP fs/read operation
// Depending on FSMode, reads from Client or local filesystem
func (b *FSBridge) ReadFile(ctx context.Context, path string) (string, error) {
	// Decide whether to use ACP RPC or local filesystem
	useRPC := b.shouldUseRPC()

	if useRPC {
		return b.readFileFromClient(ctx, path, 0, 0)
	}
	return b.readFileLocal(ctx, path)
}

// shouldUseRPC determines whether to use RPC based on FSMode and client capabilities
func (b *FSBridge) shouldUseRPC() bool {
	switch b.fsMode {
	case FSModeACP:
		return true
	case FSModeLocal:
		return false
	case FSModeAuto:
		return b.clientHasFSCaps
	default:
		return false
	}
}

// readFileFromClient sends fs/read_text_file RPC to the Client
func (b *FSBridge) readFileFromClient(ctx context.Context, path string, line, limit int) (string, error) {
	if !b.clientHasFSCaps {
		return "", fmt.Errorf("client does not support fs capabilities")
	}

	logger.Infof("ACP: Reading file from client: %s", path)

	params := map[string]interface{}{
		"sessionId": b.acpSessionID,
		"path":      path,
	}
	if line > 0 {
		params["line"] = line
	}
	if limit > 0 {
		params["limit"] = limit
	}

	resp, err := b.transport.SendRPCRequest("fs/read_text_file", params)
	if err != nil {
		return "", fmt.Errorf("RPC call failed: %w", err)
	}

	// Extract content from response
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid response format")
	}

	content, ok := resultMap["content"].(string)
	if !ok {
		return "", fmt.Errorf("content field missing or not a string")
	}

	return content, nil
}

// readFileLocal reads file from local filesystem
func (b *FSBridge) readFileLocal(ctx context.Context, path string) (string, error) {
	absPath := b.resolvePath(path)

	logger.Infof("ACP: Reading file locally: %s", absPath)

	// Check if file exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s", path)
		}
		return "", fmt.Errorf("stat file: %w", err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", path)
	}

	// Read file
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	return string(content), nil
}

// WriteFile implements ACP fs/write operation
// Depending on FSMode, writes to Client or local filesystem
func (b *FSBridge) WriteFile(ctx context.Context, path string, content string) error {
	// Decide whether to use ACP RPC or local filesystem
	useRPC := b.shouldUseRPC()

	if useRPC {
		return b.writeFileToClient(ctx, path, content)
	}
	return b.writeFileLocal(ctx, path, content)
}

// writeFileToClient sends fs/write_text_file RPC to the Client
func (b *FSBridge) writeFileToClient(ctx context.Context, path, content string) error {
	if !b.clientHasFSCaps {
		return fmt.Errorf("client does not support fs capabilities")
	}

	logger.Infof("ACP: Writing file to client: %s", path)

	params := map[string]interface{}{
		"sessionId": b.acpSessionID,
		"path":      path,
		"content":   content,
	}

	resp, err := b.transport.SendRPCRequest("fs/write_text_file", params)
	if err != nil {
		return fmt.Errorf("RPC call failed: %w", err)
	}

	// Check success
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid response format")
	}

	success, _ := resultMap["success"].(bool)
	if !success {
		return fmt.Errorf("write operation failed")
	}

	return nil
}

// writeFileLocal writes file to local filesystem
func (b *FSBridge) writeFileLocal(ctx context.Context, path string, content string) error {
	absPath := b.resolvePath(path)

	logger.Infof("ACP: Writing file locally: %s", absPath)

	// Ensure parent directory exists
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// ListFiles implements ACP fs/list operation
func (b *FSBridge) ListFiles(ctx context.Context, path string) ([]FSEntry, error) {
	absPath := b.resolvePath(path)

	logger.Infof("ACP: Listing directory: %s", absPath)

	// Check if path exists and is a directory
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		return nil, fmt.Errorf("stat path: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", path)
	}

	// Read directory entries
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	// Convert to FSEntry format
	result := make([]FSEntry, 0, len(entries))
	for _, entry := range entries {
		fsEntry := FSEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
		}

		if entryInfo, err := entry.Info(); err == nil {
			fsEntry.Size = entryInfo.Size()
			fsEntry.ModTime = entryInfo.ModTime().Unix()
		}

		result = append(result, fsEntry)
	}

	return result, nil
}

// DeleteFile implements ACP fs/delete operation
func (b *FSBridge) DeleteFile(ctx context.Context, path string) error {
	absPath := b.resolvePath(path)

	logger.Infof("ACP: Deleting file: %s", absPath)

	// Check if file exists
	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", path)
		}
		return fmt.Errorf("stat file: %w", err)
	}

	// Delete file
	if err := os.Remove(absPath); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}

	return nil
}

// resolvePath converts a relative path to an absolute path based on workdir
func (b *FSBridge) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(b.workdir, path)
}

// FSEntry represents a filesystem entry
type FSEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

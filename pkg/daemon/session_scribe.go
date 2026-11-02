package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// SessionScribe records session events and metadata
type SessionScribe struct {
	sessionID  string
	localPath  string
	ossKey     string
	bucket     *oss.Bucket
	file       *os.File
	writer     *bufio.Writer
	mutex      sync.Mutex
	syncTimer  *time.Timer
	syncNeeded bool
	closed     bool
}

func getRuntimeSessionsDir() string {
	if runtime.GOOS == "darwin" {
		return "/tmp/nano-agent/sessions"
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".nano", "sessions")
}

// NewSessionScribe creates a new session scribe
func NewSessionScribe(sessionID string) (*SessionScribe, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("sessionID is required")
	}

	sessionDir := filepath.Join(getRuntimeSessionsDir(), sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	localPath := filepath.Join(sessionDir, "journal.jsonl")
	f, err := os.OpenFile(localPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open journal file: %w", err)
	}

	ossKey := fmt.Sprintf("sessions/%s/journal.jsonl", sessionID)

	scribe := &SessionScribe{
		sessionID:  sessionID,
		localPath:  localPath,
		ossKey:     ossKey,
		file:       f,
		writer:     bufio.NewWriter(f),
		syncNeeded: false,
		closed:     false,
	}

	cfg := config.Get().OSS
	if cfg != nil && cfg.Enabled {
		client, err := oss.New(cfg.NormalizedEndpoint(), cfg.AccessKeyID, cfg.AccessKeySecret)
		if err == nil {
			bucket, err := client.Bucket(cfg.DefaultBucket)
			if err == nil {
				scribe.bucket = bucket
			} else {
				logger.Warnf("SessionScribe failed to get bucket: %v", err)
			}
		} else {
			logger.Warnf("SessionScribe failed to create OSS client: %v", err)
		}
	}

	return scribe, nil
}

// SaveMetadata saves session metadata
func (s *SessionScribe) SaveMetadata(metadata map[string]interface{}) error {
	if s == nil {
		return nil
	}

	metadataPath := filepath.Join(filepath.Dir(s.localPath), "metadata.json")

	tmpPath := metadataPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create metadata tmp file: %w", err)
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(metadata); err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}

	if err := os.Rename(tmpPath, metadataPath); err != nil {
		return fmt.Errorf("failed to save metadata file: %w", err)
	}

	if s.bucket != nil {
		metaKey := fmt.Sprintf("sessions/%s/metadata.json", s.sessionID)
		if err := s.bucket.PutObjectFromFile(metaKey, metadataPath); err != nil {
			logger.Warnf("SessionScribe failed to upload metadata to OSS: %v", err)
		}
		go s.sendCallback(metadata)
	}

	return nil
}

func (s *SessionScribe) sendCallback(metadata map[string]interface{}) {
	cfg := config.Get().OSS
	if cfg == nil || !cfg.Enabled || cfg.CallbackURL == "" {
		return
	}

	callbackData := map[string]interface{}{
		"id":       s.sessionID,
		"oss_path": fmt.Sprintf("sessions/%s.json", s.sessionID),
	}

	if title := getString(metadata, "title"); title != "" {
		callbackData["title"] = title
	}
	if status := getString(metadata, "status"); status != "" {
		callbackData["status"] = status
	}
	username := getString(metadata, "username")
	if username == "" {
		username = "向上"
	}
	callbackData["username"] = username
	if val := getString(metadata, "created_at"); val != "" {
		callbackData["created_at"] = val
	} else if val := getString(metadata, "start_time"); val != "" {
		callbackData["created_at"] = val
	}
	if val := getString(metadata, "updated_at"); val != "" {
		callbackData["updated_at"] = val
	} else if val := getString(metadata, "end_time"); val != "" {
		callbackData["updated_at"] = val
	} else {
		callbackData["updated_at"] = time.Now().Format(time.RFC3339)
	}
	callbackData["message_count"] = getInt(metadata, "message_count")
	if usage, ok := metadata["token_usage"].(map[string]interface{}); ok {
		if val := getInt(usage, "input_tokens"); val > 0 {
			callbackData["input_tokens"] = val
		}
		if val := getInt(usage, "output_tokens"); val > 0 {
			callbackData["output_tokens"] = val
		}
		if val := getInt(usage, "total_tokens"); val > 0 {
			callbackData["total_tokens"] = val
		}
	} else if usage, ok := metadata["token_usage"].(*event.TokenStats); ok {
		callbackData["input_tokens"] = usage.InputTokens
		callbackData["output_tokens"] = usage.OutputTokens
		callbackData["total_tokens"] = usage.TotalTokens
	}

	data, err := json.Marshal(callbackData)
	if err != nil {
		logger.Errorf("Failed to marshal session callback data: %v", err)
		return
	}

	req, err := http.NewRequest("POST", cfg.CallbackURL, bytes.NewReader(data))
	if err != nil {
		logger.Errorf("Failed to create session callback request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.CallbackToken != "" {
		req.Header.Set("X-Callback-Token", cfg.CallbackToken)
	}
	req.Header.Set("User-Agent", "NanoAgent/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Errorf("Failed to send session callback to %s: %v", cfg.CallbackURL, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		logger.Warnf("Session callback failed: status=%d body=%s", resp.StatusCode, string(body))
	}
}

// WriteEvent writes an event to the session log
func (s *SessionScribe) WriteEvent(ev event.StreamEvent) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.closed {
		return fmt.Errorf("scribe is closed")
	}

	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := s.writer.Write(data); err != nil {
		return err
	}
	if err := s.writer.WriteByte('\n'); err != nil {
		return err
	}

	s.syncNeeded = true

	if isSignificantEvent(ev) {
		s.flushAndSyncLocked()
	} else if s.syncTimer == nil {
		s.syncTimer = time.AfterFunc(5*time.Second, s.Sync)
	}

	return nil
}

// Sync forces a sync of the session log
func (s *SessionScribe) Sync() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.flushAndSyncLocked()
}

func (s *SessionScribe) flushAndSyncLocked() {
	if !s.syncNeeded || s.closed {
		return
	}

	if err := s.writer.Flush(); err != nil {
		logger.Errorf("SessionScribe flush failed: %v", err)
		return
	}

	if s.bucket != nil {
		if err := s.bucket.PutObjectFromFile(s.ossKey, s.localPath); err != nil {
			logger.Errorf("SessionScribe OSS sync failed: %v", err)
		} else {
			s.syncNeeded = false
		}
	}

	if s.syncTimer != nil {
		s.syncTimer.Stop()
		s.syncTimer = nil
	}
}

// Close closes the session scribe
func (s *SessionScribe) Close() {
	s.mutex.Lock()
	if s.closed {
		s.mutex.Unlock()
		return
	}
	if s.syncTimer != nil {
		s.syncTimer.Stop()
		s.syncTimer = nil
	}
	s.flushAndSyncLocked()
	s.closed = true
	if s.writer != nil {
		_ = s.writer.Flush()
	}
	if s.file != nil {
		_ = s.file.Close()
	}
	s.mutex.Unlock()
}

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type ossBucket interface {
	PutObject(objectKey string, reader io.Reader, options ...oss.Option) error
	GetObject(objectKey string, options ...oss.Option) (io.ReadCloser, error)
	IsObjectExist(objectKey string, options ...oss.Option) (bool, error)
	DeleteObject(objectKey string, options ...oss.Option) error
}

// SessionStorage defines the interface for session persistence
type SessionStorage interface {
	SaveSession(session *Session) error
	LoadSession(id string) (*Session, error)
	ListSessions() ([]string, error)
	ListSessionInfos() ([]SessionInfo, error)
	DeleteSession(id string) error
}

// IncrementalSessionStorage optionally supports append-only JSONL persistence and resume.
type IncrementalSessionStorage interface {
	SessionStorage
	AppendSessionEvent(sessionID string, event SessionEvent) error
	LoadEventsFromSeq(sessionID string, fromSeq int64) ([]SessionEvent, error)
	WriteCheckpoint(sessionID string, marker CompactionMarker) error
	GetLastSeq(sessionID string) (int64, error)
}

// OSSSessionStorage implements SessionStorage using Aliyun OSS
type OSSSessionStorage struct {
	client *oss.Client
	bucket ossBucket
	config *config.OSSConfig
}

// NewOSSSessionStorage creates a new OSS session storage
func NewOSSSessionStorage(cfg *config.OSSConfig) (*OSSSessionStorage, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("OSS configuration is nil or disabled")
	}

	client, err := oss.New(cfg.NormalizedEndpoint(), cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create OSS client: %w", err)
	}

	bucket, err := client.Bucket(cfg.DefaultBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket: %w", err)
	}

	return &OSSSessionStorage{
		client: client,
		bucket: bucket,
		config: cfg,
	}, nil
}

// SaveSession saves a session to OSS
func (s *OSSSessionStorage) SaveSession(session *Session) error {
	session.mutex.RLock()
	data, err := json.MarshalIndent(session, "", "  ")
	session.mutex.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	objectKey := fmt.Sprintf("sessions/%s.json", session.ID)
	err = s.bucket.PutObject(objectKey, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("failed to put object to OSS: %w", err)
	}

	return nil
}

// LoadSession loads a session from OSS
func (s *OSSSessionStorage) LoadSession(id string) (*Session, error) {
	objectKey := fmt.Sprintf("sessions/%s.json", id)

	exists, err := s.bucket.IsObjectExist(objectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check object existence: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("session not found in OSS")
	}

	body, err := s.bucket.GetObject(objectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get object from OSS: %w", err)
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read object body: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}
	if session.State == "" {
		session.State = SessionStateIdle
	}
	if session.StateChangedAt.IsZero() {
		session.StateChangedAt = session.LastActiveAt
	}
	session.SanitizeMessageSequence()

	return &session, nil
}

// ListSessions lists all sessions in OSS (Not supported/efficient anymore, returns empty)
func (s *OSSSessionStorage) ListSessions() ([]string, error) {
	// Return empty list as listing is now handled by Java backend (database)
	return []string{}, nil
}

// DeleteSession deletes a session from OSS
func (s *OSSSessionStorage) DeleteSession(id string) error {
	mainKey := fmt.Sprintf("sessions/%s.json", id)
	metaKey := fmt.Sprintf("sessions/%s/metadata.json", id)
	journalKey := fmt.Sprintf("sessions/%s/journal.jsonl", id)

	if err := s.bucket.DeleteObject(mainKey); err != nil && !isNoSuchKey(err) && !isAccessDenied(err) {
		return fmt.Errorf("failed to delete object: %w", err)
	}
	if err := s.bucket.DeleteObject(metaKey); err != nil && !isNoSuchKey(err) && !isAccessDenied(err) {
		return fmt.Errorf("failed to delete metadata object: %w", err)
	}
	if err := s.bucket.DeleteObject(journalKey); err != nil && !isNoSuchKey(err) && !isAccessDenied(err) {
		return fmt.Errorf("failed to delete journal object: %w", err)
	}
	return nil
}

// SessionInfo contains summary information about a session
type SessionInfo struct {
	ID           string `json:"id"`
	Title        string `json:"title,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	WorkingDir   string `json:"working_dir,omitempty"`
	MessageCount int    `json:"message_count,omitempty"`
	Summary      string `json:"summary,omitempty"`
	State        string `json:"state,omitempty"`
	LastSeq      int64  `json:"last_seq,omitempty"`
}

// ListSessionInfos lists session information
func (s *OSSSessionStorage) ListSessionInfos() ([]SessionInfo, error) {
	// Return empty list
	return []SessionInfo{}, nil
}

func isNoSuchKey(err error) bool {
	var se oss.ServiceError
	if errors.As(err, &se) {
		return se.Code == "NoSuchKey" || se.Code == "NoSuchObject"
	}
	return strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "NoSuchObject")
}

func isAccessDenied(err error) bool {
	var se oss.ServiceError
	if errors.As(err, &se) {
		return se.Code == "AccessDenied" || se.Code == "Forbidden"
	}
	msg := err.Error()
	return strings.Contains(msg, "AccessDenied") || strings.Contains(msg, "Forbidden")
}

type LocalSessionStorage struct { //nolint:revive
	dir string
}

func NewLocalSessionStorage(dir string) *LocalSessionStorage { //nolint:revive
	if strings.TrimSpace(dir) != "" {
		return &LocalSessionStorage{dir: dir}
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return &LocalSessionStorage{dir: ".nano/sessions"}
	}
	return &LocalSessionStorage{dir: filepath.Join(home, ".nano", "sessions")}
}

// SaveSession saves a session to local disk
func (s *LocalSessionStorage) SaveSession(session *Session) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}

	session.mutex.RLock()
	data, err := json.MarshalIndent(session, "", "  ")
	session.mutex.RUnlock()
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("failed to create sessions dir: %w", err)
	}

	path := filepath.Join(s.dir, session.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("failed to write session: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if mkErr := os.MkdirAll(s.dir, 0755); mkErr == nil {
				if retryErr := os.Rename(tmp, path); retryErr == nil {
					return nil
				}
			}
		}
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to commit session: %w", err)
	}
	return nil
}

func (s *LocalSessionStorage) LoadSession(id string) (*Session, error) { //nolint:revive
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("session id is empty")
	}

	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}
	if session.State == "" {
		session.State = SessionStateIdle
	}
	if session.StateChangedAt.IsZero() {
		session.StateChangedAt = session.LastActiveAt
	}
	session.SanitizeMessageSequence()
	return &session, nil
}

func (s *LocalSessionStorage) ListSessions() ([]string, error) { //nolint:revive
	infos, err := s.ListSessionInfos()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.ID)
	}
	return out, nil
}

func (s *LocalSessionStorage) ListSessionInfos() ([]SessionInfo, error) { //nolint:revive
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionInfo{}, nil
		}
		return nil, err
	}

	type header struct {
		ID        string                 `json:"id"`
		CreatedAt time.Time              `json:"created_at"`
		Metadata  map[string]interface{} `json:"Metadata"`
	}

	out := make([]SessionInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		id := strings.TrimSuffix(name, ".json")
		if id == "" {
			continue
		}

		info := SessionInfo{ID: id}
		if fi, statErr := e.Info(); statErr == nil {
			info.UpdatedAt = fi.ModTime().Format(time.RFC3339)
		}

		if data, readErr := os.ReadFile(filepath.Join(s.dir, name)); readErr == nil {
			var h header
			if jsonErr := json.Unmarshal(data, &h); jsonErr == nil {
				if !h.CreatedAt.IsZero() {
					info.CreatedAt = h.CreatedAt.Format(time.RFC3339)
				}
				if title, ok := h.Metadata["title"].(string); ok {
					info.Title = title
				}
			}
		}

		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

func (s *LocalSessionStorage) DeleteSession(id string) error { //nolint:revive
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("session id is empty")
	}
	return os.Remove(filepath.Join(s.dir, id+".json"))
}

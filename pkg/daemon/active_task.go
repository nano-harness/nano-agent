package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// loadHistoryFromDisk loads event history from disk if store is empty
func (t *ActiveTask) loadHistoryFromDisk() error {
	t.loadMutex.Lock()
	defer t.loadMutex.Unlock()

	if t.Store == nil {
		t.Store = NewTaskEventStore(5000)
	}

	// If store already has events, skip loading
	if t.Store.LastSeq() > 0 {
		return nil
	}

	sessionDir := filepath.Join(getRuntimeSessionsDir(), t.SessionID)
	journalPath := filepath.Join(sessionDir, "journal.jsonl")

	f, err := os.Open(journalPath)
	if os.IsNotExist(err) {
		// Try to download from OSS
		var ossCfg *config.OSSConfig
		if globalCfg := config.Get(); globalCfg != nil {
			ossCfg = globalCfg.OSS
		}
		if ossCfg != nil && ossCfg.Enabled {
			client, ossErr := oss.New(ossCfg.NormalizedEndpoint(), ossCfg.AccessKeyID, ossCfg.AccessKeySecret)
			if ossErr == nil {
				bucket, ossErr := client.Bucket(ossCfg.DefaultBucket)
				if ossErr == nil {
					ossKey := fmt.Sprintf("sessions/%s/journal.jsonl", t.SessionID)
					// Ensure directory exists before downloading
					_ = os.MkdirAll(sessionDir, 0755)
					ossErr = bucket.GetObjectToFile(ossKey, journalPath)
					if ossErr == nil {
						logger.Infof("Downloaded journal.jsonl from OSS for session %s", t.SessionID)
						f, err = os.Open(journalPath)
					}
				}
			}
		}
	}

	if err != nil {
		if os.IsNotExist(err) {
			return nil // No history yet locally or on OSS
		}
		return err
	}
	defer func() { _ = f.Close() }()

	logger.Infof("Loading history from %s for session %s", journalPath, t.SessionID)

	scanner := bufio.NewScanner(f)
	// Increase buffer size for large lines (up to 10MB)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	count := 0
	for scanner.Scan() {
		var ev event.StreamEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err == nil {
			t.Store.Add(ev)
			count++
		}
	}

	logger.Infof("Loaded %d events for session %s", count, t.SessionID)
	return scanner.Err()
}

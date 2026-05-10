package slash

import (
	"time"

	"github.com/nano-harness/nano-agent/pkg/checkpoint"
	"github.com/nano-harness/nano-agent/pkg/config"
)

// NewDefaultCheckpointManager returns a CheckpointManager backed by
// pkg/checkpoint when checkpointing is enabled in config. A nil result means
// the caller should keep the friendly "checkpoint not enabled" behavior.
func NewDefaultCheckpointManager(cwd string) CheckpointManager {
	cfg := config.Get()
	if cfg == nil || cfg.Checkpoint == nil || !cfg.Checkpoint.Enabled {
		return nil
	}
	cp := cfg.Checkpoint
	opts := checkpoint.Options{
		WorkingDir: cwd,
		BackupRoot: cp.BackupRoot,
		MaxCount:   cp.MaxCount,
	}
	if cp.MaxSizeMB > 0 {
		opts.MaxSizeBytes = int64(cp.MaxSizeMB) << 20
	}
	if cp.RetentionDays > 0 {
		opts.RetentionAge = time.Duration(cp.RetentionDays) * 24 * time.Hour
	}
	mgr, err := checkpoint.NewFSManager(opts)
	if err != nil {
		return nil
	}
	return checkpointAdapter{mgr: mgr}
}

type checkpointAdapter struct {
	mgr checkpoint.Manager
}

func (a checkpointAdapter) Create(reason string) (string, error) {
	cp, err := a.mgr.Snapshot(reason, "slash")
	if err != nil {
		return "", err
	}
	return cp.ID, nil
}

func (a checkpointAdapter) List() ([]CheckpointInfo, error) {
	cps, err := a.mgr.List()
	if err != nil {
		return nil, err
	}
	out := make([]CheckpointInfo, 0, len(cps))
	for _, cp := range cps {
		out = append(out, CheckpointInfo{
			ID:         cp.ID,
			CreatedAt:  cp.CreatedAt.Format(time.RFC3339),
			Reason:     cp.Reason,
			FileCount:  0,
			TotalBytes: cp.SizeBytes,
		})
	}
	return out, nil
}

func (a checkpointAdapter) Restore(id string) error {
	return a.mgr.Restore(id)
}

package watcher

import (
	"context"
	"time"
)

// AoneSource is a placeholder source for Aone (Alibaba's project management
// platform) events such as new merge requests or CI failures.
//
// The actual polling implementation requires Aone CLI / API credentials and
// is platform-specific.  This stub satisfies the Source interface so that
// watcher rules with source="aone" can be created and persisted without
// causing a startup panic.  Override this with a concrete implementation by
// replacing the "aone" case in newSource().
type AoneSource struct {
	// eventType is the event class to watch: "new_mr", "ci_failure", etc.
	eventType string
}

// Poll returns no events in the stub implementation.
func (a *AoneSource) Poll(_ context.Context, _ string, _ time.Time) ([]WatchEvent, time.Time, error) {
	// TODO: implement real Aone API polling.
	// The implementation should:
	//   1. Call the Aone CLI / REST API to list recent MRs, CI runs, etc.
	//   2. Filter by the since checkpoint to return only new events.
	//   3. Map each result to a WatchEvent with Payload fields like
	//      MR_URL, MR_TITLE, REPO, AUTHOR, CI_JOB, etc.
	return nil, time.Now(), nil
}

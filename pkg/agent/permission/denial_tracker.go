package permission

import "sync"

// DenialTracker tracks repeated policy denials so headless/binary runs can
// terminate deterministically instead of looping on the same blocked tool call.
//
// Zero values (maxConsecutive==0 && maxTotal==0) disable the tracker.
type DenialTracker struct {
	maxConsecutive, maxTotal int
	consecutive, total       int
	sample                   []string // last 5 deny command texts
	mu                       sync.Mutex
}

func NewDenialTracker(maxConsecutive, maxTotal int) *DenialTracker {
	return &DenialTracker{
		maxConsecutive: maxConsecutive,
		maxTotal:       maxTotal,
	}
}

// RecordDeny increments the denial counters and stores cmd in the rolling sample.
// It returns true when the tracker has reached a configured limit.
func (t *DenialTracker) RecordDeny(cmd string) (lockedOut bool) {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.maxConsecutive == 0 && t.maxTotal == 0 {
		return false
	}
	t.total++
	t.consecutive++
	if cmd != "" {
		t.sample = append(t.sample, cmd)
		if len(t.sample) > 5 {
			t.sample = t.sample[len(t.sample)-5:]
		}
	}
	return t.lockedOutLocked()
}

// RecordAllow resets the consecutive counter (but not total).
func (t *DenialTracker) RecordAllow() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.maxConsecutive == 0 && t.maxTotal == 0 {
		return
	}
	t.consecutive = 0
}

// LockedOut reports whether the tracker has reached a configured limit.
func (t *DenialTracker) LockedOut() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.maxConsecutive == 0 && t.maxTotal == 0 {
		return false
	}
	return t.lockedOutLocked()
}

func (t *DenialTracker) lockedOutLocked() bool {
	if t.maxConsecutive > 0 && t.consecutive >= t.maxConsecutive {
		return true
	}
	if t.maxTotal > 0 && t.total >= t.maxTotal {
		return true
	}
	return false
}

// Sample returns a copy of the most recent denied command texts.
func (t *DenialTracker) Sample() []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.sample) == 0 {
		return nil
	}
	cp := make([]string, len(t.sample))
	copy(cp, t.sample)
	return cp
}

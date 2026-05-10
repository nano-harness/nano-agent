package agent

import (
	"sort"
	"sync"
	"time"
)

const maxSeqLagSamples = 1024

// SessionMetrics tracks lifecycle observability data for sessions.
type SessionMetrics struct {
	mu                  sync.RWMutex
	countByState        map[SessionState]int
	cleanupByReason     map[string]int64
	seqLagSamples       []int64
	totalLifetime       time.Duration
	sessionLifetimeSeen int64
}

// SessionMetricsSnapshot is a point-in-time metrics view.
type SessionMetricsSnapshot struct {
	CountByState            map[string]int   `json:"count_by_state"`
	CleanupByReason         map[string]int64 `json:"cleanup_reasons"`
	TotalLoaded             int              `json:"total_loaded"`
	TotalPersistedSeqLagP99 int64            `json:"total_persisted_seq_lag_p99"`
	AvgSessionLifetime      time.Duration    `json:"avg_session_lifetime"`
}

func NewSessionMetrics() *SessionMetrics {
	return &SessionMetrics{
		countByState:    make(map[SessionState]int),
		cleanupByReason: make(map[string]int64),
	}
}

func (m *SessionMetrics) RecordStateChange(from, to SessionState) {
	if m == nil || from == to {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if from != "" && m.countByState[from] > 0 {
		m.countByState[from]--
	}
	if to != "" {
		m.countByState[to]++
	}
}

func (m *SessionMetrics) RecordCleanup(reason string) {
	if m == nil {
		return
	}
	if reason == "" {
		reason = "unknown"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupByReason[reason]++
}

func (m *SessionMetrics) RecordSeqLag(lag int64) {
	if m == nil || lag < 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.seqLagSamples) >= maxSeqLagSamples {
		copy(m.seqLagSamples, m.seqLagSamples[1:])
		m.seqLagSamples[len(m.seqLagSamples)-1] = lag
		return
	}
	m.seqLagSamples = append(m.seqLagSamples, lag)
}

func (m *SessionMetrics) RecordSessionLifetime(d time.Duration) {
	if m == nil || d < 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalLifetime += d
	m.sessionLifetimeSeen++
}

func (m *SessionMetrics) Snapshot() SessionMetricsSnapshot {
	if m == nil {
		return SessionMetricsSnapshot{CountByState: map[string]int{}, CleanupByReason: map[string]int64{}}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts := make(map[string]int, len(m.countByState))
	total := 0
	for state, count := range m.countByState {
		counts[string(state)] = count
		total += count
	}
	cleanup := make(map[string]int64, len(m.cleanupByReason))
	for reason, count := range m.cleanupByReason {
		cleanup[reason] = count
	}
	samples := append([]int64(nil), m.seqLagSamples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	var p99 int64
	if len(samples) > 0 {
		// Nearest-rank p99 uses ceil(0.99*n)-1 for zero-based slices.
		// Integer ceil(a/b) is (a+b-1)/b, so ceil(99*n/100)-1 becomes (99*n+99)/100-1.
		idx := (len(samples)*99+99)/100 - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(samples) {
			idx = len(samples) - 1
		}
		p99 = samples[idx]
	}
	var avg time.Duration
	if m.sessionLifetimeSeen > 0 {
		avg = m.totalLifetime / time.Duration(m.sessionLifetimeSeen)
	}
	return SessionMetricsSnapshot{
		CountByState:            counts,
		CleanupByReason:         cleanup,
		TotalLoaded:             total,
		TotalPersistedSeqLagP99: p99,
		AvgSessionLifetime:      avg,
	}
}

package permission

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// Classifier is the abstract two-stage AI risk classifier consulted by
// ModeAuto. Implementations must be safe for concurrent use.
type Classifier interface {
	// Classify decides whether the given tool invocation should be blocked
	// (i.e. require explicit user confirmation). Returning ShouldBlock=false
	// means the call is auto-approved.
	Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResult, error)
	// Timeout returns the per-call timeout the manager should apply.
	Timeout() time.Duration
}

// ClassifyRequest captures the inputs for a classifier decision.
type ClassifyRequest struct {
	ToolName string
	Params   map[string]interface{}
	WorkDir  string
	PermMode PermissionMode
}

// ClassifyResult is the classifier's verdict.
type ClassifyResult struct {
	ShouldBlock bool   // true → require confirmation; false → auto-approve
	Reason      string // human-readable rationale
	Stage       string // "stage1" | "stage2" | "fail-closed"
	CachedHit   bool   // whether the result was served from cache
	Confidence  float64
}

// CacheKey returns a stable cache key for the request.
func (r ClassifyRequest) CacheKey() string {
	payload := struct {
		Tool   string                 `json:"tool"`
		Params map[string]interface{} `json:"params"`
		Mode   string                 `json:"mode"`
		Cwd    string                 `json:"cwd"`
	}{r.ToolName, r.Params, string(r.PermMode), r.WorkDir}
	b, _ := json.Marshal(payload)
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}

// FailClosedClassifier always returns ShouldBlock=true. It is a useful
// fallback when no LLM-backed classifier is configured but the operator still
// wants ModeAuto plumbing to behave conservatively (= identical to default).
type FailClosedClassifier struct {
	Reason   string
	Timeout_ time.Duration //nolint:revive
}

// Classify implements Classifier.
func (c *FailClosedClassifier) Classify(_ context.Context, _ ClassifyRequest) (*ClassifyResult, error) {
	reason := c.Reason
	if reason == "" {
		reason = "no classifier configured; failing closed"
	}
	return &ClassifyResult{ShouldBlock: true, Reason: reason, Stage: "fail-closed"}, nil
}

// Timeout implements Classifier.
func (c *FailClosedClassifier) Timeout() time.Duration {
	if c.Timeout_ <= 0 {
		return 5 * time.Second
	}
	return c.Timeout_
}

// CachingClassifier wraps a delegate Classifier with an in-memory TTL cache so
// repeat decisions for identical (tool, params) pairs are free.
type CachingClassifier struct {
	Delegate Classifier
	TTL      time.Duration
	MaxSize  int

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	result    *ClassifyResult
	expiresAt time.Time
}

// Classify checks the cache then delegates on miss.
func (c *CachingClassifier) Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResult, error) {
	if c == nil || c.Delegate == nil {
		return nil, nil
	}
	key := req.CacheKey()
	if cached := c.lookup(key); cached != nil {
		hit := *cached
		hit.CachedHit = true
		return &hit, nil
	}
	result, err := c.Delegate.Classify(ctx, req)
	if err != nil || result == nil {
		return result, err
	}
	c.store(key, result)
	return result, nil
}

// Timeout delegates.
func (c *CachingClassifier) Timeout() time.Duration {
	if c == nil || c.Delegate == nil {
		return 5 * time.Second
	}
	return c.Delegate.Timeout()
}

func (c *CachingClassifier) lookup(key string) *ClassifyResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil
	}
	return entry.result
}

func (c *CachingClassifier) store(key string, result *ClassifyResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cacheEntry)
	}
	if c.MaxSize > 0 && len(c.entries) >= c.MaxSize {
		// trivial eviction: drop one arbitrary key
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
	expires := time.Time{}
	if c.TTL > 0 {
		expires = time.Now().Add(c.TTL)
	}
	c.entries[key] = cacheEntry{result: result, expiresAt: expires}
}

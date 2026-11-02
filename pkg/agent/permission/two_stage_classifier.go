package permission

import (
	"context"
	"time"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// TwoStageClassifier implements a two-stage risk classification strategy:
//
//   - Stage 1 (Fast) makes a quick judgment.  If it approves the call
//     (ShouldBlock=false), the result is returned immediately — the deep model
//     is never consulted.  This covers ~80% of tool calls with minimal latency.
//
//   - Stage 2 (Deep) is invoked only when Stage 1 decides to block.  A deeper
//     reasoning model re-evaluates the call to reduce false-positive blocks.
//
// When both stages fail (error or timeout), the FailClosed flag controls the
// fallback:
//   - FailClosed=true  → ShouldBlock=true  ("audit mode" off, conservative)
//   - FailClosed=false → ShouldBlock=false (audit mode, permissive — only for
//     deployment observation; do not use in production long-term)
type TwoStageClassifier struct {
	Fast       Classifier // Stage 1: fast model
	Deep       Classifier // Stage 2: deep/reasoning model (may be nil)
	FailClosed bool       // fallback when both stages fail
}

// Classify implements Classifier.
func (c *TwoStageClassifier) Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResult, error) {
	// Stage 1 — fast classification.
	stage1Ctx, stage1Cancel := context.WithTimeout(ctx, c.Fast.Timeout())
	result1, err1 := c.Fast.Classify(stage1Ctx, req)
	stage1Cancel()

	if err1 == nil && result1 != nil {
		result1.Stage = "stage1"
		if !result1.ShouldBlock {
			// Fast model approved → return immediately, skip Stage 2.
			logger.Debugf("TwoStageClassifier: stage1 approved %s", req.ToolName)
			return result1, nil
		}
		// Fast model blocked → escalate to Stage 2.
		logger.Debugf("TwoStageClassifier: stage1 blocked %s, escalating to stage2", req.ToolName)
	} else {
		logger.Warnf("TwoStageClassifier: stage1 error for %s: %v; escalating to stage2", req.ToolName, err1)
	}

	// Stage 2 — deep classification.
	if c.Deep != nil {
		stage2Ctx, stage2Cancel := context.WithTimeout(ctx, c.Deep.Timeout())
		result2, err2 := c.Deep.Classify(stage2Ctx, req)
		stage2Cancel()
		if err2 == nil && result2 != nil {
			result2.Stage = "stage2"
			logger.Debugf("TwoStageClassifier: stage2 decision for %s: block=%v", req.ToolName, result2.ShouldBlock)
			return result2, nil
		}
		logger.Warnf("TwoStageClassifier: stage2 error for %s: %v; falling back", req.ToolName, err2)
	}

	// Both stages failed — apply fail-open or fail-closed fallback.
	if c.FailClosed {
		logger.Warnf("TwoStageClassifier: both stages failed for %s; failing closed", req.ToolName)
		return &ClassifyResult{
			ShouldBlock: true,
			Reason:      "classifier unavailable; failing closed",
			Stage:       "fail-closed",
		}, nil
	}
	logger.Warnf("TwoStageClassifier: both stages failed for %s; failing open (audit mode)", req.ToolName)
	return &ClassifyResult{
		ShouldBlock: false,
		Reason:      "classifier unavailable; failing open",
		Stage:       "fail-open",
	}, nil
}

// Timeout implements Classifier. Returns the sum of Stage 1 and Stage 2
// timeouts so callers can allocate a suitably generous outer deadline.
func (c *TwoStageClassifier) Timeout() time.Duration {
	t := c.Fast.Timeout()
	if c.Deep != nil {
		t += c.Deep.Timeout()
	}
	return t
}

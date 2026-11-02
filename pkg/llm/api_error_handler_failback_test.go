package llm

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestAnalyzeErrorFailbackMatrix(t *testing.T) {
	handler := NewAPIErrorHandler(nil)
	tests := []struct {
		name           string
		err            error
		httpStatus     int
		category       APIErrorCategory
		retryable      bool
		shouldFailback bool
		recordCB       bool
	}{
		{
			name:           "RateLimit",
			err:            errors.New("rate limit exceeded"),
			httpStatus:     http.StatusTooManyRequests,
			category:       APIErrorCategoryRateLimit,
			retryable:      true,
			shouldFailback: true,
			recordCB:       true,
		},
		{
			name:           "Server",
			err:            errors.New("500 Internal Server Error"),
			httpStatus:     http.StatusInternalServerError,
			category:       APIErrorCategoryServer,
			retryable:      true,
			shouldFailback: true,
			recordCB:       true,
		},
		{
			name:           "Auth",
			err:            errors.New("401 Unauthorized"),
			httpStatus:     http.StatusUnauthorized,
			category:       APIErrorCategoryAuthentication,
			retryable:      false,
			shouldFailback: false,
			recordCB:       false,
		},
		{
			name:           "Quota",
			err:            errors.New("402 quota exhausted"),
			httpStatus:     http.StatusPaymentRequired,
			category:       APIErrorCategoryQuota,
			retryable:      false,
			shouldFailback: false,
			recordCB:       false,
		},
		{
			name:           "ContextOverflow",
			err:            errors.New("context_length_exceeded: maximum context length exceeded"),
			httpStatus:     http.StatusBadRequest,
			category:       APIErrorCategoryContextOverflow,
			retryable:      false,
			shouldFailback: false,
			recordCB:       false,
		},
		{
			name:           "Aborted",
			err:            context.Canceled,
			httpStatus:     0,
			category:       APIErrorCategoryAborted,
			retryable:      false,
			shouldFailback: false,
			recordCB:       false,
		},
		{
			name:           "OutputFormat",
			err:            errors.New("tool_call_validation failed: invalid_tool_call"),
			httpStatus:     http.StatusBadRequest,
			category:       APIErrorCategoryOutputFormat,
			retryable:      false,
			shouldFailback: false,
			recordCB:       false,
		},
		{
			name:           "Network",
			err:            errors.New("connection reset by peer"),
			httpStatus:     0,
			category:       APIErrorCategoryNetwork,
			retryable:      true,
			shouldFailback: true,
			recordCB:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := handler.AnalyzeError(tt.err, tt.httpStatus)
			if info.Category != tt.category {
				t.Fatalf("Category = %s, want %s", info.Category, tt.category)
			}
			if info.Retryable != tt.retryable {
				t.Fatalf("Retryable = %v, want %v", info.Retryable, tt.retryable)
			}
			if info.ShouldFailback != tt.shouldFailback {
				t.Fatalf("ShouldFailback = %v, want %v", info.ShouldFailback, tt.shouldFailback)
			}
			if got := ShouldRecordCBFailure(tt.err); got != tt.recordCB {
				t.Fatalf("ShouldRecordCBFailure = %v, want %v", got, tt.recordCB)
			}
		})
	}
}

func TestAnalyzeErrorContextOverflowBoundaries(t *testing.T) {
	handler := NewAPIErrorHandler(nil)

	canceled := handler.AnalyzeError(context.Canceled, http.StatusBadRequest)
	if canceled.Category != APIErrorCategoryAborted {
		t.Fatalf("context.Canceled category = %s, want %s", canceled.Category, APIErrorCategoryAborted)
	}

	overflow := handler.AnalyzeError(errors.New("context_length_exceeded: prompt is too long"), http.StatusBadRequest)
	if overflow.Category != APIErrorCategoryContextOverflow {
		t.Fatalf("HTTP 400 context_length_exceeded category = %s, want %s", overflow.Category, APIErrorCategoryContextOverflow)
	}
	if overflow.Retryable || overflow.ShouldFailback {
		t.Fatalf("context overflow should not retry or failback: %+v", overflow)
	}
}

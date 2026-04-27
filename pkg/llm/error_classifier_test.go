package llm

import (
	"errors"
	"net/http"
	"testing"
)

func TestAPIErrorHandlerImplementsErrorClassifier(t *testing.T) {
	var classifier ErrorClassifier = NewAPIErrorHandler(nil)

	info := classifier.Classify(errors.New("rate limit exceeded"), http.StatusTooManyRequests)
	if info == nil {
		t.Fatal("expected classification")
	}
	if info.Category != APIErrorCategoryRateLimit {
		t.Fatalf("expected rate limit category, got %q", info.Category)
	}
	if !info.Retryable {
		t.Fatal("expected rate limit errors to be retryable")
	}
}

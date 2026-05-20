package llm

import "net/http"

// isAnthropicOverloadedStatus returns true for HTTP 529 (Anthropic-specific overloaded status).
func isAnthropicOverloadedStatus(httpStatus int) bool {
	return httpStatus == 529
}

// classifyAnthropicHTTPStatus maps Anthropic-specific HTTP status codes to
// APIErrorInfo. Returns nil when the status is not a special Anthropic code so
// the general handler can take over.
func classifyAnthropicHTTPStatus(httpStatus int, msg string) *APIErrorInfo {
	switch {
	case httpStatus == 529:
		// Anthropic 529 Overloaded — retryable, should failback
		return &APIErrorInfo{
			Category:       APIErrorCategoryServer,
			Severity:       APIErrorSeverityMedium,
			Retryable:      true,
			ShouldFailback: true,
			Message:        msg,
			HTTPStatus:     httpStatus,
			Suggestion:     "Anthropic API overloaded, please retry later",
		}
	case httpStatus == http.StatusTooManyRequests:
		return &APIErrorInfo{
			Category:       APIErrorCategoryRateLimit,
			Severity:       APIErrorSeverityMedium,
			Retryable:      true,
			ShouldFailback: true,
			Message:        msg,
			HTTPStatus:     httpStatus,
			Suggestion:     "Anthropic rate limit hit, please slow down requests",
		}
	case httpStatus == http.StatusUnauthorized:
		return &APIErrorInfo{
			Category:       APIErrorCategoryAuthentication,
			Severity:       APIErrorSeverityHigh,
			Retryable:      false,
			ShouldFailback: false,
			Message:        msg,
			HTTPStatus:     httpStatus,
			Suggestion:     "Invalid Anthropic API key, please check ANTHROPIC_API_KEY",
		}
	case httpStatus == http.StatusBadRequest:
		return &APIErrorInfo{
			Category:       APIErrorCategoryClient,
			Severity:       APIErrorSeverityHigh,
			Retryable:      false,
			ShouldFailback: false,
			Message:        msg,
			HTTPStatus:     httpStatus,
			Suggestion:     "Bad request to Anthropic API, check request parameters",
		}
	case httpStatus >= 500 && httpStatus < 600:
		return &APIErrorInfo{
			Category:       APIErrorCategoryServer,
			Severity:       APIErrorSeverityMedium,
			Retryable:      true,
			ShouldFailback: true,
			Message:        msg,
			HTTPStatus:     httpStatus,
			Suggestion:     "Anthropic server error, please retry later",
		}
	}
	return nil
}

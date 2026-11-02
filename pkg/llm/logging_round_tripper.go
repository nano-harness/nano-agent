package llm

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/logger"
)

// loggingRoundTripper wraps an http.RoundTripper to log requests and responses
type loggingRoundTripper struct {
	wrapped http.RoundTripper
}

// Log truncation limits for HTTP debug output.
const (
	requestBodyDisplayLimit  = 2000                         // max chars shown from request body
	requestBodyReadLimit     = requestBodyDisplayLimit + 1  // +1 detects truncation
	responseBodyDisplayLimit = 1000                         // max chars shown from error response body
	responseBodyReadLimit    = responseBodyDisplayLimit + 1 // +1 detects truncation
)

// RoundTrip implements http.RoundTripper interface with logging
func (l *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Log request
	logger.Debugf("[HTTP] → %s %s", req.Method, req.URL.String())

	// Log headers (with Authorization redaction)
	for key, values := range req.Header {
		for _, value := range values {
			if strings.EqualFold(key, "Authorization") {
				// Redact authorization header: log only the auth scheme, never any token characters
				fields := strings.Fields(value)
				if len(fields) > 0 {
					logger.Debugf("[HTTP]   %s: %s [REDACTED]", key, fields[0])
				} else {
					logger.Debugf("[HTTP]   %s: [REDACTED]", key)
				}
			} else {
				logger.Debugf("[HTTP]   %s: %s", key, value)
			}
		}
	}

	// Log request body without consuming the live request stream
	if req.Body != nil {
		if req.GetBody != nil {
			bodyReader, err := req.GetBody()
			if err != nil {
				logger.Debugf("[HTTP] Request body: unable to clone body for logging: %v", err)
			} else {
				defer func() { _ = bodyReader.Close() }()
				bodyBytes, err := io.ReadAll(io.LimitReader(bodyReader, requestBodyReadLimit))
				if err != nil {
					logger.Debugf("[HTTP] Request body: unable to read cloned body for logging: %v", err)
				} else {
					bodyStr := string(bodyBytes)
					if len(bodyStr) >= requestBodyReadLimit {
						bodyStr = bodyStr[:requestBodyDisplayLimit] + "..."
					}
					logger.Debugf("[HTTP] Request body (%d bytes): %s", len(bodyBytes), bodyStr)
				}
			}
		} else {
			logger.Debugf("[HTTP] Request body present but cannot be logged safely: GetBody is nil")
		}
	}

	// Execute the actual request
	resp, err := l.wrapped.RoundTrip(req)
	if err != nil {
		logger.Debugf("[HTTP] ← Error: %v", err)
		return resp, err
	}

	// Log response
	logger.Debugf("[HTTP] ← %s", resp.Status)

	// Log error response bodies (4xx/5xx)
	if resp.StatusCode >= 400 {
		if resp.Body != nil {
			bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, responseBodyReadLimit))
			// Always restore the body: re-combine what we read with remaining stream
			resp.Body = io.NopCloser(io.MultiReader(bytes.NewBuffer(bodyBytes), resp.Body))
			if readErr != nil {
				logger.Debugf("[HTTP] Error response body: failed to read: %v", readErr)
			} else {
				bodyStr := string(bodyBytes)
				if len(bodyStr) >= responseBodyReadLimit {
					bodyStr = bodyStr[:responseBodyDisplayLimit] + "..."
				}
				logger.Debugf("[HTTP] Error response body: %s", bodyStr)
			}
		}
	}

	return resp, nil
}

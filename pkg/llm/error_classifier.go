package llm

// ErrorClassifier classifies provider/API errors and returns retry guidance.
type ErrorClassifier interface {
	Classify(err error, httpStatus int) *APIErrorInfo
	ShouldFailback(err error, httpStatus int) bool
}

// Classify implements ErrorClassifier for APIErrorHandler.
func (aeh *APIErrorHandler) Classify(err error, httpStatus int) *APIErrorInfo {
	return aeh.AnalyzeError(err, httpStatus)
}

// ShouldFailback reports whether an error should trigger fallback/circuit-breaker handling.
func (aeh *APIErrorHandler) ShouldFailback(err error, httpStatus int) bool {
	info := aeh.AnalyzeError(err, httpStatus)
	return info != nil && info.ShouldFailback
}

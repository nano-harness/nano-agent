package llm

// ErrorClassifier classifies provider/API errors and returns retry guidance.
type ErrorClassifier interface {
	Classify(err error, httpStatus int) *APIErrorInfo
}

// Classify implements ErrorClassifier for APIErrorHandler.
func (aeh *APIErrorHandler) Classify(err error, httpStatus int) *APIErrorInfo {
	return aeh.AnalyzeError(err, httpStatus)
}

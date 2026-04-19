package agent

import (
	"fmt"
)

// ErrorCode represents standardized error codes
type ErrorCode int

const (
	// ErrUnknown represents an unknown error
	ErrUnknown ErrorCode = iota
	// ErrInvalidInput represents an invalid input error
	ErrInvalidInput
	// ErrFileNotFound represents a file not found error
	ErrFileNotFound
	// ErrFilePermission represents a file permission error
	ErrFilePermission
	// ErrFileTooLarge represents a file too large error
	ErrFileTooLarge
	// ErrSyntaxValidation represents a syntax validation error
	ErrSyntaxValidation
	// ErrAPIRequest represents an API request error
	ErrAPIRequest
	// ErrTimeout represents a timeout error
	ErrTimeout
	// ErrCancelled represents a cancelled error
	ErrCancelled
)

// String returns string representation of ErrorCode
func (ec ErrorCode) String() string {
	switch ec {
	case ErrInvalidInput:
		return "INVALID_INPUT"
	case ErrFileNotFound:
		return "FILE_NOT_FOUND"
	case ErrFilePermission:
		return "FILE_PERMISSION"
	case ErrFileTooLarge:
		return "FILE_TOO_LARGE"
	case ErrSyntaxValidation:
		return "SYNTAX_VALIDATION"
	case ErrAPIRequest:
		return "API_REQUEST"
	case ErrTimeout:
		return "TIMEOUT"
	case ErrCancelled:
		return "CANCELLED"
	default:
		return "UNKNOWN"
	}
}

// Error represents a standardized agent error with context
type Error struct {
	Operation string
	Code      ErrorCode
	Message   string
	Cause     error
	Context   map[string]interface{}
}

// Error implements the error interface
func (ae *Error) Error() string {
	if ae.Cause != nil {
		return fmt.Sprintf("%s failed (%s): %s (caused by: %v)",
			ae.Operation, ae.Code.String(), ae.Message, ae.Cause)
	}
	return fmt.Sprintf("%s failed (%s): %s", ae.Operation, ae.Code.String(), ae.Message)
}

// Unwrap returns the underlying error
func (ae *Error) Unwrap() error {
	return ae.Cause
}

// NewError creates a new standardized error
func NewError(operation string, code ErrorCode, message string, cause error) *Error {
	return &Error{
		Operation: operation,
		Code:      code,
		Message:   message,
		Cause:     cause,
		Context:   make(map[string]interface{}),
	}
}

// WrapError wraps an existing error with agent context
func WrapError(operation string, err error) *Error {
	if err == nil {
		return nil
	}

	// If it's already an Error, just update the operation
	if agentErr, ok := err.(*Error); ok {
		agentErr.Operation = operation + " -> " + agentErr.Operation
		return agentErr
	}

	return NewError(operation, ErrUnknown, err.Error(), err)
}

// WithContext adds context to an error
func (ae *Error) WithContext(key string, value interface{}) *Error {
	ae.Context[key] = value
	return ae
}

// GetContext gets a context value
func (ae *Error) GetContext(key string) (interface{}, bool) {
	value, exists := ae.Context[key]
	return value, exists
}

package errors

import (
	"fmt"
	"net/http"
	"time"
)

// ErrorType represents different categories of errors
type ErrorType string

const (
	ErrorTypeClient   ErrorType = "client_error"
	ErrorTypeUpstream ErrorType = "upstream_error"
	ErrorTypeInstance ErrorType = "instance_error"
	ErrorTypeInternal ErrorType = "internal_error"
)

// ProxyError represents a standardized error in the proxy system
type ProxyError struct {
	Type       ErrorType              `json:"type"`
	Message    string                 `json:"message"`
	StatusCode int                    `json:"status_code"`
	Details    map[string]interface{} `json:"details,omitempty"`
	Timestamp  int64                  `json:"timestamp"`
}

// Error implements the error interface
func (e *ProxyError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// NewClientError creates a new client error
func NewClientError(message string, statusCode int, details map[string]interface{}) *ProxyError {
	return &ProxyError{
		Type:       ErrorTypeClient,
		Message:    message,
		StatusCode: statusCode,
		Details:    details,
		Timestamp:  time.Now().Unix(),
	}
}

// NewUpstreamError creates a new upstream error
func NewUpstreamError(message string, statusCode int, details map[string]interface{}) *ProxyError {
	return &ProxyError{
		Type:       ErrorTypeUpstream,
		Message:    message,
		StatusCode: statusCode,
		Details:    details,
		Timestamp:  time.Now().Unix(),
	}
}

// NewInstanceError creates a new instance error
func NewInstanceError(message string, details map[string]interface{}) *ProxyError {
	return &ProxyError{
		Type:       ErrorTypeInstance,
		Message:    message,
		StatusCode: http.StatusServiceUnavailable,
		Details:    details,
		Timestamp:  time.Now().Unix(),
	}
}

// NewInternalError creates a new internal error
func NewInternalError(message string, details map[string]interface{}) *ProxyError {
	return &ProxyError{
		Type:       ErrorTypeInternal,
		Message:    message,
		StatusCode: http.StatusInternalServerError,
		Details:    details,
		Timestamp:  time.Now().Unix(),
	}
}

// ClassifyError determines the error type based on HTTP status code and source
func ClassifyError(statusCode int, source string) ErrorType {
	switch {
	case statusCode >= 400 && statusCode < 500:
		if source == "upstream" {
			return ErrorTypeUpstream
		}
		return ErrorTypeClient
	case statusCode >= 500:
		if source == "upstream" {
			return ErrorTypeUpstream
		}
		return ErrorTypeInstance
	default:
		return ErrorTypeInternal
	}
}

// IsRetryable determines if an error should trigger a retry
func (e *ProxyError) IsRetryable() bool {
	switch e.Type {
	case ErrorTypeUpstream:
		// Retry on 5xx errors and 429 (rate limit)
		return e.StatusCode >= 500 || e.StatusCode == 429
	case ErrorTypeInstance:
		// Retry on instance errors
		return true
	case ErrorTypeInternal:
		// Retry on some internal errors
		return e.StatusCode >= 500
	default:
		// Don't retry client errors
		return false
	}
}

// GetRetryAfter extracts retry-after value from error details
func (e *ProxyError) GetRetryAfter() int {
	if e.Details == nil {
		return 0
	}
	
	if retryAfter, ok := e.Details["retry_after"].(int); ok {
		return retryAfter
	}
	
	if retryAfter, ok := e.Details["retry_after"].(float64); ok {
		return int(retryAfter)
	}
	
	return 0
}
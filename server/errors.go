package server

import (
	"fmt"

	"github.com/voocel/mcp-sdk-go/protocol"
)

type ErrorCode string

const (
	// Client Error
	ErrInvalidParams  ErrorCode = "invalid_params"    // Invalid parameter
	ErrNotFound       ErrorCode = "not_found"         // Resource not found
	ErrUnauthorized   ErrorCode = "unauthorized"      // Unauthorized
	ErrForbidden      ErrorCode = "forbidden"         // Access Denied
	ErrConflict       ErrorCode = "conflict"          // Conflict
	ErrTooManyRequest ErrorCode = "too_many_requests" // Too many requests

	// Server error
	ErrInternal       ErrorCode = "internal_error"   // Internal Error
	ErrNotImplemented ErrorCode = "not_implemented"  // Unrealized
	ErrUnavailable    ErrorCode = "unavailable"      // Service Unavailable
	ErrTimeout        ErrorCode = "timeout"          // Timeout
	ErrDependency     ErrorCode = "dependency_error" // Dependency Service Error
)

type ToolError struct {
	Code    ErrorCode
	Message string
	Details map[string]interface{}
	Cause   error
}

// Error Implement the error interface
func (e *ToolError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap Implement errors.Unwrap
func (e *ToolError) Unwrap() error {
	return e.Cause
}

func (e *ToolError) ToResult() *protocol.CallToolResult {
	errorText := e.Error()
	if len(e.Details) > 0 {
		errorText += "\nDetails:"
		for k, v := range e.Details {
			errorText += fmt.Sprintf("\n  %s: %v", k, v)
		}
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: protocol.ContentTypeText,
				Text: errorText,
			},
		},
		IsError: true,
	}
}

type ErrorOption func(*ToolError)

func WithDetail(key string, value interface{}) ErrorOption {
	return func(e *ToolError) {
		if e.Details == nil {
			e.Details = make(map[string]interface{})
		}
		e.Details[key] = value
	}
}

func WithCause(cause error) ErrorOption {
	return func(e *ToolError) {
		e.Cause = cause
	}
}

func NewToolError(code ErrorCode, message string, opts ...ErrorOption) *ToolError {
	err := &ToolError{
		Code:    code,
		Message: message,
	}

	for _, opt := range opts {
		opt(err)
	}

	return err
}

func InvalidParamsError(message string, opts ...ErrorOption) *ToolError {
	return NewToolError(ErrInvalidParams, message, opts...)
}

func NotFoundError(resource string, opts ...ErrorOption) *ToolError {
	return NewToolError(ErrNotFound, fmt.Sprintf("%s not found", resource), opts...)
}

func UnauthorizedError(message string, opts ...ErrorOption) *ToolError {
	return NewToolError(ErrUnauthorized, message, opts...)
}

func ForbiddenError(message string, opts ...ErrorOption) *ToolError {
	return NewToolError(ErrForbidden, message, opts...)
}

func ConflictError(message string, opts ...ErrorOption) *ToolError {
	return NewToolError(ErrConflict, message, opts...)
}

func InternalError(message string, opts ...ErrorOption) *ToolError {
	return NewToolError(ErrInternal, message, opts...)
}

func NotImplementedError(message string, opts ...ErrorOption) *ToolError {
	return NewToolError(ErrNotImplemented, message, opts...)
}

func UnavailableError(message string, opts ...ErrorOption) *ToolError {
	return NewToolError(ErrUnavailable, message, opts...)
}

func TimeoutError(message string, opts ...ErrorOption) *ToolError {
	return NewToolError(ErrTimeout, message, opts...)
}

func DependencyError(service string, err error, opts ...ErrorOption) *ToolError {
	opts = append(opts, WithCause(err), WithDetail("service", service))
	return NewToolError(ErrDependency, fmt.Sprintf("dependency service %s failed", service), opts...)
}

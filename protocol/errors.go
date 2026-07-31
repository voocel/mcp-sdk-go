package protocol

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// JSON-RPC standard error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602 // also: unknown tool/prompt, resource not found, bad cursor
	CodeInternal       = -32603
)

// MCP-reserved error codes (-32020..-32099). No constants exist in
// -32000..-32019: the spec forbids allocating new codes there.
const (
	CodeHeaderMismatch             = -32020
	CodeMissingClientCapability    = -32021
	CodeUnsupportedProtocolVersion = -32022
)

// Error is the single error type of this SDK. It round-trips code, message and
// data in both directions: a handler returning *Error has it written verbatim
// to the wire, and a client receiving an error response surfaces it unwrapped
// via errors.As.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`

	// httpStatus is a transport hint, never serialized. Zero means "use the
	// default mapping for Code".
	httpStatus int
}

func (e *Error) Error() string {
	return fmt.Sprintf("mcp %d: %s", e.Code, e.Message)
}

func Errorf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WithData attaches a JSON-marshaled data payload. Marshal failures are
// silently dropped (data is auxiliary by design).
func (e *Error) WithData(v any) *Error {
	if raw, err := json.Marshal(v); err == nil {
		e.Data = raw
	}
	return e
}

func (e *Error) withStatus(status int) *Error {
	e.httpStatus = status
	return e
}

// HTTPStatus returns the HTTP status an HTTP transport must use when carrying
// this error. Errors produced by request validation override the default
// per-code mapping.
func (e *Error) HTTPStatus() int {
	if e.httpStatus != 0 {
		return e.httpStatus
	}
	switch e.Code {
	case CodeParseError, CodeInvalidRequest, CodeHeaderMismatch,
		CodeMissingClientCapability, CodeUnsupportedProtocolVersion:
		return http.StatusBadRequest
	case CodeMethodNotFound:
		return http.StatusNotFound
	}
	return http.StatusOK
}

// UnsupportedVersionData is the data payload of an UnsupportedProtocolVersion
// error.
type UnsupportedVersionData struct {
	Supported []string `json:"supported"`
	Requested string   `json:"requested"`
}

func UnsupportedVersionError(requested string, supported []string) *Error {
	return Errorf(CodeUnsupportedProtocolVersion, "Unsupported protocol version").
		WithData(UnsupportedVersionData{Supported: supported, Requested: requested}).
		withStatus(http.StatusBadRequest)
}

// UnsupportedVersion decodes the data payload if this is an
// UnsupportedProtocolVersion error.
func (e *Error) UnsupportedVersion() (*UnsupportedVersionData, bool) {
	if e.Code != CodeUnsupportedProtocolVersion || len(e.Data) == 0 {
		return nil, false
	}
	var d UnsupportedVersionData
	if err := json.Unmarshal(e.Data, &d); err != nil {
		return nil, false
	}
	return &d, true
}

// MissingCapabilityData is the data payload of a MissingRequiredClientCapability
// error.
type MissingCapabilityData struct {
	RequiredCapabilities ClientCapabilities `json:"requiredCapabilities"`
}

func MissingCapabilityError(required ClientCapabilities) *Error {
	return Errorf(CodeMissingClientCapability, "Missing required client capability").
		WithData(MissingCapabilityData{RequiredCapabilities: required}).
		withStatus(http.StatusBadRequest)
}

// MissingCapabilities decodes the data payload if this is a
// MissingRequiredClientCapability error.
func (e *Error) MissingCapabilities() (*ClientCapabilities, bool) {
	if e.Code != CodeMissingClientCapability || len(e.Data) == 0 {
		return nil, false
	}
	var d MissingCapabilityData
	if err := json.Unmarshal(e.Data, &d); err != nil {
		return nil, false
	}
	return &d.RequiredCapabilities, true
}

// ResourceNotFoundError reports a missing resource. Per 2026-07-28 this is
// -32602 (Invalid Params) with the URI in data; -32002 is retired.
func ResourceNotFoundError(uri string) *Error {
	return Errorf(CodeInvalidParams, "Resource not found").
		WithData(map[string]string{"uri": uri})
}

func MethodNotFoundError(method string) *Error {
	return Errorf(CodeMethodNotFound, "Method not found: %s", method)
}

func HeaderMismatchError(detail string) *Error {
	return Errorf(CodeHeaderMismatch, "Header mismatch: %s", detail).
		withStatus(http.StatusBadRequest)
}

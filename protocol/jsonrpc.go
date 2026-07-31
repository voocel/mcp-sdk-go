// Package protocol defines the wire types of the Model Context Protocol,
// revision 2026-07-28. It has no dependencies outside the standard library.
package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

const JSONRPCVersion = "2.0"

// RequestID is a JSON-RPC request identifier: a string or an integer.
// The zero value means "absent" (notifications, error responses without id).
// It is comparable and usable as a map key.
type RequestID struct{ v any }

func StringID(s string) RequestID { return RequestID{s} }
func IntID(i int64) RequestID     { return RequestID{i} }

func (id RequestID) IsZero() bool { return id.v == nil }

func (id RequestID) String() string {
	switch v := id.v.(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	}
	return ""
}

func (id RequestID) MarshalJSON() ([]byte, error) {
	switch v := id.v.(type) {
	case string:
		return json.Marshal(v)
	case int64:
		return strconv.AppendInt(nil, v, 10), nil
	}
	return nil, fmt.Errorf("protocol: cannot marshal absent request id")
}

func (id *RequestID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return fmt.Errorf("protocol: empty request id")
	}
	// JSON-RPC error responses may carry "id": null when the faulty request's
	// id could not be determined; it decodes to the zero (absent) RequestID.
	// A request bearing a null id classifies as a notification, so null never
	// becomes a usable request id.
	if bytes.Equal(b, []byte("null")) {
		id.v = nil
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		id.v = s
		return nil
	}
	i, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("protocol: request id must be a string or an integer, got %s", b)
	}
	id.v = i
	return nil
}

// ProgressToken shares the identifier shape of RequestID: string or integer.
type ProgressToken = RequestID

// Message is the JSON-RPC 2.0 envelope shared by requests, notifications,
// responses and error responses.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      RequestID       `json:"id,omitzero"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Note on ids: 2026-07-28 forbids null ids outright ("Unlike base JSON-RPC,
// the ID MUST NOT be null"); an error response whose request id could not be
// read omits the id field entirely (id?: string | number), which omitzero
// already does. Decoding stays lenient: an incoming id:null is accepted as a
// zero ID for interoperability with sloppy peers.

// Kind classifies a Message. It is the single source of truth for message
// classification; callers must not re-derive it from field presence.
type Kind int

const (
	KindInvalid Kind = iota
	KindRequest
	KindNotification
	KindResponse
	KindError
)

func (m *Message) Kind() Kind {
	if m.JSONRPC != JSONRPCVersion {
		return KindInvalid
	}
	switch {
	case m.Error != nil && m.Method == "" && m.Result == nil:
		return KindError
	case m.Method != "" && m.Error == nil && m.Result == nil:
		if m.ID.IsZero() {
			return KindNotification
		}
		return KindRequest
	case m.Method == "" && m.Result != nil && m.Error == nil && !m.ID.IsZero():
		return KindResponse
	}
	return KindInvalid
}

func NewRequest(id RequestID, method string, params any) (*Message, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	return &Message{JSONRPC: JSONRPCVersion, ID: id, Method: method, Params: raw}, nil
}

func NewNotification(method string, params any) (*Message, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	return &Message{JSONRPC: JSONRPCVersion, Method: method, Params: raw}, nil
}

func NewResponse(id RequestID, r Result) (*Message, error) {
	raw, err := MarshalResult(r)
	if err != nil {
		return nil, err
	}
	return &Message{JSONRPC: JSONRPCVersion, ID: id, Result: raw}, nil
}

func NewErrorResponse(id RequestID, e *Error) *Message {
	return &Message{JSONRPC: JSONRPCVersion, ID: id, Error: e}
}

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return json.Marshal(params)
}

// Result is implemented by every result body. ResultType returns the wire
// discriminator: "complete", "input_required", or an extension-defined value
// (e.g. "task"). The field itself is injected by MarshalResult; result structs
// must not declare their own resultType field.
type Result interface {
	ResultType() string
}

// MarshalResult marshals r with its resultType discriminator spliced in as the
// first key of the JSON object. This is the only code path that writes
// resultType, so it can never be forgotten or inconsistent.
func MarshalResult(r Result) ([]byte, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return insertKey(body, "resultType", r.ResultType())
}

// PeekResultType reads the resultType discriminator from a raw result body.
// An absent resultType maps to "complete" — the spec's backward-compat rule
// for results from servers on earlier protocol revisions. Servers
// implementing 2026-07-28 MUST include the field, and the client rejects
// responses without it; this helper stays lenient for raw consumers.
func PeekResultType(raw json.RawMessage) (string, error) {
	var probe struct {
		ResultType *string `json:"resultType"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", err
	}
	if probe.ResultType == nil {
		return ResultTypeComplete, nil
	}
	return *probe.ResultType, nil
}

const (
	ResultTypeComplete      = "complete"
	ResultTypeInputRequired = "input_required"
)

// insertKey prepends `"key":"val"` as the first member of a marshaled JSON
// object without re-decoding it.
func insertKey(obj []byte, key, val string) ([]byte, error) {
	obj = bytes.TrimSpace(obj)
	if len(obj) < 2 || obj[0] != '{' {
		return nil, fmt.Errorf("protocol: cannot insert %q into non-object JSON", key)
	}
	quoted, err := json.Marshal(val)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Grow(len(obj) + len(key) + len(quoted) + 4)
	buf.WriteByte('{')
	buf.WriteByte('"')
	buf.WriteString(key)
	buf.WriteString(`":`)
	buf.Write(quoted)
	if !bytes.Equal(obj, []byte("{}")) {
		buf.WriteByte(',')
	}
	buf.Write(obj[1:])
	return buf.Bytes(), nil
}

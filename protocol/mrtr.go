package protocol

import (
	"encoding/json"
	"fmt"
)

// Multi Round-Trip Requests (MRTR) replace all server-initiated requests.
// A server answers tools/call, prompts/get or resources/read with an
// InputRequired interim result; the client fulfills the input requests and
// retries the original request (with a new ID) carrying inputResponses and
// the exact requestState.

// InputRequest is a bare {method, params} pair — no JSON-RPC envelope, no ID.
// Elicitation is first-class; other methods (deprecated roots/sampling, future
// extensions) round-trip untouched.
type InputRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type InputRequests map[string]InputRequest

// InputResponses maps request keys to bare result bodies. Values stay raw on
// the wire because responses carry no discriminator; the reader always knows
// what it asked for and decodes by expectation.
type InputResponses map[string]json.RawMessage

// Set marshals v as the response for key.
func (r InputResponses) Set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r[key] = raw
	return nil
}

// Elicit decodes the response for key as an ElicitResult.
func (r InputResponses) Elicit(key string) (*ElicitResult, error) {
	raw, ok := r[key]
	if !ok {
		return nil, fmt.Errorf("protocol: no input response for key %q", key)
	}
	var res ElicitResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// InputRequired is the "input_required" interim result shared by tools/call,
// prompts/get and resources/read. At least one of Requests or State must be
// set; the server validates this before sending.
type InputRequired struct {
	WithMeta
	Requests InputRequests `json:"inputRequests,omitempty"`
	State    string        `json:"requestState,omitempty"`
}

func (*InputRequired) ResultType() string { return ResultTypeInputRequired }

func (*InputRequired) toolResponse()     {}
func (*InputRequired) promptResponse()   {}
func (*InputRequired) resourceResponse() {}

// RequireInput builds an interim MRTR result. state may be empty when reqs is
// non-empty and vice versa.
func RequireInput(reqs InputRequests, state string) *InputRequired {
	return &InputRequired{Requests: reqs, State: state}
}

// Validate enforces the spec rule that an InputRequired carries at least one
// of inputRequests or requestState.
func (r *InputRequired) Validate() error {
	if len(r.Requests) == 0 && r.State == "" {
		return fmt.Errorf("protocol: InputRequired must carry at least one of inputRequests or requestState")
	}
	return nil
}

// ToolResponse is the closed sum returned by tool handlers:
// *CallToolResult | *InputRequired.
type ToolResponse interface {
	Result
	toolResponse()
}

// PromptResponse is the closed sum returned by prompt handlers:
// *GetPromptResult | *InputRequired.
type PromptResponse interface {
	Result
	promptResponse()
}

// ResourceResponse is the closed sum returned by resource handlers:
// *ReadResourceResult | *InputRequired.
type ResourceResponse interface {
	Result
	resourceResponse()
}

// ExtensionResult carries an extension-defined Result (e.g. the tasks
// extension's CreateTaskResult) through the sealed response unions. The server
// unwraps it before finalizing, so only the inner result reaches the wire.
type ExtensionResult struct{ R Result }

func (e *ExtensionResult) ResultType() string { return e.R.ResultType() }

func (*ExtensionResult) toolResponse()     {}
func (*ExtensionResult) promptResponse()   {}
func (*ExtensionResult) resourceResponse() {}

// --- Elicitation (the only first-class InputRequest payload) ---

type ElicitMode string

const (
	ElicitModeForm ElicitMode = "form"
	ElicitModeURL  ElicitMode = "url"
)

// ElicitParams is the params shape of an elicitation/create input request.
// A missing Mode means form.
type ElicitParams struct {
	Mode    ElicitMode `json:"mode,omitempty"`
	Message string     `json:"message"`
	// RequestedSchema is a flat object schema of primitives (form mode).
	RequestedSchema JSONSchema `json:"requestedSchema,omitempty"`
	// URL is the address the user should visit (url mode). Servers must use
	// url mode — never form mode — for secrets and credentials.
	URL string `json:"url,omitempty"`
}

func (p *ElicitParams) EffectiveMode() ElicitMode {
	if p.Mode == "" {
		return ElicitModeForm
	}
	return p.Mode
}

type ElicitAction string

const (
	ElicitActionAccept  ElicitAction = "accept"
	ElicitActionDecline ElicitAction = "decline"
	ElicitActionCancel  ElicitAction = "cancel"
)

// ElicitResult is the bare result body a client supplies for an elicitation
// input request. Content values are primitives or string arrays.
type ElicitResult struct {
	Action  ElicitAction   `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

// NewElicitFormRequest builds a form-mode elicitation input request.
func NewElicitFormRequest(message string, requestedSchema JSONSchema) InputRequest {
	raw, _ := json.Marshal(ElicitParams{Mode: ElicitModeForm, Message: message, RequestedSchema: requestedSchema})
	return InputRequest{Method: MethodElicitationCreate, Params: raw}
}

// NewElicitURLRequest builds a url-mode elicitation input request. There is no
// elicitationId in 2026-07-28; correlate via requestState instead.
func NewElicitURLRequest(message, url string) InputRequest {
	raw, _ := json.Marshal(ElicitParams{Mode: ElicitModeURL, Message: message, URL: url})
	return InputRequest{Method: MethodElicitationCreate, Params: raw}
}

// Elicit decodes the request params as ElicitParams; it fails if the request
// is not elicitation/create.
func (r InputRequest) Elicit() (*ElicitParams, error) {
	if r.Method != MethodElicitationCreate {
		return nil, fmt.Errorf("protocol: input request method is %q, not %q", r.Method, MethodElicitationCreate)
	}
	var p ElicitParams
	if err := json.Unmarshal(r.Params, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

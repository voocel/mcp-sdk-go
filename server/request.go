package server

import (
	"context"
	"encoding/json"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// Request is the per-request context of the stateless server. It replaces the
// former ServerSession entirely: everything a handler may need travels in the
// request's _meta.
type Request struct {
	id           protocol.RequestID
	method       string
	meta         *protocol.RequestMeta
	rawParams    json.RawMessage
	emit         func(*protocol.Message) error
	server       *Server
	templateVars map[string]string
}

func (r *Request) ID() protocol.RequestID { return r.id }

func (r *Request) Method() string { return r.method }

// RawParams exposes the undecoded params, mainly for middleware and extension
// handlers.
func (r *Request) RawParams() json.RawMessage { return r.rawParams }

func (r *Request) ProtocolVersion() string { return r.meta.ProtocolVersion }

// ClientInfo is self-reported and unverified; never use it for security
// decisions. It may be nil.
func (r *Request) ClientInfo() *protocol.Implementation { return r.meta.ClientInfo }

func (r *Request) ClientCapabilities() *protocol.ClientCapabilities {
	return &r.meta.ClientCapabilities
}

// Extension returns the per-request settings the client declared for an
// extension, and whether it declared the extension at all. Servers must gate
// extension behavior on this per request, regardless of prior declarations.
func (r *Request) Extension(id string) (json.RawMessage, bool) {
	raw, ok := r.meta.ClientCapabilities.Extensions[id]
	return raw, ok
}

// SupportsElicitation reports which elicitation modes the client declared for
// this request. Handlers should check before returning an elicitation
// InputRequired.
func (r *Request) SupportsElicitation() (form, url bool) {
	e := r.meta.ClientCapabilities.Elicitation
	return e.SupportsForm(), e.SupportsURL()
}

// TemplateVars returns the variables captured by resource template matching,
// or nil for non-template requests.
func (r *Request) TemplateVars() map[string]string { return r.templateVars }

// ReportProgress emits a notifications/progress on this request's response
// stream. It is a no-op when the client did not supply a progressToken.
// Successive calls must report increasing progress.
func (r *Request) ReportProgress(ctx context.Context, progress, total float64, message string) error {
	if r.meta.ProgressToken.IsZero() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	msg, err := protocol.NewNotification(protocol.NotificationProgress, protocol.ProgressParams{
		ProgressToken: r.meta.ProgressToken,
		Progress:      progress,
		Total:         total,
		Message:       message,
	})
	if err != nil {
		return err
	}
	return r.emit(msg)
}

// CallRequest is the typed request handed to tool handlers.
type CallRequest struct {
	*Request
	Params *protocol.CallToolParams
}

// PromptRequest is the typed request handed to prompt handlers.
type PromptRequest struct {
	*Request
	Params *protocol.GetPromptParams
}

// ResourceRequest is the typed request handed to resource handlers.
type ResourceRequest struct {
	*Request
	Params *protocol.ReadResourceParams
}

// unmarshalParams decodes raw params into a typed struct, mapping decode
// failures to -32602.
func unmarshalParams(raw json.RawMessage, v any) *protocol.Error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return protocol.Errorf(protocol.CodeInvalidParams, "invalid params: %v", err)
	}
	return nil
}

// extractMeta pulls _meta out of raw params without decoding the rest.
func extractMeta(raw json.RawMessage) (*protocol.RequestMeta, *protocol.Error) {
	var probe struct {
		Meta protocol.RequestMeta `json:"_meta"`
	}
	if err := unmarshalParams(raw, &probe); err != nil {
		return nil, err
	}
	return &probe.Meta, nil
}

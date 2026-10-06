// Package streamhttp implements the MCP Streamable HTTP transport
// (2026-07-28): a single POST endpoint, standard Mcp-* request headers,
// JSON or SSE responses, and stream-close cancellation. Handler is the server
// end; Transport is the client end.
package streamhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/voocel/mcp-sdk-go/internal/headerbind"
	"github.com/voocel/mcp-sdk-go/protocol"
)

// Backend is the structural seam of the server side, satisfied by
// *server.Server (embed it to wrap one). Header/body validation is a MUST of
// the spec and needs the registry-derived facts below, so they are part of the
// contract: a backend lacking them fails to compile instead of silently
// skipping validation.
type Backend interface {
	Handle(ctx context.Context, msg *protocol.Message, emit func(*protocol.Message) error)
	// ToolHeaderBindings returns the x-mcp-header bindings of a tool, for
	// Mcp-Param-* validation.
	ToolHeaderBindings(name string) []headerbind.Binding
	// MethodNameParam reports the params key backing the Mcp-Name header of an
	// extension method (e.g. tasks/get -> "taskId").
	MethodNameParam(method string) (key string, ok bool)
}

const defaultMaxBodyBytes = 8 << 20

type Options struct {
	// AllowedOrigins lists additional allowed Origin values (exact match,
	// e.g. "https://app.example.com"). Localhost origins and requests
	// without an Origin header are always allowed.
	AllowedOrigins []string
	// InsecureAllowAnyOrigin disables Origin validation entirely. As the name
	// says: do not set this on anything reachable from a browser.
	InsecureAllowAnyOrigin bool
	// MaxBodyBytes caps the request body (default 8 MiB).
	MaxBodyBytes int64
}

type Handler struct {
	backend Backend
	opts    Options
}

func NewHandler(backend Backend, opts *Options) *Handler {
	h := &Handler{backend: backend}
	if opts != nil {
		h.opts = *opts
	}
	if h.opts.MaxBodyBytes <= 0 {
		h.opts.MaxBodyBytes = defaultMaxBodyBytes
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// Legacy GET/DELETE (and anything else) → 405.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.originAllowed(r) {
		writeError(w, http.StatusForbidden, protocol.RequestID{},
			protocol.Errorf(protocol.CodeInvalidRequest, "origin not allowed"))
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusBadRequest, protocol.RequestID{},
			protocol.Errorf(protocol.CodeInvalidRequest, "Content-Type must be application/json"))
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.opts.MaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.RequestID{},
			protocol.Errorf(protocol.CodeInvalidRequest, "failed to read body: %v", err))
		return
	}
	var msg protocol.Message
	if err := json.Unmarshal(body, &msg); err != nil {
		writeError(w, http.StatusBadRequest, protocol.RequestID{},
			protocol.Errorf(protocol.CodeParseError, "parse error: %v", err))
		return
	}

	switch msg.Kind() {
	case protocol.KindNotification:
		// This revision defines no client-to-server notifications over HTTP,
		// but accepted notifications get 202 with no body.
		w.WriteHeader(http.StatusAccepted)
		return
	case protocol.KindRequest:
	default:
		writeError(w, http.StatusBadRequest, msg.ID,
			protocol.Errorf(protocol.CodeInvalidRequest, "body must be a single JSON-RPC request or notification"))
		return
	}

	if perr := h.validateHeaders(r, &msg); perr != nil {
		writeError(w, perr.HTTPStatus(), msg.ID, perr)
		return
	}

	// A Mcp-Session-Id from a legacy client is ignored, never echoed.
	rsp := &responder{w: w}
	h.backend.Handle(r.Context(), &msg, rsp.emit)
	rsp.finish()
}

func (h *Handler) originAllowed(r *http.Request) bool {
	if h.opts.InsecureAllowAnyOrigin {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// Only localhost origins and the explicit allowlist pass. Comparing the
	// Origin host against the request's Host header would defeat the check's
	// purpose: in a DNS rebinding attack both carry the attacker's domain
	// (rebound to a local address), so "same host" is exactly what must NOT
	// be trusted.
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	return slices.Contains(h.opts.AllowedOrigins, origin)
}

// validateHeaders enforces the standard header rules and the Mcp-Param-*
// validation matrix.
func (h *Handler) validateHeaders(r *http.Request, msg *protocol.Message) *protocol.Error {
	// MCP-Protocol-Version: required, must match _meta.
	hv := r.Header.Get(headerProtocolVersion)
	if hv == "" {
		return protocol.HeaderMismatchError("missing required header " + headerProtocolVersion)
	}
	var probe struct {
		Meta struct {
			Version string `json:"io.modelcontextprotocol/protocolVersion"`
		} `json:"_meta"`
		Name      string          `json:"name"`
		URI       string          `json:"uri"`
		Arguments json.RawMessage `json:"arguments"`
	}
	_ = json.Unmarshal(msg.Params, &probe)
	if probe.Meta.Version != "" && hv != probe.Meta.Version {
		return protocol.HeaderMismatchError(fmt.Sprintf(
			"%s header value '%s' does not match body value '%s'", headerProtocolVersion, hv, probe.Meta.Version))
	}

	// Mcp-Method: required, must match the body method.
	hm := r.Header.Get(headerMethod)
	if hm == "" {
		return protocol.HeaderMismatchError("missing required header " + headerMethod)
	}
	if hm != msg.Method {
		return protocol.HeaderMismatchError(fmt.Sprintf(
			"%s header value '%s' does not match body value '%s'", headerMethod, hm, msg.Method))
	}

	// Mcp-Name: required for the three named core methods, plus extension
	// methods that registered a routing param (e.g. tasks/* -> taskId).
	var bodyName string
	switch msg.Method {
	case protocol.MethodToolsCall, protocol.MethodPromptsGet:
		bodyName = probe.Name
	case protocol.MethodResourcesRead:
		bodyName = probe.URI
	default:
		key, ok := h.backend.MethodNameParam(msg.Method)
		if !ok {
			return nil
		}
		var pm map[string]any
		_ = json.Unmarshal(msg.Params, &pm)
		bodyName, _ = pm[key].(string)
	}
	hn := r.Header.Get(headerName)
	if hn == "" {
		return protocol.HeaderMismatchError("missing required header " + headerName)
	}
	decoded, err := headerbind.Decode(hn)
	if err != nil {
		return protocol.HeaderMismatchError(fmt.Sprintf("invalid %s header: %v", headerName, err))
	}
	if decoded != bodyName {
		return protocol.HeaderMismatchError(fmt.Sprintf(
			"%s header value '%s' does not match body value '%s'", headerName, decoded, bodyName))
	}

	if msg.Method == protocol.MethodToolsCall {
		return validateParamHeaders(r, h.backend.ToolHeaderBindings(bodyName), probe.Arguments)
	}
	return nil
}

// validateParamHeaders applies the spec's header/body validation matrix for
// x-mcp-header bindings.
func validateParamHeaders(r *http.Request, bindings []headerbind.Binding, rawArgs json.RawMessage) *protocol.Error {
	if len(bindings) == 0 {
		return nil
	}
	var args map[string]any
	_ = json.Unmarshal(rawArgs, &args)
	for _, b := range bindings {
		hval := r.Header.Get(b.HeaderName())
		v, present := headerbind.Lookup(args, b.Path)
		if !present {
			if hval != "" {
				return protocol.HeaderMismatchError(fmt.Sprintf(
					"header %s must be absent when the parameter is not provided", b.HeaderName()))
			}
			continue
		}
		if hval == "" {
			return protocol.HeaderMismatchError("missing required header " + b.HeaderName())
		}
		decoded, err := headerbind.Decode(hval)
		if err != nil {
			return protocol.HeaderMismatchError(fmt.Sprintf("invalid %s header: %v", b.HeaderName(), err))
		}
		want, err := headerbind.Format(v)
		if err != nil {
			return protocol.Errorf(protocol.CodeInvalidParams,
				"invalid value for header-bound parameter %s: %v", strings.Join(b.Path, "."), err)
		}
		if !headerValueEqual(b.Type, decoded, want) {
			return protocol.HeaderMismatchError(fmt.Sprintf(
				"%s header value '%s' does not match body value '%s'", b.HeaderName(), decoded, want))
		}
	}
	return nil
}

// headerValueEqual compares integers numerically ("42.0" == "42"), everything
// else byte-exact.
func headerValueEqual(typ, got, want string) bool {
	if typ != "integer" {
		return got == want
	}
	gi, err1 := strconv.ParseFloat(got, 64)
	wi, err2 := strconv.ParseFloat(want, 64)
	if err1 != nil || err2 != nil {
		return got == want
	}
	return gi == wi
}

// responder adapts emit to HTTP: a lone final response becomes a JSON body;
// as soon as a notification arrives it switches to SSE.
type responder struct {
	w  http.ResponseWriter
	mu sync.Mutex

	sse   bool
	wrote bool
}

func (rp *responder) emit(m *protocol.Message) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if !rp.wrote {
		rp.wrote = true
		if m.Kind() == protocol.KindNotification {
			rp.sse = true
			h := rp.w.Header()
			h.Set("Content-Type", "text/event-stream")
			h.Set("Cache-Control", "no-cache")
			h.Set("X-Accel-Buffering", "no")
			rp.w.WriteHeader(http.StatusOK)
			return writeSSE(rp.w, event{Data: raw})
		}
		// Single JSON response. Protocol-level errors map to HTTP statuses.
		status := http.StatusOK
		if m.Error != nil {
			status = m.Error.HTTPStatus()
		}
		rp.w.Header().Set("Content-Type", "application/json")
		rp.w.WriteHeader(status)
		_, err = rp.w.Write(raw)
		return err
	}
	if rp.sse {
		return writeSSE(rp.w, event{Data: raw})
	}
	return fmt.Errorf("streamhttp: message after final JSON response")
}

func (rp *responder) finish() {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if !rp.wrote {
		// Handler emitted nothing (should not happen): report it.
		rp.wrote = true
		rp.w.Header().Set("Content-Type", "application/json")
		rp.w.WriteHeader(http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, id protocol.RequestID, e *protocol.Error) {
	msg := protocol.NewErrorResponse(id, e)
	raw, err := json.Marshal(msg)
	if err != nil {
		http.Error(w, e.Message, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

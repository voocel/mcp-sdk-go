// Package client implements a stateless MCP client for protocol revision
// 2026-07-28. There is no session: a Client is configuration plus a transport;
// every request carries its own context in _meta.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"

	"github.com/voocel/mcp-sdk-go/internal/headerbind"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// ElicitHandler answers a form-mode elicitation input request.
type ElicitHandler func(ctx context.Context, p *protocol.ElicitParams) (*protocol.ElicitResult, error)

type Options struct {
	// Info is sent as clientInfo in every request's _meta.
	Info *protocol.Implementation

	// Elicitor enables form-mode elicitation: it is declared in the client
	// capabilities and drives the automatic MRTR fulfillment loop.
	Elicitor ElicitHandler

	// URLOpener enables url-mode elicitation. It should return once the user
	// has completed the out-of-band interaction; the outcome is learned by
	// retrying the original request.
	URLOpener func(ctx context.Context, url, message string) error

	// OnProgress receives notifications/progress for requests issued by this
	// client. When set, every request carries an auto-generated progressToken.
	OnProgress func(p *protocol.ProgressParams)

	// Extensions is declared verbatim in clientCapabilities.extensions on
	// every request (e.g. tasks.ID -> struct{}{}).
	Extensions map[string]any

	// RouteNames maps extension methods to the params key whose string value
	// must be sent as the Mcp-Name routing header over Streamable HTTP (the
	// tasks extension maps its methods to "taskId"; see tasks.EnableClient).
	// Core methods are built in.
	RouteNames map[string]string

	// MaxInputRounds caps MRTR retries per call (default 10).
	MaxInputRounds int
	// MaxLoadSheddingRetries caps retries when the server returns an
	// input_required result with no inputRequests (default 3).
	MaxLoadSheddingRetries int
	// NoAutoInput disables the automatic MRTR fulfillment loop; interim
	// results surface as *InputRequiredError for manual continuation.
	NoAutoInput bool

	// Logger receives warnings (e.g. invalid tools excluded from tools/list).
	// Defaults to slog.Default().
	Logger *slog.Logger
}

type Client struct {
	t       transport.Transport
	opts    Options
	extCaps map[string]json.RawMessage // Options.Extensions, marshaled once
	nextID  atomic.Int64

	mu       sync.RWMutex
	bindings map[string][]headerbind.Binding // tool name -> x-mcp-header bindings
}

// New builds a client. It panics on unmarshalable Options.Extensions values —
// configuration errors must fail loudly, not degrade into empty declarations.
func New(t transport.Transport, opts *Options) *Client {
	c := &Client{t: t, bindings: make(map[string][]headerbind.Binding)}
	if opts != nil {
		c.opts = *opts
	}
	if c.opts.MaxInputRounds <= 0 {
		c.opts.MaxInputRounds = 10
	}
	if c.opts.MaxLoadSheddingRetries <= 0 {
		c.opts.MaxLoadSheddingRetries = 3
	}
	if c.opts.Logger == nil {
		c.opts.Logger = slog.Default()
	}
	if len(c.opts.Extensions) > 0 {
		c.extCaps = make(map[string]json.RawMessage, len(c.opts.Extensions))
		for id, settings := range c.opts.Extensions {
			raw, err := json.Marshal(settings)
			if err != nil {
				panic(fmt.Sprintf("client: extension %s settings do not marshal: %v", id, err))
			}
			c.extCaps[id] = raw
		}
	}
	return c
}

func (c *Client) Close() error { return c.t.Close() }

// Call issues a raw request and decodes its result — the escape hatch for
// extension methods (it satisfies the tasks extension's Caller interface).
// Error responses surface as *protocol.Error.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	raw, err := c.do(ctx, method, params)
	if err != nil {
		return err
	}
	if result != nil {
		return json.Unmarshal(raw, result)
	}
	return nil
}

// do sends one request and returns the raw result body. Request-scoped
// notifications are dispatched along the way.
func (c *Client) do(ctx context.Context, method string, params any) (json.RawMessage, error) {
	pm, err := paramsToMap(params)
	if err != nil {
		return nil, err
	}
	id := protocol.IntID(c.nextID.Add(1))
	if err := c.stampMeta(pm, id); err != nil {
		return nil, err
	}
	rawParams, err := json.Marshal(pm)
	if err != nil {
		return nil, err
	}
	msg := &protocol.Message{JSONRPC: protocol.JSONRPCVersion, ID: id, Method: method, Params: rawParams}

	headers, err := c.headers(method, pm)
	if err != nil {
		return nil, err
	}
	stream, err := c.t.Do(ctx, &transport.Request{Message: msg, Headers: headers})
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	for {
		m, err := stream.Recv()
		if err == io.EOF {
			return nil, fmt.Errorf("client: stream ended without a response for %s", method)
		}
		if err != nil {
			return nil, err
		}
		switch m.Kind() {
		case protocol.KindNotification:
			c.dispatchNotification(m)
		case protocol.KindResponse:
			if m.ID == id {
				// An absent resultType is not rejected here: the spec makes
				// treating it as "complete" a client MUST, and
				// protocol.PeekResultType applies that rule for every caller.
				return m.Result, nil
			}
		case protocol.KindError:
			if m.ID == id || m.ID.IsZero() {
				return nil, m.Error
			}
		}
	}
}

func (c *Client) dispatchNotification(m *protocol.Message) {
	if m.Method == protocol.NotificationProgress && c.opts.OnProgress != nil {
		var p protocol.ProgressParams
		if err := json.Unmarshal(m.Params, &p); err == nil {
			c.opts.OnProgress(&p)
		}
	}
}

// stampMeta injects the required per-request _meta fields, preserving any
// caller-provided keys (progressToken, traceparent, vendor keys).
func (c *Client) stampMeta(pm map[string]any, id protocol.RequestID) error {
	meta, _ := pm["_meta"].(map[string]any)
	if meta == nil {
		meta = make(map[string]any)
	}
	meta[protocol.MetaProtocolVersion] = protocol.Version
	caps, err := toJSONValue(c.capabilities())
	if err != nil {
		return err
	}
	meta[protocol.MetaClientCapabilities] = caps
	if c.opts.Info != nil {
		info, err := toJSONValue(c.opts.Info)
		if err != nil {
			return err
		}
		meta[protocol.MetaClientInfo] = info
	}
	if c.opts.OnProgress != nil {
		if _, has := meta["progressToken"]; !has {
			meta["progressToken"] = fmt.Sprintf("pt-%s", id)
		}
	}
	pm["_meta"] = meta
	return nil
}

// capabilities derives the per-request declaration from what is actually
// configured, so it can never lie.
func (c *Client) capabilities() protocol.ClientCapabilities {
	var caps protocol.ClientCapabilities
	if c.opts.Elicitor != nil || c.opts.URLOpener != nil {
		caps.Elicitation = &protocol.ElicitationCapability{}
		if c.opts.Elicitor != nil {
			caps.Elicitation.Form = &struct{}{}
		}
		if c.opts.URLOpener != nil {
			caps.Elicitation.URL = &struct{}{}
		}
	}
	if len(c.extCaps) > 0 {
		caps.Extensions = maps.Clone(c.extCaps)
	}
	return caps
}

func paramsToMap(params any) (map[string]any, error) {
	if params == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	var pm map[string]any
	if err := json.Unmarshal(raw, &pm); err != nil {
		return nil, fmt.Errorf("client: params must marshal to a JSON object: %w", err)
	}
	if pm == nil {
		pm = map[string]any{}
	}
	return pm, nil
}

func toJSONValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

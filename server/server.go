// Package server implements a stateless MCP server for protocol revision
// 2026-07-28. A Server is a pure function over messages: transports feed it
// one message at a time via Handle and receive the emitted stream of
// notifications followed by exactly one response. There is no session object;
// all per-request context travels in _meta.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/voocel/mcp-sdk-go/protocol"
)

type Options struct {
	// Impl is stamped as serverInfo into every result's _meta unless
	// OmitServerInfo is set.
	Impl           protocol.Implementation
	Instructions   string
	OmitServerInfo bool

	// PageSize bounds list results. Defaults to DefaultPageSize.
	PageSize int

	// ListCache is the default cache control applied to cacheable results the
	// handler left unset. The zero value ({0, private}) is the safest choice
	// and therefore the default.
	ListCache protocol.CacheControl

	// StateKey enables the SignState/VerifyState HMAC helpers for MRTR
	// requestState integrity.
	StateKey []byte

	// MaxConcurrency bounds concurrently executing requests. 0 means
	// unlimited (appropriate for HTTP, where the listener governs).
	MaxConcurrency int

	// OnDiscover mutates the assembled DiscoverResult before it is sent
	// (e.g. tenant-specific instructions or _meta annotations). It edits the
	// presentation only: capabilities and method availability derive from
	// registrations, so hiding a capability here does not gate its methods.
	// Per-tenant access control belongs in a Middleware, which sees every
	// request.
	OnDiscover func(ctx context.Context, req *Request, d *protocol.DiscoverResult) error

	// CompletionHandler enables the completions capability.
	CompletionHandler func(ctx context.Context, req *Request, p *protocol.CompleteParams) (*protocol.CompleteResult, error)
}

type Server struct {
	opts Options

	mu                sync.RWMutex
	tools             *featureSet[*serverTool]
	prompts           *featureSet[*serverPrompt]
	resources         *featureSet[*serverResource]
	resourceTemplates *featureSet[*serverResourceTemplate]
	middleware        []Middleware
	extensions        map[string]Extension
	extSettings       map[string]json.RawMessage
	extMethods        map[string]RawHandler
	extNameParams     map[string]string

	// Sticky capability declarations: set on first registration, never unset.
	// Per spec a declared capability's list may become empty ("This set MAY be
	// empty and MAY change over time") — removing the last tool must not turn
	// tools/list into -32601 while a list_changed notification is in flight.
	toolsDeclared     bool
	promptsDeclared   bool
	resourcesDeclared bool

	hub     *hub
	methods map[string]RawHandler
	sem     chan struct{}

	// Shutdown state: closing ends every listen stream; streams tracks the
	// active ones until their final message has been emitted.
	closing   chan struct{}
	closeOnce sync.Once
	streamMu  sync.Mutex
	streams   map[chan struct{}]struct{}
}

func New(opts *Options) *Server {
	s := &Server{
		tools:             newFeatureSet(func(t *serverTool) string { return t.tool.Name }),
		prompts:           newFeatureSet(func(p *serverPrompt) string { return p.prompt.Name }),
		resources:         newFeatureSet(func(r *serverResource) string { return r.resource.URI }),
		resourceTemplates: newFeatureSet(func(t *serverResourceTemplate) string { return t.template.URITemplate }),
		extensions:        make(map[string]Extension),
		extSettings:       make(map[string]json.RawMessage),
		extMethods:        make(map[string]RawHandler),
		extNameParams:     make(map[string]string),
		hub:               newHub(),
		closing:           make(chan struct{}),
		streams:           make(map[chan struct{}]struct{}),
	}
	if opts != nil {
		s.opts = *opts
	}
	if s.opts.PageSize <= 0 {
		s.opts.PageSize = DefaultPageSize
	}
	if s.opts.ListCache.CacheScope == "" {
		s.opts.ListCache.CacheScope = protocol.CacheScopePrivate
	}
	if s.opts.MaxConcurrency > 0 {
		s.sem = make(chan struct{}, s.opts.MaxConcurrency)
	}
	s.methods = map[string]RawHandler{
		protocol.MethodDiscover:               s.handleDiscover,
		protocol.MethodToolsList:              s.handleListTools,
		protocol.MethodToolsCall:              s.handleCallTool,
		protocol.MethodPromptsList:            s.handleListPrompts,
		protocol.MethodPromptsGet:             s.handleGetPrompt,
		protocol.MethodResourcesList:          s.handleListResources,
		protocol.MethodResourcesTemplatesList: s.handleListResourceTemplates,
		protocol.MethodResourcesRead:          s.handleReadResource,
		protocol.MethodCompletionComplete:     s.handleComplete,
		protocol.MethodSubscriptionsListen:    s.handleListen,
	}
	return s
}

// Extension plugs additional methods and subscription topics into the server
// (e.g. the official tasks extension). The server itself knows nothing about
// any specific extension.
type Extension struct {
	// ID is the extension identifier, e.g. "io.modelcontextprotocol/tasks".
	ID string
	// Settings is marshaled into capabilities.extensions[ID]. Use struct{}{}
	// for "supported, no settings".
	Settings any
	// Methods maps additional RPC method names to handlers.
	Methods map[string]RawHandler
	// NameParams maps extension methods to the params key whose string value
	// clients send as the Mcp-Name routing header over Streamable HTTP (the
	// tasks draft maps its methods to "taskId"). The transport validates the
	// header against the body via Server.MethodNameParam.
	NameParams map[string]string
	// Topics translates one extension field of a subscription filter (key,
	// raw value) into hub topics. Returning ok=false leaves the field to
	// other extensions; ok=true with no topics leaves it unhonored. A non-nil
	// err rejects the whole listen request (e.g. a missing client capability).
	Topics func(ctx context.Context, req *Request, key string, value json.RawMessage) (topics []string, ok bool, err error)
}

// AddExtension registers an extension. It panics on ID or method collisions
// and on unmarshalable Settings — registration errors are programmer errors
// and must fail loudly, not degrade silently at discover time.
func (s *Server) AddExtension(e Extension) {
	if e.ID == "" {
		panic("server: extension ID must not be empty")
	}
	settings := e.Settings
	if settings == nil {
		settings = struct{}{}
	}
	settingsRaw, err := json.Marshal(settings)
	if err != nil {
		panic(fmt.Sprintf("server: extension %s settings do not marshal: %v", e.ID, err))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.extensions[e.ID]; dup {
		panic("server: duplicate extension " + e.ID)
	}
	for name := range e.Methods {
		if _, dup := s.methods[name]; dup {
			panic("server: extension method collides with core method " + name)
		}
		if _, dup := s.extMethods[name]; dup {
			panic("server: duplicate extension method " + name)
		}
	}
	s.extensions[e.ID] = e
	s.extSettings[e.ID] = settingsRaw
	maps.Copy(s.extMethods, e.Methods)
	maps.Copy(s.extNameParams, e.NameParams)
}

// MethodNameParam reports the params key backing the Mcp-Name routing header
// for an extension method, if the extension registered one. It is the seam
// the Streamable HTTP transport uses to validate headers on extension
// methods; core methods are built into the transport.
func (s *Server) MethodNameParam(method string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.extNameParams[method]
	return key, ok
}

// Publish delivers a notification to all subscription streams listening on
// topic. Intended for extensions; core notifications flow through the same
// hub internally. Delivery itself is best-effort (an overflowing stream is
// terminated visibly on its own side); the returned error reports encoding
// failures, which mean the notification reached no subscriber at all.
func (s *Server) Publish(topic, method string, params any) error {
	return s.hub.publish(topic, method, params)
}

// Handle processes one message. For requests, emit is called zero or more
// times with request-scoped notifications and then exactly once with the
// final response (subscriptions/listen keeps emitting until ctx ends).
// Cancelling ctx aborts the in-flight handler; transports translate their
// native cancellation signal (HTTP stream close, stdio notifications/cancelled)
// into ctx cancellation.
func (s *Server) Handle(ctx context.Context, msg *protocol.Message, emit func(*protocol.Message) error) {
	switch msg.Kind() {
	case protocol.KindNotification:
		// notifications/cancelled is transport-level; nothing reaches here.
		return
	case protocol.KindRequest:
		s.handleRequest(ctx, msg, emit)
	default:
		_ = emit(protocol.NewErrorResponse(msg.ID,
			protocol.Errorf(protocol.CodeInvalidRequest, "invalid JSON-RPC message")))
	}
}

func (s *Server) handleRequest(ctx context.Context, msg *protocol.Message, emit func(*protocol.Message) error) {
	if msg.Method == protocol.MethodSubscriptionsListen {
		// Tracked until the final response is emitted, which Shutdown waits for.
		defer s.trackStream()()
	}
	req := &Request{
		id:        msg.ID,
		method:    msg.Method,
		rawParams: msg.Params,
		emit:      emit,
		server:    s,
	}
	res, err := s.dispatch(ctx, req)
	if err != nil {
		_ = emit(protocol.NewErrorResponse(msg.ID, toProtocolError(err)))
		return
	}
	if er, ok := res.(*protocol.ExtensionResult); ok {
		if er == nil || er.R == nil {
			res = nil
		} else {
			res = er.R
		}
	}
	if res == nil {
		_ = emit(protocol.NewErrorResponse(msg.ID, protocol.Errorf(protocol.CodeInternal, "handler returned no result")))
		return
	}
	if e := s.finalize(req, res); e != nil {
		_ = emit(protocol.NewErrorResponse(msg.ID, e))
		return
	}
	out, merr := protocol.NewResponse(msg.ID, res)
	if merr != nil {
		_ = emit(protocol.NewErrorResponse(msg.ID, protocol.Errorf(protocol.CodeInternal, "failed to marshal result: %v", merr)))
		return
	}
	_ = emit(out)
}

func (s *Server) dispatch(ctx context.Context, req *Request) (protocol.Result, error) {
	meta, perr := extractMeta(req.rawParams)
	if perr != nil {
		return nil, perr
	}
	req.meta = meta
	if err := meta.Validate(); err != nil {
		return nil, err
	}
	// server/discover is exempt from the version match: the spec defines it as
	// the version-selection and backward-compatibility probe, so a caller on
	// another revision must still learn what this server speaks.
	if meta.ProtocolVersion != protocol.Version && req.method != protocol.MethodDiscover {
		return nil, protocol.UnsupportedVersionError(meta.ProtocolVersion, []string{protocol.Version})
	}

	h := s.lookupMethod(req.method)
	if h == nil || !s.methodAdvertised(req.method) {
		return nil, protocol.MethodNotFoundError(req.method)
	}

	// subscriptions/listen holds its slot for the lifetime of the stream, so
	// counting it would let idle subscribers starve every other request.
	if s.sem != nil && req.method != protocol.MethodSubscriptionsListen {
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	s.mu.RLock()
	mw := s.middleware
	s.mu.RUnlock()
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h(ctx, req)
}

// methodAdvertised reports whether the feature backing a core method is
// currently advertised. Per the spec, a method gated behind a capability the
// server does not advertise is treated as not found (-32601). The check reads
// the same registrations discover derives capabilities from, so the two stay
// consistent (OnDiscover edits presentation only; see Options.OnDiscover).
func (s *Server) methodAdvertised(method string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch method {
	case protocol.MethodToolsList, protocol.MethodToolsCall:
		return s.toolsDeclared
	case protocol.MethodPromptsList, protocol.MethodPromptsGet:
		return s.promptsDeclared
	case protocol.MethodResourcesList, protocol.MethodResourcesTemplatesList, protocol.MethodResourcesRead:
		return s.resourcesDeclared
	}
	return true
}

func (s *Server) lookupMethod(method string) RawHandler {
	if h, ok := s.methods[method]; ok {
		return h
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.extMethods[method]
}

// finalize applies the invariants every outgoing result must satisfy:
// input_required validity and capability conformance, cache-control defaults,
// and serverInfo stamping.
func (s *Server) finalize(req *Request, res protocol.Result) *protocol.Error {
	if ir, ok := res.(*protocol.InputRequired); ok {
		if !methodSupportsMRTR(req.method) {
			return protocol.Errorf(protocol.CodeInternal,
				"server bug: %s must not return input_required", req.method)
		}
		if err := ir.Validate(); err != nil {
			return protocol.Errorf(protocol.CodeInternal, "server bug: %v", err)
		}
		if err := s.checkInputCapabilities(req, ir); err != nil {
			return err
		}
	}
	if c, ok := res.(protocol.Cacheable); ok {
		cc := c.CacheControlRef()
		if cc.TTLMs < 0 {
			cc.TTLMs = 0
		}
		if cc.CacheScope == "" {
			if cc.TTLMs == 0 {
				cc.TTLMs = s.opts.ListCache.TTLMs
			}
			cc.CacheScope = s.opts.ListCache.CacheScope
		}
	}
	if mc, ok := res.(protocol.MetaCarrier); ok && !s.opts.OmitServerInfo {
		m := mc.ResultMetaRef()
		if m.ServerInfo == nil {
			impl := s.opts.Impl
			m.ServerInfo = &impl
		}
	}
	return nil
}

func methodSupportsMRTR(method string) bool {
	switch method {
	case protocol.MethodToolsCall, protocol.MethodPromptsGet, protocol.MethodResourcesRead:
		return true
	}
	return false
}

// checkInputCapabilities enforces the MRTR rule that a server must not send
// input requests the client has not declared support for on this request.
func (s *Server) checkInputCapabilities(req *Request, ir *protocol.InputRequired) *protocol.Error {
	form, url := req.SupportsElicitation()
	for key, in := range ir.Requests {
		switch in.Method {
		case protocol.MethodElicitationCreate:
		case "sampling/createMessage", "roots/list":
			// Legal MRTR payloads, deprecated by SEP-2577. This SDK does not
			// model their client capabilities, so the handler owns that
			// contract; the payload passes through untouched.
			continue
		default:
			// The spec's whitelist is a MUST: inputRequests values must be
			// one of ElicitRequest, CreateMessageRequest or ListRootsRequest.
			return protocol.Errorf(protocol.CodeInternal,
				"server bug: input request %q uses method %q, which is not a valid inputRequests method", key, in.Method)
		}
		p, err := in.Elicit()
		if err != nil {
			return protocol.Errorf(protocol.CodeInternal, "server bug: invalid input request %q: %v", key, err)
		}
		mode := p.EffectiveMode()
		if (mode == protocol.ElicitModeForm && !form) || (mode == protocol.ElicitModeURL && !url) {
			required := protocol.ClientCapabilities{Elicitation: &protocol.ElicitationCapability{}}
			if mode == protocol.ElicitModeForm {
				required.Elicitation.Form = &struct{}{}
			} else {
				required.Elicitation.URL = &struct{}{}
			}
			return protocol.MissingCapabilityError(required)
		}
	}
	return nil
}

func (s *Server) handleComplete(ctx context.Context, req *Request) (protocol.Result, error) {
	if s.opts.CompletionHandler == nil {
		return nil, protocol.MethodNotFoundError(req.method)
	}
	var p protocol.CompleteParams
	if err := unmarshalParams(req.rawParams, &p); err != nil {
		return nil, err
	}
	return s.opts.CompletionHandler(ctx, req, &p)
}

func toProtocolError(err error) *protocol.Error {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		return pe
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return protocol.Errorf(protocol.CodeInternal, "request cancelled: %v", err)
	}
	return protocol.Errorf(protocol.CodeInternal, "%v", err)
}

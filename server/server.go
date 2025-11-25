package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// Server represents an MCP server instance that can serve one or more MCP sessions
type Server struct {
	impl *protocol.ServerInfo
	opts ServerOptions

	mu                    sync.Mutex
	middlewares           []Middleware // Middleware chain
	tools                 map[string]*serverTool
	resources             map[string]*serverResource
	resourceTemplates     map[string]*serverResourceTemplate
	prompts               map[string]*serverPrompt
	sessions              []*ServerSession
	resourceSubscriptions map[string]map[*ServerSession]bool // uri -> session -> bool
}

type ServerOptions struct {
	// Optional client instructions
	Instructions string

	// Initialized handler function
	InitializedHandler func(context.Context, *ServerSession)

	// Progress notification handler function
	ProgressNotificationHandler func(context.Context, *ServerSession, *protocol.ProgressNotificationParams)

	// Completion handler function
	CompletionHandler func(context.Context, *protocol.CompleteRequest) (*protocol.CompleteResult, error)

	// Logging level setting handler function
	LoggingSetLevelHandler func(context.Context, *ServerSession, protocol.LoggingLevel) error

	// Resource subscribe/unsubscribe handler functions
	SubscribeHandler   func(context.Context, *protocol.SubscribeParams) error
	UnsubscribeHandler func(context.Context, *protocol.UnsubscribeParams) error

	// KeepAlive defines the interval for periodic "ping" requests
	// If the peer fails to respond to a keepalive ping, the session will be closed automatically
	KeepAlive time.Duration
}

type serverTool struct {
	tool    *protocol.Tool
	handler ToolHandler
}

type serverResource struct {
	resource *protocol.Resource
	handler  ResourceHandler
}

type serverResourceTemplate struct {
	template *protocol.ResourceTemplate
	handler  ResourceHandler
}

type serverPrompt struct {
	prompt  *protocol.Prompt
	handler PromptHandler
}

type ResourceHandler func(ctx context.Context, req *ReadResourceRequest) (*protocol.ReadResourceResult, error)
type PromptHandler func(ctx context.Context, req *GetPromptRequest) (*protocol.GetPromptResult, error)

type ReadResourceRequest struct {
	Session *ServerSession
	Params  *protocol.ReadResourceParams
}

type GetPromptRequest struct {
	Session *ServerSession
	Params  *protocol.GetPromptParams
}

func NewServer(impl *protocol.ServerInfo, opts *ServerOptions) *Server {
	s := &Server{
		impl:                  impl,
		tools:                 make(map[string]*serverTool),
		resources:             make(map[string]*serverResource),
		resourceTemplates:     make(map[string]*serverResourceTemplate),
		prompts:               make(map[string]*serverPrompt),
		sessions:              make([]*ServerSession, 0),
		resourceSubscriptions: make(map[string]map[*ServerSession]bool),
	}
	if opts != nil {
		s.opts = *opts
	}
	return s
}

// AddTool adds a tool to the server, or replaces a tool with the same name (low-level API).
// The Tool parameter must not be modified after this call.
//
// The tool's input schema must be non-nil and have type "object". For tools that accept
// no input or any input, set [Tool.InputSchema] to `{"type": "object"}` using your
// preferred library or `json.RawMessage`.
//
// If [Tool.OutputSchema] exists, it must also have type "object".
//
// When the handler is invoked as part of a CallTool request, req.Params.Arguments
// will be json.RawMessage.
//
// It is the caller's responsibility to deserialize arguments and validate them
// against the input schema.
//
// It is the caller's responsibility to validate the result against the output
// schema (if any).
//
// It is the caller's responsibility to set the Content, StructuredContent, and
// IsError fields of the result.
//
// Most users should use the top-level function [AddTool], which handles all
// these responsibilities.
func (s *Server) AddTool(t *protocol.Tool, h ToolHandler) {
	if t.InputSchema == nil {
		panic(fmt.Errorf("AddTool %q: missing input schema", t.Name))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Apply middleware
	wrappedHandler := applyMiddleware(h, s.middlewares)

	s.tools[t.Name] = &serverTool{
		tool:    t,
		handler: wrappedHandler,
	}

	// Notify all sessions that the tool list has changed
	s.notifyToolListChanged()
}

func (s *Server) RemoveTool(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tools[name]; exists {
		delete(s.tools, name)
		s.notifyToolListChanged()
	}
}

func (s *Server) AddResource(r *protocol.Resource, h ResourceHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.resources[r.URI] = &serverResource{
		resource: r,
		handler:  h,
	}

	s.notifyResourceListChanged()
}

func (s *Server) RemoveResource(uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.resources[uri]; exists {
		delete(s.resources, uri)
		s.notifyResourceListChanged()
	}
}

func (s *Server) AddResourceTemplate(t *protocol.ResourceTemplate, h ResourceHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.resourceTemplates[t.URITemplate] = &serverResourceTemplate{
		template: t,
		handler:  h,
	}

	s.notifyResourceTemplateListChanged()
}

func (s *Server) RemoveResourceTemplate(uriTemplate string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.resourceTemplates[uriTemplate]; exists {
		delete(s.resourceTemplates, uriTemplate)
		s.notifyResourceTemplateListChanged()
	}
}

func (s *Server) AddPrompt(p *protocol.Prompt, h PromptHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.prompts[p.Name] = &serverPrompt{
		prompt:  p,
		handler: h,
	}

	s.notifyPromptListChanged()
}

func (s *Server) RemovePrompt(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.prompts[name]; exists {
		delete(s.prompts, name)
		s.notifyPromptListChanged()
	}
}

// Run runs the server on the given transport.
// This is a convenience method for handling a single session (or one session at a time).
//
// Run blocks until the client terminates the connection or the provided context is cancelled.
// If the context is cancelled, Run will close the connection.
func (s *Server) Run(ctx context.Context, t transport.Transport) error {
	ss, err := s.Connect(ctx, t, nil)
	if err != nil {
		return err
	}

	ssClosed := make(chan error)
	go func() {
		ssClosed <- ss.Wait()
	}()

	select {
	case <-ctx.Done():
		ss.Close()
		<-ssClosed // Wait for goroutine to finish
		return ctx.Err()
	case err := <-ssClosed:
		return err
	}
}

// Connect connects the MCP server via the given transport and starts processing messages.
//
// It returns a connection object that can be used to terminate the connection (using Close)
// or wait for the client to terminate (using Wait).
func (s *Server) Connect(ctx context.Context, t transport.Transport, opts *ServerSessionOptions) (*ServerSession, error) {
	conn, err := t.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("transport connect failed: %w", err)
	}

	ss := &ServerSession{
		server:          s,
		conn:            newConnAdapter(conn),
		waitErr:         make(chan error, 1),
		pendingRequests: make(map[string]context.CancelFunc),
	}

	if opts != nil && opts.State != nil {
		ss.state = *opts.State
	}

	if opts != nil && opts.onClose != nil {
		ss.onClose = opts.onClose
	}

	s.mu.Lock()
	s.sessions = append(s.sessions, ss)
	s.mu.Unlock()

	// Start message processing loop
	go func() {
		err := s.handleConnection(ctx, ss, ss.conn)
		ss.waitErr <- err
		close(ss.waitErr)
	}()

	return ss, nil
}

// handleConnection handles the message loop for a connection
func (s *Server) handleConnection(ctx context.Context, ss *ServerSession, conn Connection) error {
	defer func() {
		s.disconnect(ss)
		conn.Close()
	}()

	// Get the underlying connAdapter for handling response messages
	adapter, ok := conn.(*connAdapter)
	if !ok {
		return fmt.Errorf("invalid connection type")
	}

	for {
		// Explicitly check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := adapter.conn.Read(ctx)
		if err != nil {
			return err
		}

		// If it's a response message, route to connAdapter
		if msg.Method == "" && msg.ID != nil {
			adapter.handleResponse(msg)
			continue
		}

		response := s.handleMessage(ctx, ss, msg)
		if response != nil {
			if err := adapter.conn.Write(ctx, response); err != nil {
				return err
			}
		}
	}
}

// handleMessage handles a single JSON-RPC message
func (s *Server) handleMessage(ctx context.Context, ss *ServerSession, msg *protocol.JSONRPCMessage) *protocol.JSONRPCMessage {
	if msg.ID != nil {
		// Request - needs response
		// Create cancellable context and track request
		requestID := protocol.IDToString(msg.ID)
		requestCtx, cancel := context.WithCancel(ctx)

		ss.mu.Lock()
		ss.pendingRequests[requestID] = cancel
		ss.mu.Unlock()

		// Ensure request is cleaned up after completion
		defer func() {
			ss.mu.Lock()
			delete(ss.pendingRequests, requestID)
			ss.mu.Unlock()
			cancel()
		}()

		result, err := s.handleRequest(requestCtx, ss, msg.Method, msg.Params)
		if err != nil {
			return &protocol.JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Error: &protocol.JSONRPCError{
					Code:    protocol.InternalError,
					Message: err.Error(),
				},
			}
		}

		// Serialize result
		resultBytes, err := json.Marshal(result)
		if err != nil {
			return &protocol.JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Error: &protocol.JSONRPCError{
					Code:    protocol.InternalError,
					Message: fmt.Sprintf("failed to marshal result: %v", err),
				},
			}
		}

		return &protocol.JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  json.RawMessage(resultBytes),
		}
	} else {
		// Notification - no response needed
		_ = s.handleNotification(ctx, ss, msg.Method, msg.Params)
		return nil
	}
}

func (s *Server) disconnect(ss *ServerSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, session := range s.sessions {
		if session == ss {
			s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
			break
		}
	}

	for _, subscribedSessions := range s.resourceSubscriptions {
		delete(subscribedSessions, ss)
	}
}

type ServerSessionOptions struct {
	State   *ServerSessionState
	onClose func()
}

// notifyToolListChanged notifies all sessions that the tool list has changed
func (s *Server) notifyToolListChanged() {
	for _, ss := range s.sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationToolsListChanged, &protocol.ToolListChangedParams{})
	}
}

// notifyResourceListChanged notifies all sessions that the resource list has changed
func (s *Server) notifyResourceListChanged() {
	for _, ss := range s.sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationResourcesListChanged, &protocol.ResourceListChangedParams{})
	}
}

// notifyResourceTemplateListChanged notifies all sessions that the resource template list has changed
func (s *Server) notifyResourceTemplateListChanged() {
	for _, ss := range s.sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationResourcesTemplatesListChanged, &protocol.ResourceTemplateListChangedParams{})
	}
}

// notifyPromptListChanged notifies all sessions that the prompt list has changed
func (s *Server) notifyPromptListChanged() {
	for _, ss := range s.sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationPromptsListChanged, &protocol.PromptListChangedParams{})
	}
}

// NotifyResourceUpdated notifies all sessions subscribed to the specified resource that it has been updated.
// Only clients that have previously called resources/subscribe to subscribe to this URI will receive the notification.
func (s *Server) NotifyResourceUpdated(uri string) {
	s.mu.Lock()
	subscribedSessions, exists := s.resourceSubscriptions[uri]
	if !exists || len(subscribedSessions) == 0 {
		s.mu.Unlock()
		return
	}

	// Copy session list to avoid holding lock for too long
	sessions := make([]*ServerSession, 0, len(subscribedSessions))
	for ss := range subscribedSessions {
		sessions = append(sessions, ss)
	}
	s.mu.Unlock()

	// Send notifications
	params := &protocol.ResourceUpdatedNotificationParams{
		URI: uri,
	}
	for _, ss := range sessions {
		if ss.conn != nil {
			_ = ss.conn.SendNotification(context.Background(), protocol.NotificationResourcesUpdated, params)
		}
	}
}

// handleRequest handles requests from the client
func (s *Server) handleRequest(ctx context.Context, ss *ServerSession, method string, params json.RawMessage) (interface{}, error) {
	switch method {
	case protocol.MethodInitialize:
		return s.handleInitialize(ctx, ss, params)
	case protocol.MethodToolsList:
		return s.handleListTools(ctx, ss, params)
	case protocol.MethodToolsCall:
		return s.handleCallTool(ctx, ss, params)
	case protocol.MethodResourcesList:
		return s.handleListResources(ctx, ss, params)
	case protocol.MethodResourcesRead:
		return s.handleReadResource(ctx, ss, params)
	case protocol.MethodResourcesTemplatesList:
		return s.handleListResourceTemplates(ctx, ss, params)
	case protocol.MethodResourcesSubscribe:
		return s.handleSubscribe(ctx, ss, params)
	case protocol.MethodResourcesUnsubscribe:
		return s.handleUnsubscribe(ctx, ss, params)
	case protocol.MethodPromptsList:
		return s.handleListPrompts(ctx, ss, params)
	case protocol.MethodPromptsGet:
		return s.handleGetPrompt(ctx, ss, params)
	case protocol.MethodPing:
		return &protocol.EmptyResult{}, nil
	case protocol.MethodCompletionComplete:
		return s.handleComplete(ctx, ss, params)
	case protocol.MethodLoggingSetLevel:
		return s.handleSetLoggingLevel(ctx, ss, params)
	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}
}

// handleNotification handles notifications from the client
func (s *Server) handleNotification(ctx context.Context, ss *ServerSession, method string, params json.RawMessage) error {
	switch method {
	case protocol.NotificationInitialized:
		return s.handleInitialized(ctx, ss, params)
	case protocol.NotificationCancelled:
		return s.handleCancelled(ctx, ss, params)
	case protocol.NotificationProgress:
		return s.handleProgress(ctx, ss, params)
	case protocol.NotificationRootsListChanged:
		return s.handleRootsListChanged(ctx, ss, params)
	default:
		return fmt.Errorf("unknown notification: %s", method)
	}
}

// handleInitialize handles the initialize request
func (s *Server) handleInitialize(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.InitializeResult, error) {
	var req protocol.InitializeParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid initialize params: %w", err)
	}

	if !protocol.IsVersionSupported(req.ProtocolVersion) {
		return nil, fmt.Errorf("unsupported protocol version: %s (supported: %v)",
			req.ProtocolVersion, protocol.GetSupportedVersions())
	}

	ss.updateState(func(state *ServerSessionState) {
		state.InitializeParams = &req
	})

	capabilities := protocol.ServerCapabilities{}

	s.mu.Lock()
	if len(s.tools) > 0 {
		capabilities.Tools = &protocol.ToolsCapability{ListChanged: true}
	}
	if len(s.resources) > 0 {
		capabilities.Resources = &protocol.ResourcesCapability{
			ListChanged: true,
			Subscribe:   true,
		}
	}
	if len(s.prompts) > 0 {
		capabilities.Prompts = &protocol.PromptsCapability{ListChanged: true}
	}
	s.mu.Unlock()

	capabilities.Logging = &protocol.LoggingCapability{}

	if s.opts.CompletionHandler != nil {
		capabilities.Completion = &protocol.CompletionCapability{}
	}

	return &protocol.InitializeResult{
		ProtocolVersion: req.ProtocolVersion,
		Capabilities:    capabilities,
		ServerInfo:      *s.impl,
		Instructions:    s.opts.Instructions,
	}, nil
}

// handleInitialized handles the initialized notification
func (s *Server) handleInitialized(ctx context.Context, ss *ServerSession, params json.RawMessage) error {
	var req protocol.InitializedParams
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("invalid initialized params: %w", err)
	}

	ss.updateState(func(state *ServerSessionState) {
		state.InitializedParams = &req
	})

	// Start keepalive
	if s.opts.KeepAlive > 0 {
		ss.startKeepalive(s.opts.KeepAlive)
	}

	if s.opts.InitializedHandler != nil {
		s.opts.InitializedHandler(ctx, ss)
	}

	return nil
}

// handleListTools handles the tools/list request
func (s *Server) handleListTools(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.ListToolsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tools := make([]protocol.Tool, 0, len(s.tools))
	for _, st := range s.tools {
		tools = append(tools, *st.tool)
	}

	return &protocol.ListToolsResult{
		Tools: tools,
	}, nil
}

// handleCallTool handles the tools/call request
func (s *Server) handleCallTool(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.CallToolResult, error) {
	var req protocol.CallToolParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid call tool params: %w", err)
	}

	s.mu.Lock()
	st, exists := s.tools[req.Name]
	s.mu.Unlock()

	if !exists {
		return protocol.NewToolResultError(fmt.Sprintf("tool not found: %s", req.Name)), nil
	}

	toolReq := &CallToolRequest{
		Session: ss,
		Params:  &req,
	}

	return st.handler(ctx, toolReq)
}

// handleListResources handles the resources/list request
func (s *Server) handleListResources(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.ListResourcesResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resources := make([]protocol.Resource, 0, len(s.resources))
	for _, sr := range s.resources {
		resources = append(resources, *sr.resource)
	}

	return &protocol.ListResourcesResult{
		Resources: resources,
	}, nil
}

// handleListResourceTemplates handles the resources/templates/list request
func (s *Server) handleListResourceTemplates(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.ListResourceTemplatesResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	templates := make([]protocol.ResourceTemplate, 0, len(s.resourceTemplates))
	for _, srt := range s.resourceTemplates {
		templates = append(templates, *srt.template)
	}

	return &protocol.ListResourceTemplatesResult{
		ResourceTemplates: templates,
	}, nil
}

// handleReadResource handles the resources/read request
func (s *Server) handleReadResource(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.ReadResourceResult, error) {
	var req protocol.ReadResourceParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid read resource params: %w", err)
	}

	s.mu.Lock()
	sr, exists := s.resources[req.URI]
	s.mu.Unlock()

	if !exists {
		return nil, fmt.Errorf("resource not found: %s", req.URI)
	}

	resourceReq := &ReadResourceRequest{
		Session: ss,
		Params:  &req,
	}

	return sr.handler(ctx, resourceReq)
}

// handleSubscribe handles the resources/subscribe request
func (s *Server) handleSubscribe(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.EmptyResult, error) {
	if s.opts.SubscribeHandler == nil {
		return nil, fmt.Errorf("resource subscription not supported")
	}

	var req protocol.SubscribeParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid subscribe params: %w", err)
	}

	if err := s.opts.SubscribeHandler(ctx, &req); err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.resourceSubscriptions[req.URI] == nil {
		s.resourceSubscriptions[req.URI] = make(map[*ServerSession]bool)
	}
	s.resourceSubscriptions[req.URI][ss] = true
	s.mu.Unlock()

	return &protocol.EmptyResult{}, nil
}

// handleUnsubscribe handles the resources/unsubscribe request
func (s *Server) handleUnsubscribe(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.EmptyResult, error) {
	if s.opts.UnsubscribeHandler == nil {
		return nil, fmt.Errorf("resource unsubscription not supported")
	}

	var req protocol.UnsubscribeParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid unsubscribe params: %w", err)
	}

	if err := s.opts.UnsubscribeHandler(ctx, &req); err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.resourceSubscriptions[req.URI] != nil {
		delete(s.resourceSubscriptions[req.URI], ss)
	}
	s.mu.Unlock()

	return &protocol.EmptyResult{}, nil
}

// handleListPrompts handles the prompts/list request
func (s *Server) handleListPrompts(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.ListPromptsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prompts := make([]protocol.Prompt, 0, len(s.prompts))
	for _, sp := range s.prompts {
		prompts = append(prompts, *sp.prompt)
	}

	return &protocol.ListPromptsResult{
		Prompts: prompts,
	}, nil
}

// handleGetPrompt handles the prompts/get request
func (s *Server) handleGetPrompt(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.GetPromptResult, error) {
	var req protocol.GetPromptParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid get prompt params: %w", err)
	}

	s.mu.Lock()
	sp, exists := s.prompts[req.Name]
	s.mu.Unlock()

	if !exists {
		return nil, fmt.Errorf("prompt not found: %s", req.Name)
	}

	promptReq := &GetPromptRequest{
		Session: ss,
		Params:  &req,
	}

	return sp.handler(ctx, promptReq)
}

// handleComplete handles the completion/complete request
func (s *Server) handleComplete(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.CompleteResult, error) {
	if s.opts.CompletionHandler == nil {
		return nil, fmt.Errorf("completion not supported")
	}

	var req protocol.CompleteRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid complete params: %w", err)
	}

	return s.opts.CompletionHandler(ctx, &req)
}

// handleCancelled handles the notifications/cancelled notification
func (s *Server) handleCancelled(ctx context.Context, ss *ServerSession, params json.RawMessage) error {
	var req protocol.CancelledNotificationParams
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("invalid cancelled params: %w", err)
	}

	requestID := ""
	switch v := req.RequestID.(type) {
	case string:
		requestID = v
	case float64:
		requestID = fmt.Sprintf("%.0f", v)
	case json.Number:
		requestID = v.String()
	default:
		return fmt.Errorf("invalid requestId type: %T", req.RequestID)
	}

	ss.mu.Lock()
	cancel, exists := ss.pendingRequests[requestID]
	ss.mu.Unlock()

	if exists {
		cancel()
	}

	// Return nil even if request doesn't exist, as the request may have already completed
	return nil
}

// handleProgress handles the notifications/progress notification
func (s *Server) handleProgress(ctx context.Context, ss *ServerSession, params json.RawMessage) error {
	if s.opts.ProgressNotificationHandler == nil {
		return nil
	}

	var req protocol.ProgressNotificationParams
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("invalid progress params: %w", err)
	}

	s.opts.ProgressNotificationHandler(ctx, ss, &req)
	return nil
}

// handleRootsListChanged handles the notifications/roots/list_changed notification
func (s *Server) handleRootsListChanged(ctx context.Context, ss *ServerSession, params json.RawMessage) error {
	// Client notifies that the root list has changed, server can choose to re-query
	return nil
}

// handleSetLoggingLevel handles the logging/setLevel request
func (s *Server) handleSetLoggingLevel(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.EmptyResult, error) {
	var req protocol.SetLoggingLevelParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid set level params: %w", err)
	}

	ss.updateState(func(state *ServerSessionState) {
		state.LogLevel = req.Level
	})

	if s.opts.LoggingSetLevelHandler != nil {
		if err := s.opts.LoggingSetLevelHandler(ctx, ss, req.Level); err != nil {
			return nil, err
		}
	}

	return &protocol.EmptyResult{}, nil
}

// HandleMessage implements the SSE Handler interface (for backward compatibility)
func (s *Server) HandleMessage(ctx context.Context, msg *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error) {
	// Create a temporary session (SSE uses the old single session mode)
	ss := &ServerSession{
		server:          s,
		conn:            nil, // SSE does not use connection
		pendingRequests: make(map[string]context.CancelFunc),
	}

	// Handle message
	response := s.handleMessage(ctx, ss, msg)
	return response, nil
}

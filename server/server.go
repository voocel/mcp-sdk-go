package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
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
	tasks                 map[string]*serverTask             // taskId -> task (MCP 2025-11-25)
}

// serverTask represents a task stored in the server (MCP 2025-11-25)
type serverTask struct {
	task     *protocol.Task
	result   any
	rpcError *protocol.JSONRPCError
	cancel   context.CancelFunc
	done     chan struct{}
	doneOnce sync.Once
	sessionID string
}

type ServerOptions struct {
	// Optional client instructions
	Instructions string

	// Initialized handler function
	InitializedHandler func(context.Context, *ServerSession)

	// Progress notification handler function
	ProgressNotificationHandler func(context.Context, *ServerSession, *protocol.ProgressNotificationParams)

	// Elicitation complete notification handler (MCP 2025-11-25)
	ElicitationCompleteHandler func(context.Context, *ServerSession, *protocol.ElicitationCompleteNotificationParams)

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

	// Tasks capability options (MCP 2025-11-25)
	TasksEnabled bool // Enable tasks support

	// TaskGetHandler handles tasks/get requests (MCP 2025-11-25)
	TaskGetHandler func(context.Context, *protocol.GetTaskParams) (*protocol.GetTaskResult, error)

	// TaskListHandler handles tasks/list requests (MCP 2025-11-25)
	TaskListHandler func(context.Context, *protocol.ListTasksParams) (*protocol.ListTasksResult, error)

	// TaskCancelHandler handles tasks/cancel requests (MCP 2025-11-25)
	TaskCancelHandler func(context.Context, *protocol.CancelTaskParams) (*protocol.CancelTaskResult, error)

	// TaskResultHandler handles tasks/result requests (MCP 2025-11-25)
	// Returns the original request's result type (e.g., *CallToolResult)
	TaskResultHandler func(context.Context, *protocol.TaskResultParams) (interface{}, error)
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
		tasks:                 make(map[string]*serverTask),
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

	// Apply middleware
	wrappedHandler := applyMiddleware(h, s.middlewares)

	s.tools[t.Name] = &serverTool{
		tool:    t,
		handler: wrappedHandler,
	}

	sessions := make([]*ServerSession, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	// Notify all sessions that the tool list has changed
	notifyToolListChanged(sessions)
}

func (s *Server) RemoveTool(name string) {
	s.mu.Lock()

	var changed bool
	if _, exists := s.tools[name]; exists {
		delete(s.tools, name)
		changed = true
	}

	sessions := make([]*ServerSession, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	if changed {
		notifyToolListChanged(sessions)
	}
}

func (s *Server) AddResource(r *protocol.Resource, h ResourceHandler) {
	s.mu.Lock()

	s.resources[r.URI] = &serverResource{
		resource: r,
		handler:  h,
	}

	sessions := make([]*ServerSession, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	notifyResourceListChanged(sessions)
}

func (s *Server) RemoveResource(uri string) {
	s.mu.Lock()

	var changed bool
	if _, exists := s.resources[uri]; exists {
		delete(s.resources, uri)
		changed = true
	}

	sessions := make([]*ServerSession, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	if changed {
		notifyResourceListChanged(sessions)
	}
}

func (s *Server) AddResourceTemplate(t *protocol.ResourceTemplate, h ResourceHandler) {
	s.mu.Lock()

	s.resourceTemplates[t.URITemplate] = &serverResourceTemplate{
		template: t,
		handler:  h,
	}

	sessions := make([]*ServerSession, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	notifyResourceListChanged(sessions)
}

func (s *Server) RemoveResourceTemplate(uriTemplate string) {
	s.mu.Lock()

	var changed bool
	if _, exists := s.resourceTemplates[uriTemplate]; exists {
		delete(s.resourceTemplates, uriTemplate)
		changed = true
	}

	sessions := make([]*ServerSession, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	if changed {
		notifyResourceListChanged(sessions)
	}
}

func (s *Server) AddPrompt(p *protocol.Prompt, h PromptHandler) {
	s.mu.Lock()

	s.prompts[p.Name] = &serverPrompt{
		prompt:  p,
		handler: h,
	}

	sessions := make([]*ServerSession, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	notifyPromptListChanged(sessions)
}

func (s *Server) RemovePrompt(name string) {
	s.mu.Lock()

	var changed bool
	if _, exists := s.prompts[name]; exists {
		delete(s.prompts, name)
		changed = true
	}

	sessions := make([]*ServerSession, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	if changed {
		notifyPromptListChanged(sessions)
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

func jsonRPCErrorFrom(err error) *protocol.JSONRPCError {
	if err == nil {
		return nil
	}

	// Preserve MCP error codes when available.
	var mcpErr *protocol.MCPError
	if errors.As(err, &mcpErr) {
		return &protocol.JSONRPCError{
			Code:    mcpErr.Code,
			Message: mcpErr.Message,
			Data:    mcpErr.Data,
		}
	}

	return &protocol.JSONRPCError{
		Code:    protocol.InternalError,
		Message: err.Error(),
	}
}

func relatedTaskMeta(taskID string) map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/related-task": map[string]any{
			"taskId": taskID,
		},
	}
}

func (s *Server) scheduleTaskCleanup(taskID string, ttlMs int) {
	if ttlMs <= 0 {
		return
	}
	time.AfterFunc(time.Duration(ttlMs)*time.Millisecond, func() {
		s.mu.Lock()
		delete(s.tasks, taskID)
		s.mu.Unlock()
	})
}

func mergeMap(dst map[string]any, src map[string]any) map[string]any {
	if dst == nil && src == nil {
		return nil
	}
	if dst == nil {
		dst = make(map[string]any, len(src))
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func isTerminalTaskStatus(status protocol.TaskStatus) bool {
	switch status {
	case protocol.TaskStatusCompleted, protocol.TaskStatusFailed, protocol.TaskStatusCancelled:
		return true
	default:
		return false
	}
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
				Error:   jsonRPCErrorFrom(err),
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

func notifyToolListChanged(sessions []*ServerSession) {
	for _, ss := range sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationToolsListChanged, &protocol.ToolListChangedParams{})
	}
}

func notifyResourceListChanged(sessions []*ServerSession) {
	for _, ss := range sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationResourcesListChanged, &protocol.ResourceListChangedParams{})
	}
}

func notifyPromptListChanged(sessions []*ServerSession) {
	for _, ss := range sessions {
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
	// Tasks methods (MCP 2025-11-25)
	case protocol.MethodTasksGet:
		if !s.opts.TasksEnabled {
			return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": method})
		}
		return s.handleTasksGet(ctx, ss, params)
	case protocol.MethodTasksList:
		if !s.opts.TasksEnabled {
			return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": method})
		}
		return s.handleTasksList(ctx, ss, params)
	case protocol.MethodTasksCancel:
		if !s.opts.TasksEnabled {
			return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": method})
		}
		return s.handleTasksCancel(ctx, ss, params)
	case protocol.MethodTasksResult:
		if !s.opts.TasksEnabled {
			return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": method})
		}
		return s.handleTasksResult(ctx, ss, params)
	default:
		return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": method})
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
	case protocol.NotificationElicitationComplete:
		return s.handleElicitationComplete(ctx, ss, params)
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
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodInitialize})
	}

	// Determine the protocol version to use
	// If client version is supported, use it; otherwise use server's latest version
	negotiatedVersion := req.ProtocolVersion
	if !protocol.IsVersionSupported(req.ProtocolVersion) {
		// Log warning but don't reject - use server's latest version instead
		log.Printf("[MCP] Warning: client requested unsupported protocol version: %s, using server version: %s",
			req.ProtocolVersion, protocol.MCPVersion)
		negotiatedVersion = protocol.MCPVersion
	}

	ss.updateState(func(state *ServerSessionState) {
		state.InitializeParams = &req
	})

	capabilities := protocol.ServerCapabilities{}

	s.mu.Lock()
	hasTools := len(s.tools) > 0
	hasResources := len(s.resources) > 0 || len(s.resourceTemplates) > 0
	hasPrompts := len(s.prompts) > 0
	subscribeSupported := s.opts.SubscribeHandler != nil && s.opts.UnsubscribeHandler != nil

	if hasTools {
		capabilities.Tools = &protocol.ToolsCapability{ListChanged: true}
	}
	if hasResources {
		capabilities.Resources = &protocol.ResourcesCapability{
			ListChanged: true,
			Subscribe:   subscribeSupported,
		}
	}
	if hasPrompts {
		capabilities.Prompts = &protocol.PromptsCapability{ListChanged: true}
	}
	s.mu.Unlock()

	capabilities.Logging = &protocol.LoggingCapability{}

	if s.opts.CompletionHandler != nil {
		capabilities.Completion = &protocol.CompletionCapability{}
	}

	// Add Tasks capability (MCP 2025-11-25)
	if s.opts.TasksEnabled {
		capabilities.Tasks = &protocol.TasksCapability{}
		// Default implementations exist, so these are always available when TasksEnabled.
		capabilities.Tasks.List = &struct{}{}
		capabilities.Tasks.Cancel = &struct{}{}

		// Server supports task augmentation for tools/call (per-tool allow/deny is negotiated via tool.execution.taskSupport).
		capabilities.Tasks.Requests = &protocol.ServerTaskRequestsCapability{
			Tools: &protocol.ToolsTaskCapability{
				Call: &struct{}{},
			},
		}
	}

	return &protocol.InitializeResult{
		ProtocolVersion: negotiatedVersion,
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
func (s *Server) handleCallTool(ctx context.Context, ss *ServerSession, params json.RawMessage) (interface{}, error) {
	var req protocol.CallToolParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodToolsCall})
	}

	s.mu.Lock()
	st, exists := s.tools[req.Name]
	s.mu.Unlock()

	if !exists {
		return nil, protocol.NewMCPError(protocol.InvalidParams, fmt.Sprintf("Unknown tool: %s", req.Name), nil)
	}

	var taskSupport protocol.TaskSupport
	if st.tool.Execution != nil {
		taskSupport = st.tool.Execution.TaskSupport
	}

	// Tool-level task negotiation (MCP 2025-11-25)
	//
	// Default behavior: task augmentation is forbidden unless explicitly enabled.
	if req.Task != nil {
		if !s.opts.TasksEnabled {
			return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": protocol.MethodToolsCall})
		}
		// If taskSupport is not present or forbidden, servers SHOULD return -32601.
		if st.tool.Execution == nil || taskSupport == "" || taskSupport == protocol.TaskSupportForbidden {
			return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": protocol.MethodToolsCall})
		}
	} else {
		// If taskSupport is required, servers MUST return -32601 if client does not attempt task augmentation.
		if taskSupport == protocol.TaskSupportRequired {
			return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": protocol.MethodToolsCall})
		}
	}

	// Task-augmented tools/call (MCP 2025-11-25)
	if req.Task != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		taskID := uuid.NewString()
		task := &protocol.Task{
			TaskID:        taskID,
			Status:        protocol.TaskStatusWorking,
			CreatedAt:     now,
			LastUpdatedAt: now,
			TTL:           req.Task.TTL,
		}

		taskCtx, cancel := context.WithCancel(context.Background())
		taskCtx = contextWithTaskID(taskCtx, taskID)
		meta := relatedTaskMeta(taskID)

		s.mu.Lock()
		s.tasks[taskID] = &serverTask{
			task:     task,
			result:   nil,
			rpcError: nil,
			cancel:   cancel,
			done:     make(chan struct{}),
			sessionID: ss.ID(),
		}
		s.mu.Unlock()

		s.NotifyTaskStatus(task)

		toolReq := &CallToolRequest{
			Session: ss,
			Params:  &req,
		}

		go func() {
			defer cancel()
			result, err := st.handler(taskCtx, toolReq)

			s.mu.Lock()
			stored := s.tasks[taskID]
			if stored == nil || stored.task == nil {
				s.mu.Unlock()
				return
			}

			// Once cancelled, task MUST remain cancelled even if execution continues.
			if stored.task.Status == protocol.TaskStatusCancelled {
				stored.doneOnce.Do(func() {
					if stored.done != nil {
						close(stored.done)
					}
				})
				s.mu.Unlock()
				return
			}

			if err != nil {
				stored.rpcError = jsonRPCErrorFrom(err)
				stored.result = nil
				stored.task.Status = protocol.TaskStatusFailed
				stored.task.StatusMessage = err.Error()
				stored.task.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
				stored.doneOnce.Do(func() {
					if stored.done != nil {
						close(stored.done)
					}
				})
				taskCopy := *stored.task
				ttl := stored.task.TTL
				s.mu.Unlock()
				s.NotifyTaskStatus(&taskCopy)
				if ttl != nil {
					s.scheduleTaskCleanup(taskID, *ttl)
				}
				return
			}

			// Per spec: tool result with isError=true should lead to failed task status.
			if result != nil {
				result.Meta = mergeMap(result.Meta, meta)
			}
			stored.result = result
			stored.rpcError = nil
			if result != nil && result.IsError {
				stored.task.Status = protocol.TaskStatusFailed
			} else {
				stored.task.Status = protocol.TaskStatusCompleted
			}
			stored.task.StatusMessage = ""
			stored.task.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			stored.doneOnce.Do(func() {
				if stored.done != nil {
					close(stored.done)
				}
			})
			taskCopy := *stored.task
			ttl := stored.task.TTL
			s.mu.Unlock()

			s.NotifyTaskStatus(&taskCopy)
			if ttl != nil {
				s.scheduleTaskCleanup(taskID, *ttl)
			}
		}()

		return &protocol.CreateTaskResult{Meta: meta, Task: *task}, nil
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
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodResourcesRead})
	}

	s.mu.Lock()
	sr, exists := s.resources[req.URI]
	s.mu.Unlock()

	if !exists {
		return nil, protocol.NewMCPError(protocol.ResourceNotFound, "resource not found", map[string]any{"uri": req.URI})
	}

	resourceReq := &ReadResourceRequest{
		Session: ss,
		Params:  &req,
	}

	return sr.handler(ctx, resourceReq)
}

// handleSubscribe handles the resources/subscribe request
func (s *Server) handleSubscribe(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.EmptyResult, error) {
	if s.opts.SubscribeHandler == nil || s.opts.UnsubscribeHandler == nil {
		return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": protocol.MethodResourcesSubscribe})
	}

	var req protocol.SubscribeParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodResourcesSubscribe})
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
	if s.opts.SubscribeHandler == nil || s.opts.UnsubscribeHandler == nil {
		return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": protocol.MethodResourcesUnsubscribe})
	}

	var req protocol.UnsubscribeParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodResourcesUnsubscribe})
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
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodPromptsGet})
	}

	s.mu.Lock()
	sp, exists := s.prompts[req.Name]
	s.mu.Unlock()

	if !exists {
		return nil, protocol.NewMCPError(protocol.PromptNotFound, "prompt not found", map[string]any{"name": req.Name})
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
		return nil, protocol.NewMCPError(protocol.MethodNotFound, "Method not found", map[string]any{"method": protocol.MethodCompletionComplete})
	}

	var req protocol.CompleteRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodCompletionComplete})
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

// handleElicitationComplete handles notifications/elicitation/complete (MCP 2025-11-25)
func (s *Server) handleElicitationComplete(ctx context.Context, ss *ServerSession, params json.RawMessage) error {
	if s.opts.ElicitationCompleteHandler == nil {
		return nil
	}

	var req protocol.ElicitationCompleteNotificationParams
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("invalid elicitation complete params: %w", err)
	}

	s.opts.ElicitationCompleteHandler(ctx, ss, &req)
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
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodLoggingSetLevel})
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

// handleTasksGet handles the tasks/get request (MCP 2025-11-25)
func (s *Server) handleTasksGet(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.GetTaskResult, error) {
	var req protocol.GetTaskParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodTasksGet})
	}

	if s.opts.TaskGetHandler != nil {
		return s.opts.TaskGetHandler(ctx, &req)
	}

	// Default implementation: look up task in internal storage
	s.mu.Lock()
	st, exists := s.tasks[req.TaskID]
	s.mu.Unlock()

	if !exists {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}
	if !ss.sameSession(st.sessionID) {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}

	return &protocol.GetTaskResult{
		Task: *st.task,
	}, nil
}

// handleTasksList handles the tasks/list request (MCP 2025-11-25)
func (s *Server) handleTasksList(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.ListTasksResult, error) {
	var req protocol.ListTasksParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodTasksList})
	}

	if s.opts.TaskListHandler != nil {
		return s.opts.TaskListHandler(ctx, &req)
	}

	// Default implementation: return all tasks from internal storage
	s.mu.Lock()
	tasks := make([]protocol.Task, 0, len(s.tasks))
	for _, st := range s.tasks {
		if st == nil || st.task == nil {
			continue
		}
		if !ss.sameSession(st.sessionID) {
			continue
		}
		tasks = append(tasks, *st.task)
	}
	s.mu.Unlock()

	return &protocol.ListTasksResult{
		Tasks: tasks,
	}, nil
}

// handleTasksCancel handles the tasks/cancel request (MCP 2025-11-25)
func (s *Server) handleTasksCancel(ctx context.Context, ss *ServerSession, params json.RawMessage) (*protocol.CancelTaskResult, error) {
	var req protocol.CancelTaskParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodTasksCancel})
	}

	if s.opts.TaskCancelHandler != nil {
		return s.opts.TaskCancelHandler(ctx, &req)
	}

	s.mu.Lock()
	st := s.tasks[req.TaskID]
	if st == nil || st.task == nil {
		s.mu.Unlock()
		return nil, protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}
	if !ss.sameSession(st.sessionID) {
		s.mu.Unlock()
		return nil, protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}
	if isTerminalTaskStatus(st.task.Status) {
		s.mu.Unlock()
		return nil, protocol.NewMCPError(protocol.InvalidParams, fmt.Sprintf("Cannot cancel task: already in terminal status %q", st.task.Status), nil)
	}

	st.task.Status = protocol.TaskStatusCancelled
	st.task.StatusMessage = req.Reason
	st.task.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	st.result = nil
	// tasks/result returns the original request's JSON-RPC error when a task is cancelled.
	// MCP does not define a dedicated cancellation error code, so we use -32603 with a stable message.
	data := map[string]any(nil)
	if req.Reason != "" {
		data = map[string]any{"reason": req.Reason}
	}
	st.rpcError = &protocol.JSONRPCError{
		Code:    protocol.InternalError,
		Message: "Request cancelled",
		Data:    data,
	}
	st.doneOnce.Do(func() {
		if st.done != nil {
			close(st.done)
		}
	})
	cancel := st.cancel
	ttl := st.task.TTL
	taskCopy := *st.task
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.NotifyTaskStatus(&taskCopy)
	if ttl != nil {
		s.scheduleTaskCleanup(req.TaskID, *ttl)
	}

	return &protocol.CancelTaskResult{Task: taskCopy}, nil
}

// handleTasksResult handles the tasks/result request (MCP 2025-11-25)
// Per spec, this returns the original request's result type directly
func (s *Server) handleTasksResult(ctx context.Context, ss *ServerSession, params json.RawMessage) (interface{}, error) {
	var req protocol.TaskResultParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{"method": protocol.MethodTasksResult})
	}

	if s.opts.TaskResultHandler != nil {
		return s.opts.TaskResultHandler(ctx, &req)
	}

	// Default implementation: return task result from internal storage
	s.mu.Lock()
	st := s.tasks[req.TaskID]
	if st == nil || st.task == nil {
		s.mu.Unlock()
		return nil, protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}
	if !ss.sameSession(st.sessionID) {
		s.mu.Unlock()
		return nil, protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}
	status := st.task.Status
	done := st.done
	s.mu.Unlock()

	// Must block until terminal status.
	if !isTerminalTaskStatus(status) {
		if done == nil {
			return nil, protocol.NewMCPError(protocol.InternalError, "task result not available", nil)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
		}
	}

	s.mu.Lock()
	st = s.tasks[req.TaskID]
	if st == nil || st.task == nil {
		s.mu.Unlock()
		return nil, protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}
	if !ss.sameSession(st.sessionID) {
		s.mu.Unlock()
		return nil, protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}
	taskID := st.task.TaskID
	rpcErr := st.rpcError
	result := st.result
	s.mu.Unlock()

	meta := relatedTaskMeta(taskID)

	// If the original request would have produced a JSON-RPC error, return it here.
	if rpcErr != nil {
		data := map[string]any{"_meta": meta}
		if existing, ok := rpcErr.Data.(map[string]any); ok {
			data = mergeMap(data, existing)
		}
		return nil, protocol.NewMCPError(rpcErr.Code, rpcErr.Message, data)
	}

	// For tools/call, ensure the returned result carries related-task metadata.
	if ctr, ok := result.(*protocol.CallToolResult); ok && ctr != nil {
		ctr.Meta = mergeMap(ctr.Meta, meta)
		return ctr, nil
	}
	if result == nil {
		return nil, protocol.NewMCPError(protocol.InternalError, "task result missing", map[string]any{"_meta": meta})
	}
	return result, nil
}

// StoreTask stores a task in the server's internal storage (MCP 2025-11-25)
func (s *Server) StoreTask(task *protocol.Task, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := &serverTask{
		task:     task,
		result:   result,
		rpcError: nil,
		cancel:   nil,
		done:     make(chan struct{}),
	}
	if task != nil && isTerminalTaskStatus(task.Status) {
		st.doneOnce.Do(func() { close(st.done) })
	}
	s.tasks[task.TaskID] = st

	if task != nil && task.TTL != nil && isTerminalTaskStatus(task.Status) {
		s.scheduleTaskCleanup(task.TaskID, *task.TTL)
	}
}

// UpdateTask updates a task in the server's internal storage (MCP 2025-11-25)
func (s *Server) UpdateTask(taskID string, status protocol.TaskStatus, statusMessage string) error {
	s.mu.Lock()

	st, exists := s.tasks[taskID]
	if !exists {
		s.mu.Unlock()
		return protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}

	st.task.Status = status
	st.task.StatusMessage = statusMessage
	st.task.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	shouldCleanup := false
	ttl := (*int)(nil)
	if isTerminalTaskStatus(status) {
		st.doneOnce.Do(func() {
			if st.done != nil {
				close(st.done)
			}
		})
		ttl = st.task.TTL
		shouldCleanup = ttl != nil
	}
	s.mu.Unlock()
	if shouldCleanup {
		s.scheduleTaskCleanup(taskID, *ttl)
		return nil
	}
	return nil
}

// SetTaskResult sets the result for a task (MCP 2025-11-25)
func (s *Server) SetTaskResult(taskID string, result any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, exists := s.tasks[taskID]
	if !exists {
		return protocol.NewMCPError(protocol.InvalidParams, "task not found", nil)
	}

	st.result = result
	return nil
}

// RemoveTask removes a task from the server's internal storage (MCP 2025-11-25)
func (s *Server) RemoveTask(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, taskID)
}

// NotifyTaskStatus sends a task status notification to all sessions (MCP 2025-11-25)
func (s *Server) NotifyTaskStatus(task *protocol.Task) {
	params := &protocol.TaskStatusNotificationParams{
		Task: *task,
	}
	s.mu.Lock()
	sessions := make([]*ServerSession, len(s.sessions))
	copy(sessions, s.sessions)
	s.mu.Unlock()

	for _, ss := range sessions {
		if ss.conn != nil {
			_ = ss.conn.SendNotification(context.Background(), protocol.NotificationTasksStatus, params)
		}
	}
}

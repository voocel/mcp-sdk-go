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

// Server MCP服务器实例, 可以服务一个或多个 MCP 会话
type Server struct {
	impl *protocol.ServerInfo
	opts ServerOptions

	mu                    sync.Mutex
	middlewares           []Middleware // 中间件链
	tools                 map[string]*serverTool
	resources             map[string]*serverResource
	resourceTemplates     map[string]*serverResourceTemplate
	prompts               map[string]*serverPrompt
	sessions              []*ServerSession
	resourceSubscriptions map[string]map[*ServerSession]bool // uri -> session -> bool
}

type ServerOptions struct {
	// 可选的客户端指令
	Instructions string

	// 初始化处理函数
	InitializedHandler func(context.Context, *ServerSession)

	// 进度通知处理函数
	ProgressNotificationHandler func(context.Context, *ServerSession, *protocol.ProgressNotificationParams)

	// 补全处理函数
	CompletionHandler func(context.Context, *protocol.CompleteRequest) (*protocol.CompleteResult, error)

	// 日志级别设置处理函数
	LoggingSetLevelHandler func(context.Context, *ServerSession, protocol.LoggingLevel) error

	// 资源订阅/取消订阅处理函数
	SubscribeHandler   func(context.Context, *protocol.SubscribeParams) error
	UnsubscribeHandler func(context.Context, *protocol.UnsubscribeParams) error

	// KeepAlive 定义定期 "ping" 请求的间隔
	// 如果对等方未能响应 keepalive 检查发起的 ping,会话将自动关闭
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

// AddTool 添加工具到服务器，或替换同名工具（低级 API）。
// Tool 参数在调用后不得修改。
//
// 工具的输入 schema 必须非 nil 且类型为 "object"。对于不接受输入的工具，
// 或接受任何输入的工具，将 [Tool.InputSchema] 设置为 `{"type": "object"}`，
// 使用你喜欢的库或 `json.RawMessage`。
//
// 如果存在 [Tool.OutputSchema]，它也必须类型为 "object"。
//
// 当处理函数作为 CallTool 请求的一部分被调用时，req.Params.Arguments
// 将是 json.RawMessage。
//
// 反序列化参数并根据输入 schema 验证它们是调用者的责任。
//
// 根据输出 schema（如有）验证结果是调用者的责任。
//
// 设置结果的 Content、StructuredContent 和 IsError 字段是调用者的责任。
//
// 大多数用户应该使用顶级函数 [AddTool]，它会处理所有这些责任。
func (s *Server) AddTool(t *protocol.Tool, h ToolHandler) {
	if t.InputSchema == nil {
		panic(fmt.Errorf("AddTool %q: missing input schema", t.Name))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 应用中间件
	wrappedHandler := applyMiddleware(h, s.middlewares)

	s.tools[t.Name] = &serverTool{
		tool:    t,
		handler: wrappedHandler,
	}

	// 通知所有会话工具列表已更改
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

// Run 在给定的 transport 上运行服务器
// 这是一个便捷方法,用于处理单个会话(或一次一个会话)
//
// Run 会阻塞直到客户端终止连接或提供的 context 被取消
// 如果 context 被取消,Run 会关闭连接
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
		<-ssClosed // 等待 goroutine 完成
		return ctx.Err()
	case err := <-ssClosed:
		return err
	}
}

// Connect 通过给定的 transport 连接 MCP 服务器并开始处理消息
//
// 它返回一个连接对象,可用于终止连接(使用 Close)或等待客户端终止(使用 Wait)
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

	// 启动消息处理循环
	go func() {
		err := s.handleConnection(ctx, ss, ss.conn)
		ss.waitErr <- err
		close(ss.waitErr)
	}()

	return ss, nil
}

// handleConnection 处理连接的消息循环
func (s *Server) handleConnection(ctx context.Context, ss *ServerSession, conn Connection) error {
	defer func() {
		s.disconnect(ss)
		conn.Close()
	}()

	// 获取底层的 connAdapter 用于处理响应消息
	adapter, ok := conn.(*connAdapter)
	if !ok {
		return fmt.Errorf("invalid connection type")
	}

	for {
		// 显式检查上下文取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := adapter.conn.Read(ctx)
		if err != nil {
			return err
		}

		// 如果是响应消息,路由到 connAdapter
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

// handleMessage 处理单个 JSON-RPC 消息
func (s *Server) handleMessage(ctx context.Context, ss *ServerSession, msg *protocol.JSONRPCMessage) *protocol.JSONRPCMessage {
	if msg.ID != nil {
		// 请求 - 需要响应
		// 创建可取消的 context 并跟踪请求
		requestID := protocol.IDToString(msg.ID)
		requestCtx, cancel := context.WithCancel(ctx)

		ss.mu.Lock()
		ss.pendingRequests[requestID] = cancel
		ss.mu.Unlock()

		// 确保请求完成后清理
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

		// 序列化结果
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
		// 通知 - 不需要响应
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

// notifyToolListChanged 通知所有会话工具列表已更改
func (s *Server) notifyToolListChanged() {
	for _, ss := range s.sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationToolsListChanged, &protocol.ToolListChangedParams{})
	}
}

// notifyResourceListChanged 通知所有会话资源列表已更改
func (s *Server) notifyResourceListChanged() {
	for _, ss := range s.sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationResourcesListChanged, &protocol.ResourceListChangedParams{})
	}
}

// notifyResourceTemplateListChanged 通知所有会话资源模板列表已更改
func (s *Server) notifyResourceTemplateListChanged() {
	for _, ss := range s.sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationResourcesTemplatesListChanged, &protocol.ResourceTemplateListChangedParams{})
	}
}

// notifyPromptListChanged 通知所有会话提示列表已更改
func (s *Server) notifyPromptListChanged() {
	for _, ss := range s.sessions {
		_ = ss.conn.SendNotification(context.Background(), protocol.NotificationPromptsListChanged, &protocol.PromptListChangedParams{})
	}
}

// handleRequest 处理来自客户端的请求
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

// handleNotification 处理来自客户端的通知
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

// handleInitialize 处理 initialize 请求
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

// handleInitialized 处理 initialized 通知
func (s *Server) handleInitialized(ctx context.Context, ss *ServerSession, params json.RawMessage) error {
	var req protocol.InitializedParams
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("invalid initialized params: %w", err)
	}

	ss.updateState(func(state *ServerSessionState) {
		state.InitializedParams = &req
	})

	// 启动 keepalive
	if s.opts.KeepAlive > 0 {
		ss.startKeepalive(s.opts.KeepAlive)
	}

	if s.opts.InitializedHandler != nil {
		s.opts.InitializedHandler(ctx, ss)
	}

	return nil
}

// handleListTools 处理 tools/list 请求
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

// handleCallTool 处理 tools/call 请求
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

// handleListResources 处理 resources/list 请求
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

// handleListResourceTemplates 处理 resources/templates/list 请求
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

// handleReadResource 处理 resources/read 请求
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

// handleSubscribe 处理 resources/subscribe 请求
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

// handleUnsubscribe 处理 resources/unsubscribe 请求
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

// handleListPrompts 处理 prompts/list 请求
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

// handleGetPrompt 处理 prompts/get 请求
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

// handleComplete 处理 completion/complete 请求
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

// handleCancelled 处理 notifications/cancelled 通知
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

	// 即使请求不存在也不返回错误,因为请求可能已经完成
	return nil
}

// handleProgress 处理 notifications/progress 通知
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

// handleRootsListChanged 处理 notifications/roots/list_changed 通知
func (s *Server) handleRootsListChanged(ctx context.Context, ss *ServerSession, params json.RawMessage) error {
	// 客户端通知根目录列表已更改,服务器可以选择重新查询
	return nil
}

// handleSetLoggingLevel 处理 logging/setLevel 请求
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

// HandleMessage 实现 SSE Handler 接口 (用于向后兼容)
func (s *Server) HandleMessage(ctx context.Context, msg *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error) {
	// 创建一个临时的 session (SSE 使用旧的单会话模式)
	ss := &ServerSession{
		server: s,
		conn:   nil, // SSE 不使用 connection
	}

	// 处理消息
	response := s.handleMessage(ctx, ss, msg)
	return response, nil
}

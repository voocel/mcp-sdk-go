package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/utils"
)

type Server interface {
	RegisterTool(name, description string, inputSchema protocol.JSONSchema, handler ToolHandler) error
	UnregisterTool(name string) error

	RegisterResource(uri, name, description, mimeType string, handler ResourceHandler) error
	UnregisterResource(uri string) error

	RegisterPrompt(name, description string, arguments []protocol.PromptArgument, handler PromptHandler) error
	UnregisterPrompt(name string) error

	GetServerInfo() protocol.ServerInfo
	GetCapabilities() protocol.ServerCapabilities

	HandleMessage(ctx context.Context, message *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error)
	SendNotification(method string, params interface{}) error

	RequestRootsList(ctx context.Context) (*protocol.ListRootsResult, error)
}

type ToolHandler func(ctx context.Context, arguments map[string]interface{}) (*protocol.CallToolResult, error)
type ResourceHandler func(ctx context.Context) (*protocol.ReadResourceResult, error)
type PromptHandler func(ctx context.Context, arguments map[string]string) (*protocol.GetPromptResult, error)
type CompletionHandler func(ctx context.Context, ref protocol.CompletionReference, argument protocol.CompletionArgument, context *protocol.CompletionContext) (*protocol.CompletionResult, error)

type ToolHandlerWithElicitation func(ctx *MCPContext, arguments map[string]interface{}) (*protocol.CallToolResult, error)
type ResourceHandlerWithElicitation func(ctx *MCPContext) (*protocol.ReadResourceResult, error)
type PromptHandlerWithElicitation func(ctx *MCPContext, arguments map[string]string) (*protocol.GetPromptResult, error)

type MCPServer struct {
	serverInfo   protocol.ServerInfo
	capabilities protocol.ServerCapabilities

	tools             map[string]*ToolRegistration
	resources         map[string]*ResourceRegistration
	resourceTemplates map[string]*ResourceTemplateRegistration
	prompts           map[string]*PromptRegistration
	completionHandler CompletionHandler // MCP 2025-06-18: 参数自动补全

	// 资源订阅管理
	resourceSubscriptions map[string]map[string]bool // uri -> set of session IDs

	initialized bool
	clientInfo  *protocol.ClientInfo

	notificationHandler func(method string, params interface{}) error
	elicitor            Elicitor

	// requestSender Send a request to the client (such as a roots list request)
	requestSender func(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error)

	mu sync.RWMutex
}

type ToolRegistration struct {
	Tool    protocol.Tool
	Handler ToolHandler
}

type ResourceRegistration struct {
	Resource protocol.Resource
	Handler  ResourceHandler
}

type ResourceTemplateRegistration struct {
	Template protocol.ResourceTemplate
}

type PromptRegistration struct {
	Prompt  protocol.Prompt
	Handler PromptHandler
}

// NewServer creates MCP server
func NewServer(name, version string) *MCPServer {
	return &MCPServer{
		serverInfo: protocol.ServerInfo{
			Name:    name,
			Version: version,
		},
		capabilities: protocol.ServerCapabilities{
			Tools:     &protocol.ToolsCapability{ListChanged: true},
			Resources: &protocol.ResourcesCapability{ListChanged: true, Subscribe: true, Templates: true},
			Prompts:   &protocol.PromptsCapability{ListChanged: true},
		},
		tools:                 make(map[string]*ToolRegistration),
		resources:             make(map[string]*ResourceRegistration),
		resourceTemplates:     make(map[string]*ResourceTemplateRegistration),
		prompts:               make(map[string]*PromptRegistration),
		resourceSubscriptions: make(map[string]map[string]bool),
	}
}

// ToolOptions tool registration options
type ToolOptions struct {
	Title        string              // optional human-friendly title (MCP 2025-06-18)
	OutputSchema protocol.JSONSchema // optional output schema (MCP 2025-06-18)
	Meta         map[string]any      // optional metadata (MCP 2025-06-18)
}

// RegisterTool registers tool with optional output schema support
func (s *MCPServer) RegisterTool(name, description string, inputSchema protocol.JSONSchema, handler ToolHandler, opts ...ToolOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var title string
	var outputSchema protocol.JSONSchema
	var meta map[string]any
	if len(opts) > 0 {
		if opts[0].Title != "" {
			title = opts[0].Title
		}
		if len(opts[0].OutputSchema) > 0 {
			outputSchema = opts[0].OutputSchema
		}
		if len(opts[0].Meta) > 0 {
			meta = opts[0].Meta
		}
	}

	var tool protocol.Tool
	if len(outputSchema) > 0 {
		tool = protocol.NewToolWithOutput(name, description, inputSchema, outputSchema)
	} else {
		tool = protocol.NewTool(name, description, inputSchema)
	}

	if title != "" {
		tool.Title = title
	}

	if len(meta) > 0 {
		tool.Meta = meta
	}

	s.tools[name] = &ToolRegistration{
		Tool:    tool,
		Handler: handler,
	}

	// send change notification
	if s.initialized {
		go s.SendNotification("notifications/tools/list_changed", &protocol.ToolsListChangedNotification{})
	}

	return nil
}

// UnregisterTool unregisters tool
func (s *MCPServer) UnregisterTool(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tools, name)

	// send change notification
	if s.initialized {
		go s.SendNotification("notifications/tools/list_changed", &protocol.ToolsListChangedNotification{})
	}

	return nil
}

// RegisterResource registers resource
func (s *MCPServer) RegisterResource(uri, name, description, mimeType string, handler ResourceHandler, meta ...map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	resource := protocol.NewResource(uri, name, description, mimeType)

	if len(meta) > 0 && len(meta[0]) > 0 {
		resource.Meta = meta[0]
	}

	s.resources[uri] = &ResourceRegistration{
		Resource: resource,
		Handler:  handler,
	}

	// send change notification
	if s.initialized {
		go s.SendNotification("notifications/resources/list_changed", &protocol.ResourcesListChangedNotification{})
	}

	return nil
}

// RegisterResourceTemplate registers a resource template
func (s *MCPServer) RegisterResourceTemplate(uriTemplate, name, description, mimeType string, meta ...map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	template := protocol.NewResourceTemplate(uriTemplate, name, description, mimeType)

	if len(meta) > 0 && len(meta[0]) > 0 {
		template.Meta = meta[0]
	}

	s.resourceTemplates[uriTemplate] = &ResourceTemplateRegistration{
		Template: template,
	}

	if s.initialized {
		go s.SendNotification("notifications/resources/templates/list_changed", &protocol.ResourceTemplatesListChangedNotification{})
	}

	return nil
}

// UnregisterResourceTemplate unregisters a resource template
func (s *MCPServer) UnregisterResourceTemplate(uriTemplate string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.resourceTemplates, uriTemplate)

	if s.initialized {
		go s.SendNotification("notifications/resources/templates/list_changed", &protocol.ResourceTemplatesListChangedNotification{})
	}

	return nil
}

// UnregisterResource unregisters resource
func (s *MCPServer) UnregisterResource(uri string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.resources, uri)

	// send change notification
	if s.initialized {
		go s.SendNotification("notifications/resources/list_changed", &protocol.ResourcesListChangedNotification{})
	}

	return nil
}

// PromptOptions prompt registration options
type PromptOptions struct {
	Title string         // optional human-friendly title (MCP 2025-06-18)
	Meta  map[string]any // optional metadata (MCP 2025-06-18)
}

// RegisterPrompt registers prompt template
func (s *MCPServer) RegisterPrompt(name, description string, arguments []protocol.PromptArgument, handler PromptHandler, opts ...PromptOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prompt := protocol.NewPrompt(name, description, arguments...)

	if len(opts) > 0 {
		if opts[0].Title != "" {
			prompt.Title = opts[0].Title
		}
		if len(opts[0].Meta) > 0 {
			prompt.Meta = opts[0].Meta
		}
	}

	s.prompts[name] = &PromptRegistration{
		Prompt:  prompt,
		Handler: handler,
	}

	// send change notification
	if s.initialized {
		go s.SendNotification("notifications/prompts/list_changed", &protocol.PromptsListChangedNotification{})
	}

	return nil
}

// UnregisterPrompt unregisters prompt template
func (s *MCPServer) UnregisterPrompt(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.prompts, name)

	// send change notification
	if s.initialized {
		go s.SendNotification("notifications/prompts/list_changed", &protocol.PromptsListChangedNotification{})
	}

	return nil
}

// RegisterCompletionHandler registers completion handler (MCP 2025-06-18)
func (s *MCPServer) RegisterCompletionHandler(handler CompletionHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.completionHandler = handler

	// 启用 completion 能力
	if s.capabilities.Completion == nil {
		s.capabilities.Completion = &protocol.CompletionCapability{}
	}
}

// UnregisterCompletionHandler unregisters completion handler
func (s *MCPServer) UnregisterCompletionHandler() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.completionHandler = nil
	s.capabilities.Completion = nil
}

// GetServerInfo gets server information
func (s *MCPServer) GetServerInfo() protocol.ServerInfo {
	return s.serverInfo
}

// GetCapabilities gets server capabilities
func (s *MCPServer) GetCapabilities() protocol.ServerCapabilities {
	return s.capabilities
}

// SetElicitor sets elicitation handler
func (s *MCPServer) SetElicitor(elicitor Elicitor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.elicitor = elicitor
}

// CreateMCPContext creates MCP context with elicitation support
func (s *MCPServer) CreateMCPContext(ctx context.Context) *MCPContext {
	return NewMCPContext(ctx, s, s.elicitor)
}

// SetNotificationHandler sets notification handler
func (s *MCPServer) SetNotificationHandler(handler func(method string, params interface{}) error) {
	s.notificationHandler = handler
}

// SetRequestSender sets request sender for server-initiated requests
func (s *MCPServer) SetRequestSender(sender func(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestSender = sender
}

// SendNotification sends notification
func (s *MCPServer) SendNotification(method string, params interface{}) error {
	if s.notificationHandler != nil {
		return s.notificationHandler(method, params)
	}
	return nil
}

func (s *MCPServer) NotifyProgress(progressToken any, progress, total float64, message string) error {
	params := protocol.ProgressNotificationParams{
		ProgressToken: progressToken,
		Progress:      progress,
		Total:         total,
		Message:       message,
	}
	return s.SendNotification("notifications/progress", params)
}

func (s *MCPServer) NotifyCancelled(requestID any, reason string) error {
	params := protocol.CancelledNotificationParams{
		RequestID: requestID,
		Reason:    reason,
	}
	return s.SendNotification("notifications/cancelled", params)
}

// HandleMessage handles JSON-RPC messages
func (s *MCPServer) HandleMessage(ctx context.Context, message *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error) {
	if err := utils.ValidateJSONRPCMessage(message); err != nil {
		// if it's a notification message, don't return error response
		if message.ID == nil {
			return nil, err
		}
		return utils.NewJSONRPCError("", protocol.InvalidRequest, err.Error(), nil)
	}

	// handle request or notification
	if message.Method != "" {
		return s.handleRequest(ctx, message)
	}

	return nil, fmt.Errorf("unsupported message type")
}

// handle request
func (s *MCPServer) handleRequest(ctx context.Context, request *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error) {
	var result interface{}
	var err error

	switch request.Method {
	case "initialize":
		result, err = s.handleInitialize(ctx, request.Params)
	case "notifications/initialized":
		// handle initialization completed notification
		err = s.handleInitialized(ctx, request.Params)
		if err == nil {
			return nil, nil // notification messages don't need response
		}
	case "tools/list":
		result, err = s.handleListTools(ctx, request.Params)
	case "tools/call":
		result, err = s.handleCallTool(ctx, request.Params)
	case "resources/list":
		result, err = s.handleListResources(ctx, request.Params)
	case "resources/read":
		result, err = s.handleReadResource(ctx, request.Params)
	case "resources/templates/list":
		result, err = s.handleListResourceTemplates(ctx, request.Params)
	case "resources/subscribe":
		result, err = s.handleSubscribe(ctx, request.Params)
	case "resources/unsubscribe":
		result, err = s.handleUnsubscribe(ctx, request.Params)
	case "prompts/list":
		result, err = s.handleListPrompts(ctx, request.Params)
	case "prompts/get":
		result, err = s.handleGetPrompt(ctx, request.Params)
	case "completion/complete":
		result, err = s.handleComplete(ctx, request.Params)
	case "ping":
		result, err = s.handlePing(ctx, request.Params)
	default:
		// for notification messages, don't return error response
		if request.IsNotification() {
			return nil, fmt.Errorf("unknown notification method: %s", request.Method)
		}
		return utils.NewJSONRPCError(request.GetIDString(), protocol.MethodNotFound,
			fmt.Sprintf("method not found: %s", request.Method), nil)
	}

	// if it's a notification message, don't return response
	if request.IsNotification() {
		if err != nil {
			// for notification message errors, only log, don't return response
			return nil, err
		}
		return nil, nil
	}

	// for request messages, return response
	if err != nil {
		return utils.NewJSONRPCError(request.GetIDString(), protocol.InternalError, err.Error(), nil)
	}

	return utils.NewJSONRPCResponse(request.GetIDString(), result)
}

// initialization request
func (s *MCPServer) handleInitialize(ctx context.Context, params json.RawMessage) (*protocol.InitializeResult, error) {
	var req protocol.InitializeRequest
	if params != nil {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid initialize params: %w", err)
		}
	}

	// check protocol version compatibility and select appropriate version
	if !protocol.IsVersionSupported(req.ProtocolVersion) {
		return nil, fmt.Errorf("unsupported protocol version: %s, supported versions: %v",
			req.ProtocolVersion, protocol.GetSupportedVersions())
	}

	// use the version requested by client
	negotiatedVersion := req.ProtocolVersion

	s.mu.Lock()
	s.initialized = true
	s.clientInfo = &req.ClientInfo
	s.mu.Unlock()

	return &protocol.InitializeResult{
		ProtocolVersion: negotiatedVersion,
		Capabilities:    s.capabilities,
		ServerInfo:      s.serverInfo,
	}, nil
}

// handle initialization completed notification
func (s *MCPServer) handleInitialized(ctx context.Context, params json.RawMessage) error {
	// initialization completed notification, client indicates ready to receive notifications
	return nil
}

// tool list
func (s *MCPServer) handleListTools(ctx context.Context, params json.RawMessage) (*protocol.ListToolsResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]protocol.Tool, 0, len(s.tools))
	for _, reg := range s.tools {
		tools = append(tools, reg.Tool)
	}

	return &protocol.ListToolsResult{
		Tools: tools,
	}, nil
}

// tool call
func (s *MCPServer) handleCallTool(ctx context.Context, params json.RawMessage) (*protocol.CallToolResult, error) {
	var req protocol.CallToolParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid call tool params: %w", err)
	}

	s.mu.RLock()
	registration, exists := s.tools[req.Name]
	s.mu.RUnlock()

	if !exists {
		return protocol.NewToolResultError(fmt.Sprintf("tool not found: %s", req.Name)), nil
	}

	return registration.Handler(ctx, req.Arguments)
}

// RequestRootsList root list
func (s *MCPServer) RequestRootsList(ctx context.Context) (*protocol.ListRootsResult, error) {
	s.mu.RLock()
	sender := s.requestSender
	s.mu.RUnlock()

	if sender == nil {
		return nil, fmt.Errorf("request sender not configured")
	}

	resp, err := sender(ctx, "roots/list", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to send roots/list request: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("roots/list request failed: %s", resp.Error.Message)
	}

	var result protocol.ListRootsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal roots/list result: %w", err)
	}

	return &result, nil
}

// resource list
func (s *MCPServer) handleListResources(ctx context.Context, params json.RawMessage) (*protocol.ListResourcesResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resources := make([]protocol.Resource, 0, len(s.resources))
	for _, reg := range s.resources {
		resources = append(resources, reg.Resource)
	}

	return &protocol.ListResourcesResult{
		Resources: resources,
	}, nil
}

// resource template list
func (s *MCPServer) handleListResourceTemplates(ctx context.Context, params json.RawMessage) (*protocol.ListResourceTemplatesResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	templates := make([]protocol.ResourceTemplate, 0, len(s.resourceTemplates))
	for _, reg := range s.resourceTemplates {
		templates = append(templates, reg.Template)
	}

	return &protocol.ListResourceTemplatesResult{
		ResourceTemplates: templates,
	}, nil
}

// resource read
func (s *MCPServer) handleReadResource(ctx context.Context, params json.RawMessage) (*protocol.ReadResourceResult, error) {
	var req protocol.ReadResourceParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid read resource params: %w", err)
	}

	s.mu.RLock()
	registration, exists := s.resources[req.URI]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("resource not found: %s", req.URI)
	}

	return registration.Handler(ctx)
}

// handleSubscribe 处理资源订阅请求
func (s *MCPServer) handleSubscribe(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req protocol.SubscribeParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid subscribe params: %w", err)
	}

	s.mu.RLock()
	_, exists := s.resources[req.URI]
	s.mu.RUnlock()

	if !exists {
		return nil, &protocol.MCPError{
			Code:    protocol.ResourceNotFound,
			Message: fmt.Sprintf("resource not found: %s", req.URI),
		}
	}

	// 添加订阅 (这里使用空字符串作为 session ID,实际应用中应该从 context 获取)
	sessionID := getSessionIDFromContext(ctx)

	s.mu.Lock()
	if s.resourceSubscriptions[req.URI] == nil {
		s.resourceSubscriptions[req.URI] = make(map[string]bool)
	}
	s.resourceSubscriptions[req.URI][sessionID] = true
	s.mu.Unlock()

	return struct{}{}, nil
}

// handleUnsubscribe 处理取消资源订阅请求
func (s *MCPServer) handleUnsubscribe(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req protocol.UnsubscribeParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid unsubscribe params: %w", err)
	}

	sessionID := getSessionIDFromContext(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	if subscribers, ok := s.resourceSubscriptions[req.URI]; ok {
		delete(subscribers, sessionID)
		if len(subscribers) == 0 {
			delete(s.resourceSubscriptions, req.URI)
		}
	}

	return struct{}{}, nil
}

// NotifyResourceUpdated 通知所有订阅者资源已更新
func (s *MCPServer) NotifyResourceUpdated(uri string) error {
	s.mu.RLock()
	subscribers := s.resourceSubscriptions[uri]
	if len(subscribers) == 0 {
		s.mu.RUnlock()
		return nil
	}

	// 复制订阅者列表,避免持有锁时间过长
	sessionIDs := make([]string, 0, len(subscribers))
	for sessionID := range subscribers {
		sessionIDs = append(sessionIDs, sessionID)
	}
	s.mu.RUnlock()

	// 发送通知给所有订阅者
	params := protocol.ResourceUpdatedNotificationParams{
		URI: uri,
	}

	if s.notificationHandler != nil {
		return s.notificationHandler("notifications/resources/updated", params)
	}

	return nil
}

// getSessionIDFromContext 从 context 获取 session ID
// 这是一个辅助函数,实际应用中应该从传输层的 context 中获取
func getSessionIDFromContext(ctx context.Context) string {
	// TODO: 从 context 中获取真实的 session ID
	// 这里暂时返回默认值,实际使用时需要传输层支持
	if sessionID, ok := ctx.Value("sessionID").(string); ok {
		return sessionID
	}
	return "default-session"
}

// prompt template list
func (s *MCPServer) handleListPrompts(ctx context.Context, params json.RawMessage) (*protocol.ListPromptsResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prompts := make([]protocol.Prompt, 0, len(s.prompts))
	for _, reg := range s.prompts {
		prompts = append(prompts, reg.Prompt)
	}

	return &protocol.ListPromptsResult{
		Prompts: prompts,
	}, nil
}

// get prompt template
func (s *MCPServer) handleGetPrompt(ctx context.Context, params json.RawMessage) (*protocol.GetPromptResult, error) {
	var req protocol.GetPromptParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid get prompt params: %w", err)
	}

	s.mu.RLock()
	registration, exists := s.prompts[req.Name]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("prompt not found: %s", req.Name)
	}

	return registration.Handler(ctx, req.Arguments)
}

// handle completion request (MCP 2025-06-18)
func (s *MCPServer) handleComplete(ctx context.Context, params json.RawMessage) (*protocol.CompleteResult, error) {
	var req protocol.CompleteRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &protocol.MCPError{
			Code:    -32602,
			Message: "Invalid completion params",
			Data:    err.Error(),
		}
	}

	s.mu.RLock()
	handler := s.completionHandler
	s.mu.RUnlock()

	if handler == nil {
		return nil, &protocol.MCPError{
			Code:    -32601,
			Message: "Completion not supported",
		}
	}

	ref, err := protocol.UnmarshalCompletionReference(req.Ref)
	if err != nil {
		return nil, err
	}

	result, err := handler(ctx, ref, req.Argument, req.Context)
	if err != nil {
		return nil, err
	}

	return &protocol.CompleteResult{
		Completion: *result,
	}, nil
}

func (s *MCPServer) handlePing(ctx context.Context, params json.RawMessage) (interface{}, error) {
	// ping 请求只需要返回空对象即可
	return struct{}{}, nil
}

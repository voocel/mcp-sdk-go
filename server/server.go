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
}

type ToolHandler func(ctx context.Context, arguments map[string]interface{}) (*protocol.CallToolResult, error)
type ResourceHandler func(ctx context.Context) (*protocol.ReadResourceResult, error)
type PromptHandler func(ctx context.Context, arguments map[string]string) (*protocol.GetPromptResult, error)

type MCPServer struct {
	serverInfo   protocol.ServerInfo
	capabilities protocol.ServerCapabilities

	tools     map[string]*ToolRegistration
	resources map[string]*ResourceRegistration
	prompts   map[string]*PromptRegistration

	initialized bool
	clientInfo  *protocol.ClientInfo

	notificationHandler func(method string, params interface{}) error

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

type PromptRegistration struct {
	Prompt  protocol.Prompt
	Handler PromptHandler
}

// NewServer 创建MCP服务端
func NewServer(name, version string) *MCPServer {
	return &MCPServer{
		serverInfo: protocol.ServerInfo{
			Name:    name,
			Version: version,
		},
		capabilities: protocol.ServerCapabilities{
			Tools:     &protocol.ToolsCapability{ListChanged: true},
			Resources: &protocol.ResourcesCapability{ListChanged: true, Subscribe: false},
			Prompts:   &protocol.PromptsCapability{ListChanged: true},
		},
		tools:     make(map[string]*ToolRegistration),
		resources: make(map[string]*ResourceRegistration),
		prompts:   make(map[string]*PromptRegistration),
	}
}

// RegisterTool 注册工具
func (s *MCPServer) RegisterTool(name, description string, inputSchema protocol.JSONSchema, handler ToolHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tool := protocol.NewTool(name, description, inputSchema)
	s.tools[name] = &ToolRegistration{
		Tool:    tool,
		Handler: handler,
	}

	// 发送变更通知
	if s.initialized {
		go s.SendNotification("notifications/tools/list_changed", &protocol.ToolsListChangedNotification{})
	}

	return nil
}

// UnregisterTool 注销工具
func (s *MCPServer) UnregisterTool(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tools, name)

	// 发送变更通知
	if s.initialized {
		go s.SendNotification("notifications/tools/list_changed", &protocol.ToolsListChangedNotification{})
	}

	return nil
}

// RegisterResource 注册资源
func (s *MCPServer) RegisterResource(uri, name, description, mimeType string, handler ResourceHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	resource := protocol.NewResource(uri, name, description, mimeType)
	s.resources[uri] = &ResourceRegistration{
		Resource: resource,
		Handler:  handler,
	}

	// 发送变更通知
	if s.initialized {
		go s.SendNotification("notifications/resources/list_changed", &protocol.ResourcesListChangedNotification{})
	}

	return nil
}

// UnregisterResource 注销资源
func (s *MCPServer) UnregisterResource(uri string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.resources, uri)

	// 发送变更通知
	if s.initialized {
		go s.SendNotification("notifications/resources/list_changed", &protocol.ResourcesListChangedNotification{})
	}

	return nil
}

// RegisterPrompt 注册提示模板
func (s *MCPServer) RegisterPrompt(name, description string, arguments []protocol.PromptArgument, handler PromptHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prompt := protocol.NewPrompt(name, description, arguments...)
	s.prompts[name] = &PromptRegistration{
		Prompt:  prompt,
		Handler: handler,
	}

	// 发送变更通知
	if s.initialized {
		go s.SendNotification("notifications/prompts/list_changed", &protocol.PromptsListChangedNotification{})
	}

	return nil
}

// UnregisterPrompt 注销提示模板
func (s *MCPServer) UnregisterPrompt(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.prompts, name)

	// 发送变更通知
	if s.initialized {
		go s.SendNotification("notifications/prompts/list_changed", &protocol.PromptsListChangedNotification{})
	}

	return nil
}

// GetServerInfo 获取服务器信息
func (s *MCPServer) GetServerInfo() protocol.ServerInfo {
	return s.serverInfo
}

// GetCapabilities 获取服务器能力
func (s *MCPServer) GetCapabilities() protocol.ServerCapabilities {
	return s.capabilities
}

// SetNotificationHandler 设置通知处理器
func (s *MCPServer) SetNotificationHandler(handler func(method string, params interface{}) error) {
	s.notificationHandler = handler
}

// SendNotification 发送通知
func (s *MCPServer) SendNotification(method string, params interface{}) error {
	if s.notificationHandler != nil {
		return s.notificationHandler(method, params)
	}
	return nil
}

// HandleMessage 处理JSON-RPC消息
func (s *MCPServer) HandleMessage(ctx context.Context, message *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error) {
	if err := utils.ValidateJSONRPCMessage(message); err != nil {
		return utils.NewJSONRPCError("", protocol.InvalidRequest, err.Error(), nil)
	}

	// 处理请求
	if message.Method != "" {
		return s.handleRequest(ctx, message)
	}

	return nil, fmt.Errorf("unsupported message type")
}

// 处理请求
func (s *MCPServer) handleRequest(ctx context.Context, request *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error) {
	var result interface{}
	var err error

	switch request.Method {
	case "initialize":
		result, err = s.handleInitialize(ctx, request.Params)
	case "tools/list":
		result, err = s.handleListTools(ctx, request.Params)
	case "tools/call":
		result, err = s.handleCallTool(ctx, request.Params)
	case "resources/list":
		result, err = s.handleListResources(ctx, request.Params)
	case "resources/read":
		result, err = s.handleReadResource(ctx, request.Params)
	case "prompts/list":
		result, err = s.handleListPrompts(ctx, request.Params)
	case "prompts/get":
		result, err = s.handleGetPrompt(ctx, request.Params)
	default:
		return utils.NewJSONRPCError(*request.ID, protocol.MethodNotFound,
			fmt.Sprintf("method not found: %s", request.Method), nil)
	}

	if err != nil {
		return utils.NewJSONRPCError(*request.ID, protocol.InternalError, err.Error(), nil)
	}

	return utils.NewJSONRPCResponse(*request.ID, result)
}

// 初始化请求
func (s *MCPServer) handleInitialize(ctx context.Context, params json.RawMessage) (*protocol.InitializeResult, error) {
	var req protocol.InitializeRequest
	if params != nil {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid initialize params: %w", err)
		}
	}

	// 检查协议版本
	if req.ProtocolVersion != protocol.MCPVersion {
		return nil, fmt.Errorf("unsupported protocol version: %s", req.ProtocolVersion)
	}

	s.mu.Lock()
	s.initialized = true
	s.clientInfo = &req.ClientInfo
	s.mu.Unlock()

	return &protocol.InitializeResult{
		ProtocolVersion: protocol.MCPVersion,
		Capabilities:    s.capabilities,
		ServerInfo:      s.serverInfo,
	}, nil
}

// 工具列表
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

// 工具调用
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

// 资源列表
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

// 资源读取
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

// 提示模板列表
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

// 获取提示模板
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

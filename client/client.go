package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
	"github.com/voocel/mcp-sdk-go/transport/sse"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

// Client 定义了 MCP 客户端接口
type Client interface {
	// 初始化和连接
	Initialize(ctx context.Context, clientInfo protocol.ClientInfo) (*protocol.InitializeResult, error)
	SendInitialized(ctx context.Context) error

	// 工具相关
	ListTools(ctx context.Context, cursor string) (*protocol.ListToolsResult, error)
	CallTool(ctx context.Context, name string, args map[string]interface{}) (*protocol.CallToolResult, error)

	// 资源相关
	ListResources(ctx context.Context, cursor string) (*protocol.ListResourcesResult, error)
	ReadResource(ctx context.Context, uri string) (*protocol.ReadResourceResult, error)

	// 提示模板相关
	ListPrompts(ctx context.Context, cursor string) (*protocol.ListPromptsResult, error)
	GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error)

	// 通用
	SendNotification(ctx context.Context, method string, params interface{}) error
	Close() error
}

// MCPClient 实现了 MCP 客户端
type MCPClient struct {
	transport       transport.Transport
	clientInfo      protocol.ClientInfo
	serverInfo      *protocol.ServerInfo
	capabilities    *protocol.ServerCapabilities
	protocolVersion string

	pendingRequests map[string]chan *protocol.JSONRPCMessage
	mu              sync.RWMutex

	initialized bool
}

// Option 定义客户端配置选项
type Option func(*MCPClient) error

// WithTransport 设置传输层
func WithTransport(t transport.Transport) Option {
	return func(c *MCPClient) error {
		c.transport = t
		return nil
	}
}

// WithStdioTransport 配置 STDIO 传输层
func WithStdioTransport(command string, args []string) Option {
	return func(c *MCPClient) error {
		t, err := stdio.NewWithCommand(command, args)
		if err != nil {
			return fmt.Errorf("failed to create stdio transport: %w", err)
		}
		c.transport = t
		return nil
	}
}

// WithSSETransport 配置 SSE 传输层
func WithSSETransport(url string) Option {
	return func(c *MCPClient) error {
		t := sse.New(url,
			sse.WithProtocolVersion(protocol.MCPVersion))
		if err := t.Connect(context.Background()); err != nil {
			return fmt.Errorf("failed to connect SSE transport: %w", err)
		}
		c.transport = t
		return nil
	}
}

// WithClientInfo 设置客户端信息
func WithClientInfo(name, version string) Option {
	return func(c *MCPClient) error {
		c.clientInfo = protocol.ClientInfo{
			Name:    name,
			Version: version,
		}
		return nil
	}
}

// New 创建新的 MCP 客户端
func New(options ...Option) (Client, error) {
	client := &MCPClient{
		pendingRequests: make(map[string]chan *protocol.JSONRPCMessage),
		protocolVersion: protocol.MCPVersion,
		clientInfo: protocol.ClientInfo{
			Name:    "mcp-go-client",
			Version: "1.0.0",
		},
	}

	for _, option := range options {
		if err := option(client); err != nil {
			return nil, err
		}
	}

	if client.transport == nil {
		return nil, fmt.Errorf("transport is required")
	}

	// 启动消息接收循环
	go client.receiveLoop(context.Background())

	return client, nil
}

// receiveLoop 处理接收到的消息
func (c *MCPClient) receiveLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			data, err := c.transport.Receive(ctx)
			if err != nil {
				// TODO: 添加错误处理和重连逻辑
				return
			}

			var message protocol.JSONRPCMessage
			if err := json.Unmarshal(data, &message); err != nil {
				continue
			}

			// 处理响应消息
			if message.ID != nil {
				c.mu.RLock()
				ch, ok := c.pendingRequests[*message.ID]
				c.mu.RUnlock()

				if ok {
					select {
					case ch <- &message:
					case <-ctx.Done():
						return
					}
				}
			}

			// 处理通知消息
			if message.Method != "" && message.ID == nil {
				c.handleNotification(&message)
			}
		}
	}
}

// handleNotification 处理服务端通知
func (c *MCPClient) handleNotification(message *protocol.JSONRPCMessage) {
	// TODO: 实现通知处理逻辑
	// 例如：tools/list_changed, resources/list_changed, prompts/list_changed 等
}

// sendRequest 发送请求并等待响应
func (c *MCPClient) sendRequest(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error) {
	id := uuid.New().String()

	var paramsJSON json.RawMessage
	if params != nil {
		bytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal parameters: %w", err)
		}
		paramsJSON = bytes
	}

	message := protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      &id,
		Method:  method,
		Params:  paramsJSON,
	}

	respChan := make(chan *protocol.JSONRPCMessage, 1)

	c.mu.Lock()
	c.pendingRequests[id] = respChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pendingRequests, id)
		c.mu.Unlock()
	}()

	msgBytes, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := c.transport.Send(ctx, msgBytes); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respChan:
		if resp.Error != nil {
			return nil, fmt.Errorf("server error [%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("request timeout")
	}
}

// Initialize 执行 MCP 初始化握手
func (c *MCPClient) Initialize(ctx context.Context, clientInfo protocol.ClientInfo) (*protocol.InitializeResult, error) {
	if c.initialized {
		return nil, fmt.Errorf("client already initialized")
	}

	c.clientInfo = clientInfo

	// 构造初始化请求
	initRequest := protocol.InitializeRequest{
		ProtocolVersion: c.protocolVersion,
		Capabilities: protocol.ClientCapabilities{
			// 客户端能力可以根据需要扩展
			Experimental: make(map[string]interface{}),
		},
		ClientInfo: c.clientInfo,
	}

	resp, err := c.sendRequest(ctx, "initialize", initRequest)
	if err != nil {
		return nil, fmt.Errorf("initialize failed: %w", err)
	}

	var result protocol.InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal initialize result: %w", err)
	}

	c.serverInfo = &result.ServerInfo
	c.capabilities = &result.Capabilities
	c.protocolVersion = result.ProtocolVersion
	c.initialized = true

	return &result, nil
}

// SendInitialized 发送初始化完成通知
func (c *MCPClient) SendInitialized(ctx context.Context) error {
	if !c.initialized {
		return fmt.Errorf("client not initialized")
	}

	return c.SendNotification(ctx, "notifications/initialized", nil)
}

// ListTools 获取工具列表
func (c *MCPClient) ListTools(ctx context.Context, cursor string) (*protocol.ListToolsResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	var params *protocol.ListToolsParams
	if cursor != "" {
		params = &protocol.ListToolsParams{
			Cursor: cursor,
		}
	}

	resp, err := c.sendRequest(ctx, "tools/list", params)
	if err != nil {
		return nil, err
	}

	var result protocol.ListToolsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tools list: %w", err)
	}

	return &result, nil
}

// CallTool 调用工具
func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*protocol.CallToolResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := protocol.CallToolParams{
		Name:      name,
		Arguments: args,
	}

	resp, err := c.sendRequest(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}

	var result protocol.CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tool result: %w", err)
	}

	return &result, nil
}

// ListResources 获取资源列表
func (c *MCPClient) ListResources(ctx context.Context, cursor string) (*protocol.ListResourcesResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	var params *protocol.ListResourcesParams
	if cursor != "" {
		params = &protocol.ListResourcesParams{
			Cursor: cursor,
		}
	}

	resp, err := c.sendRequest(ctx, "resources/list", params)
	if err != nil {
		return nil, err
	}

	var result protocol.ListResourcesResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resources list: %w", err)
	}

	return &result, nil
}

// ReadResource 读取资源内容
func (c *MCPClient) ReadResource(ctx context.Context, uri string) (*protocol.ReadResourceResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := protocol.ReadResourceParams{
		URI: uri,
	}

	resp, err := c.sendRequest(ctx, "resources/read", params)
	if err != nil {
		return nil, err
	}

	var result protocol.ReadResourceResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource content: %w", err)
	}

	return &result, nil
}

// ListPrompts 获取提示模板列表
func (c *MCPClient) ListPrompts(ctx context.Context, cursor string) (*protocol.ListPromptsResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	var params *protocol.ListPromptsParams
	if cursor != "" {
		params = &protocol.ListPromptsParams{
			Cursor: cursor,
		}
	}

	resp, err := c.sendRequest(ctx, "prompts/list", params)
	if err != nil {
		return nil, err
	}

	var result protocol.ListPromptsResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompts list: %w", err)
	}

	return &result, nil
}

// GetPrompt 获取提示模板
func (c *MCPClient) GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := protocol.GetPromptParams{
		Name:      name,
		Arguments: args,
	}

	resp, err := c.sendRequest(ctx, "prompts/get", params)
	if err != nil {
		return nil, err
	}

	var result protocol.GetPromptResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompt result: %w", err)
	}

	return &result, nil
}

// SendNotification 发送通知消息
func (c *MCPClient) SendNotification(ctx context.Context, method string, params interface{}) error {
	var paramsJSON json.RawMessage
	if params != nil {
		bytes, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal parameters: %w", err)
		}
		paramsJSON = bytes
	}

	message := protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		Params:  paramsJSON,
	}

	msgBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	return c.transport.Send(ctx, msgBytes)
}

// Close 关闭客户端连接
func (c *MCPClient) Close() error {
	if c.transport != nil {
		return c.transport.Close()
	}
	return nil
}

// GetServerInfo 获取服务器信息
func (c *MCPClient) GetServerInfo() *protocol.ServerInfo {
	return c.serverInfo
}

// GetCapabilities 获取服务器能力
func (c *MCPClient) GetCapabilities() *protocol.ServerCapabilities {
	return c.capabilities
}

// IsInitialized 检查是否已初始化
func (c *MCPClient) IsInitialized() bool {
	return c.initialized
}

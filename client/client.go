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
	"github.com/voocel/mcp-sdk-go/transport/streamable"
)

type Client interface {
	Initialize(ctx context.Context, clientInfo protocol.ClientInfo) (*protocol.InitializeResult, error)
	SendInitialized(ctx context.Context) error

	ListTools(ctx context.Context, cursor string) (*protocol.ListToolsResult, error)
	CallTool(ctx context.Context, name string, args map[string]interface{}) (*protocol.CallToolResult, error)

	ListResources(ctx context.Context, cursor string) (*protocol.ListResourcesResult, error)
	ReadResource(ctx context.Context, uri string) (*protocol.ReadResourceResult, error)

	ListPrompts(ctx context.Context, cursor string) (*protocol.ListPromptsResult, error)
	GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error)

	SendNotification(ctx context.Context, method string, params interface{}) error
	Close() error
}

// MCPClient 实现MCP客户端
type MCPClient struct {
	transport       transport.Transport
	clientInfo      protocol.ClientInfo
	serverInfo      *protocol.ServerInfo
	capabilities    *protocol.ServerCapabilities
	protocolVersion string

	pendingRequests map[string]chan *protocol.JSONRPCMessage
	mu              sync.RWMutex

	initialized    bool
	requestTimeout time.Duration // 添加可配置的超时时间
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

// WithStdioTransport 配置STDIO传输层
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

// WithSSETransport 配置SSE传输层
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

// WithStreamableHTTPTransport 配置Streamable HTTP传输层
func WithStreamableHTTPTransport(url string) Option {
	return func(c *MCPClient) error {
		t := streamable.New(url,
			streamable.WithProtocolVersion("2025-03-26"))
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

// WithTimeout 设置请求超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *MCPClient) error {
		c.requestTimeout = timeout
		return nil
	}
}

// New 创建MCP客户端
func New(options ...Option) (Client, error) {
	client := &MCPClient{
		pendingRequests: make(map[string]chan *protocol.JSONRPCMessage),
		protocolVersion: protocol.MCPVersion,
		clientInfo: protocol.ClientInfo{
			Name:    "mcp-go-client",
			Version: "1.0.0",
		},
		requestTimeout: 30 * time.Second, // 默认30秒超时
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
				// 如果是context取消，正常退出
				if ctx.Err() != nil {
					return
				}
				// 其他错误，暂时返回（未来可以添加重连逻辑）
				// fmt.Printf("接收消息错误: %v\n", err)
				return
			}

			var message protocol.JSONRPCMessage
			if err := json.Unmarshal(data, &message); err != nil {
				// JSON解析错误，跳过这条消息
				// fmt.Printf("JSON解析错误: %v\n", err)
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
	switch message.Method {
	case "notifications/tools/list_changed":
		// 工具列表变更通知
		// 客户端可以选择重新获取工具列表
		// 这里可以触发回调或事件
	case "notifications/resources/list_changed":
		// 资源列表变更通知
		// 客户端可以选择重新获取资源列表
	case "notifications/prompts/list_changed":
		// 提示模板列表变更通知
		// 客户端可以选择重新获取提示模板列表
	case "notifications/progress":
		// 进度通知（如果服务器支持）
		// 可以用于长时间运行的操作
	default:
		// 未知通知，记录但不报错
		// fmt.Printf("收到未知通知: %s\n", message.Method)
	}
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
	case <-time.After(c.requestTimeout):
		return nil, fmt.Errorf("request timeout")
	}
}

// Initialize MCP初始化握手
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

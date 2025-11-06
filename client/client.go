package client

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
	"github.com/voocel/mcp-sdk-go/transport/sse"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
	"github.com/voocel/mcp-sdk-go/transport/streamable"
)

// ElicitationHandler 定义处理 elicitation 请求的接口
type ElicitationHandler func(ctx context.Context, params *protocol.ElicitationCreateParams) (*protocol.ElicitationResult, error)

// SamplingHandler 定义处理 sampling 请求的接口
type SamplingHandler func(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error)

// RootsProvider 定义提供根目录列表的接口
type RootsProvider func(ctx context.Context) ([]protocol.Root, error)

type Client interface {
	Initialize(ctx context.Context, clientInfo protocol.ClientInfo) (*protocol.InitializeResult, error)
	SendInitialized(ctx context.Context) error

	ListTools(ctx context.Context, cursor string) (*protocol.ListToolsResult, error)
	CallTool(ctx context.Context, name string, args map[string]interface{}) (*protocol.CallToolResult, error)

	ListResources(ctx context.Context, cursor string) (*protocol.ListResourcesResult, error)
	ReadResource(ctx context.Context, uri string) (*protocol.ReadResourceResult, error)
	ListResourceTemplates(ctx context.Context, cursor string) (*protocol.ListResourceTemplatesResult, error)
	SubscribeResource(ctx context.Context, uri string) error
	UnsubscribeResource(ctx context.Context, uri string) error

	ListPrompts(ctx context.Context, cursor string) (*protocol.ListPromptsResult, error)
	GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error)

	SendRequest(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error)
	SendNotification(ctx context.Context, method string, params interface{}) error

	SetElicitationHandler(handler ElicitationHandler)
	SetSamplingHandler(handler SamplingHandler)
	SetRootsProvider(provider RootsProvider)

	NotifyRootsChanged(ctx context.Context) error

	Close() error
}

// MCPClient 实现MCP客户端
type MCPClient struct {
	transport       transport.Transport
	clientInfo      protocol.ClientInfo
	serverInfo      *protocol.ServerInfo
	capabilities    *protocol.ServerCapabilities
	protocolVersion string

	pendingRequests    map[string]chan *protocol.JSONRPCMessage
	elicitationHandler ElicitationHandler
	samplingHandler    SamplingHandler
	rootsProvider      RootsProvider
	mu                 sync.RWMutex

	initialized    bool
	requestTimeout time.Duration // 添加可配置的超时时间

	// Goroutine 生命周期管理
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
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
	return WithStdioTransportAndEnv(command, args, nil)
}

// WithStdioTransportAndEnv 配置STDIO传输层并设置环境变量
func WithStdioTransportAndEnv(command string, args []string, env []string) Option {
	return WithStdioTransportEnvAndDir(command, args, env, "")
}

// WithStdioTransportEnvAndDir 配置STDIO传输层并设置环境变量和工作目录
func WithStdioTransportEnvAndDir(command string, args []string, env []string, dir string) Option {
	return func(c *MCPClient) error {
		t, err := stdio.NewWithCommandEnvAndDir(command, args, env, dir)
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
		factory := func(ctx context.Context) (transport.Transport, error) {
			t := sse.New(url,
				sse.WithProtocolVersion(protocol.MCPVersion))
			if err := t.Connect(ctx); err != nil {
				return nil, fmt.Errorf("failed to connect SSE transport: %w", err)
			}
			return t, nil
		}

		wrapper := newReconnectingTransport(factory)
		if err := wrapper.ensure(context.Background()); err != nil {
			return err
		}
		c.transport = wrapper
		return nil
	}
}

// WithStreamableHTTPTransport 配置Streamable HTTP传输层
func WithStreamableHTTPTransport(url string) Option {
	return func(c *MCPClient) error {
		factory := func(ctx context.Context) (transport.Transport, error) {
			t := streamable.New(url,
				streamable.WithProtocolVersion(protocol.MCPVersion))
			return t, nil
		}

		wrapper := newReconnectingTransport(factory)
		if err := wrapper.ensure(context.Background()); err != nil {
			return err
		}
		c.transport = wrapper
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

// WithElicitationHandler 设置elicitation处理器
func WithElicitationHandler(handler ElicitationHandler) Option {
	return func(c *MCPClient) error {
		c.elicitationHandler = handler
		return nil
	}
}

// WithSamplingHandler 设置sampling处理器
func WithSamplingHandler(handler SamplingHandler) Option {
	return func(c *MCPClient) error {
		c.samplingHandler = handler
		return nil
	}
}

// WithRootsProvider 设置根目录提供器
func WithRootsProvider(provider RootsProvider) Option {
	return func(c *MCPClient) error {
		c.rootsProvider = provider
		return nil
	}
}

// WithRoots 设置静态根目录列表
func WithRoots(roots ...protocol.Root) Option {
	return func(c *MCPClient) error {
		c.rootsProvider = func(ctx context.Context) ([]protocol.Root, error) {
			return roots, nil
		}
		return nil
	}
}

// New 创建MCP客户端
func New(options ...Option) (Client, error) {
	// 创建可取消的 context
	ctx, cancel := context.WithCancel(context.Background())

	client := &MCPClient{
		pendingRequests: make(map[string]chan *protocol.JSONRPCMessage),
		protocolVersion: protocol.MCPVersion,
		clientInfo: protocol.ClientInfo{
			Name:    "mcp-go-client",
			Version: "1.0.0",
		},
		requestTimeout: 30 * time.Second, // 默认30秒超时
		ctx:            ctx,
		cancel:         cancel,
	}

	for _, option := range options {
		if err := option(client); err != nil {
			cancel() // 清理 context
			return nil, err
		}
	}

	if client.transport == nil {
		cancel() // 清理 context
		return nil, fmt.Errorf("transport is required")
	}

	// 启动消息接收循环
	client.wg.Add(1)
	go func() {
		defer client.wg.Done()
		client.receiveLoop(ctx)
	}()

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
				if ctx.Err() != nil {
					return
				}
				c.failPendingRequests(err)
				if c.tryReconnect(ctx) {
					continue
				}
				return
			}

			var message protocol.JSONRPCMessage
			if err := json.Unmarshal(data, &message); err != nil {
				// JSON解析错误，跳过这条消息
				// fmt.Printf("JSON解析错误: %v\n", err)
				continue
			}

			// 处理响应消息
			if !message.IsNotification() {
				idStr := message.GetIDString()
				c.mu.RLock()
				ch, ok := c.pendingRequests[idStr]
				c.mu.RUnlock()

				if ok {
					select {
					case ch <- &message:
					case <-ctx.Done():
						return
					}
				}
			}

			// 处理服务器发起的请求
			if message.Method != "" && !message.IsNotification() {
				c.handleServerRequest(ctx, &message)
			}

			// 处理通知消息
			if message.Method != "" && message.IsNotification() {
				c.handleNotification(&message)
			}
		}
	}
}

func (c *MCPClient) tryReconnect(ctx context.Context) bool {
	reconnectable, ok := c.transport.(interface {
		Reconnect(context.Context) error
	})
	if !ok {
		return false
	}

	backoff := []time.Duration{0, time.Second, 2 * time.Second, 5 * time.Second}
	for _, wait := range backoff {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(wait):
			}
		}

		if err := reconnectable.Reconnect(context.Background()); err == nil {
			return true
		}
	}

	return false
}

func (c *MCPClient) failPendingRequests(err error) {
	c.mu.Lock()
	if len(c.pendingRequests) == 0 {
		c.mu.Unlock()
		return
	}

	channels := make([]chan *protocol.JSONRPCMessage, 0, len(c.pendingRequests))
	for id, ch := range c.pendingRequests {
		channels = append(channels, ch)
		delete(c.pendingRequests, id)
	}
	c.mu.Unlock()

	errMsg := &protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		Error: &protocol.JSONRPCError{
			Code:    protocol.InternalError,
			Message: err.Error(),
		},
	}

	for _, ch := range channels {
		select {
		case ch <- errMsg:
		default:
		}
		close(ch)
	}
}

// handleServerRequest 处理服务器发起的请求
func (c *MCPClient) handleServerRequest(ctx context.Context, message *protocol.JSONRPCMessage) {
	var response *protocol.JSONRPCMessage
	var err error

	switch message.Method {
	case "sampling/createMessage":
		// 处理 LLM 采样请求
		response, err = c.handleSamplingRequest(ctx, message)
	case "roots/list":
		// 处理根目录列表请求
		response, err = c.handleRootsListRequest(ctx, message)
	case "elicitation/create":
		// 处理 elicitation 请求
		response, err = c.handleElicitationRequest(ctx, message)
	default:
		// 未知方法，返回方法未找到错误
		response = &protocol.JSONRPCMessage{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      message.ID,
			Error: &protocol.JSONRPCError{
				Code:    protocol.MethodNotFound,
				Message: fmt.Sprintf("method not found: %s", message.Method),
			},
		}
	}

	if err != nil {
		response = &protocol.JSONRPCMessage{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      message.ID,
			Error: &protocol.JSONRPCError{
				Code:    protocol.InternalError,
				Message: err.Error(),
			},
		}
	}

	// 发送响应
	if response != nil {
		responseBytes, _ := json.Marshal(response)
		c.transport.Send(ctx, responseBytes)
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
	case "notifications/resources/updated":
		// 资源更新通知
		var params protocol.ResourceUpdatedNotificationParams
		if err := json.Unmarshal(message.Params, &params); err == nil {
			// 通知应用层资源已更新
			// 可以触发回调或事件,让应用重新读取资源
			// fmt.Printf("资源已更新: %s\n", params.URI)
		}
	case "notifications/resources/templates/list_changed":
		// 资源模板列表变更通知
		// 客户端可以选择重新获取资源模板
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

// handleRootsListRequest 处理根目录列表请求
func (c *MCPClient) handleRootsListRequest(ctx context.Context, message *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error) {
	c.mu.RLock()
	provider := c.rootsProvider
	c.mu.RUnlock()

	var roots []protocol.Root
	var err error

	if provider != nil {
		roots, err = provider(ctx)
		if err != nil {
			return &protocol.JSONRPCMessage{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      message.ID,
				Error: &protocol.JSONRPCError{
					Code:    protocol.InternalError,
					Message: fmt.Sprintf("failed to list roots: %v", err),
				},
			}, nil
		}
	} else {
		// 如果没有设置提供器，返回空列表
		roots = []protocol.Root{}
	}

	result := protocol.NewListRootsResult(roots...)
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	return &protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      message.ID,
		Result:  json.RawMessage(resultBytes),
	}, nil
}

// handleSamplingRequest 处理 LLM 采样请求
func (c *MCPClient) handleSamplingRequest(ctx context.Context, message *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error) {
	c.mu.RLock()
	handler := c.samplingHandler
	c.mu.RUnlock()

	if handler == nil {
		return &protocol.JSONRPCMessage{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      message.ID,
			Error: &protocol.JSONRPCError{
				Code:    protocol.MethodNotFound,
				Message: "sampling handler not set",
			},
		}, nil
	}

	var request protocol.CreateMessageRequest
	if message.Params != nil {
		if err := json.Unmarshal(message.Params, &request); err != nil {
			return &protocol.JSONRPCMessage{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      message.ID,
				Error: &protocol.JSONRPCError{
					Code:    protocol.InvalidParams,
					Message: fmt.Sprintf("invalid params: %v", err),
				},
			}, nil
		}
	}

	if err := request.Validate(); err != nil {
		mcpErr, ok := err.(*protocol.MCPError)
		if !ok {
			mcpErr = protocol.NewMCPError(protocol.ErrorCodeInvalidParams, err.Error(), nil)
		}
		return &protocol.JSONRPCMessage{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      message.ID,
			Error: &protocol.JSONRPCError{
				Code:    mcpErr.Code,
				Message: mcpErr.Message,
				Data:    mcpErr.Data,
			},
		}, nil
	}

	// 调用处理器
	result, err := handler(ctx, &request)
	if err != nil {
		mcpErr, ok := err.(*protocol.MCPError)
		if !ok {
			mcpErr = protocol.NewMCPError(protocol.InternalError, err.Error(), nil)
		}
		return &protocol.JSONRPCMessage{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      message.ID,
			Error: &protocol.JSONRPCError{
				Code:    mcpErr.Code,
				Message: mcpErr.Message,
				Data:    mcpErr.Data,
			},
		}, nil
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return &protocol.JSONRPCMessage{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      message.ID,
			Error: &protocol.JSONRPCError{
				Code:    protocol.InternalError,
				Message: fmt.Sprintf("failed to marshal result: %v", err),
			},
		}, nil
	}

	return &protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      message.ID,
		Result:  json.RawMessage(resultBytes),
	}, nil
}

// handleElicitationRequest 处理 elicitation 请求
func (c *MCPClient) handleElicitationRequest(ctx context.Context, message *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error) {
	c.mu.RLock()
	handler := c.elicitationHandler
	c.mu.RUnlock()

	if handler == nil {
		// 如果没有设置处理器，返回取消响应
		result := protocol.NewElicitationCancel()
		resultBytes, _ := json.Marshal(result)

		return &protocol.JSONRPCMessage{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      message.ID,
			Result:  resultBytes,
		}, nil
	}

	var params protocol.ElicitationCreateParams
	if message.Params != nil {
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return nil, fmt.Errorf("failed to unmarshal elicitation params: %w", err)
		}
	}

	result, err := handler(ctx, &params)
	if err != nil {
		return nil, fmt.Errorf("elicitation handler failed: %w", err)
	}

	if result == nil {
		result = protocol.NewElicitationCancel()
	}

	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("invalid elicitation result: %w", err)
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal elicitation result: %w", err)
	}

	return &protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      message.ID,
		Result:  resultBytes,
	}, nil
}

func isNilInterface(i interface{}) bool {
	if i == nil {
		return true
	}
	v := reflect.ValueOf(i)
	kind := v.Kind()
	return (kind == reflect.Ptr || kind == reflect.Slice || kind == reflect.Map ||
		kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface) && v.IsNil()
}

// SendRequest 发送请求并等待响应
func (c *MCPClient) SendRequest(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error) {
	return c.sendRequest(ctx, method, params)
}

// sendRequest 发送请求并等待响应
func (c *MCPClient) sendRequest(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error) {
	id := uuid.New().String()

	var paramsJSON json.RawMessage
	if params != nil && !isNilInterface(params) {
		bytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal parameters: %w", err)
		}
		paramsJSON = bytes
	} else {
		paramsJSON = json.RawMessage("{}")
	}

	message := protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.StringToID(id),
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
		close(respChan) // 关闭 channel,防止 goroutine 泄漏
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
		if resp == nil {
			return nil, fmt.Errorf("connection reset")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("server error [%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-time.After(c.requestTimeout):
		return nil, fmt.Errorf("request timeout after %v for method %s", c.requestTimeout, method)
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
			// 根据 MCP 规范声明客户端能力
			Roots: &protocol.RootsCapability{
				ListChanged: true, // 支持根目录变更通知
			},
			Sampling:     &protocol.SamplingCapability{},    // 支持 LLM 采样
			Elicitation:  &protocol.ElicitationCapability{}, // 支持 elicitation
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
		params = &protocol.ListToolsParams{Cursor: cursor}
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

// ListResourceTemplates 获取资源模板列表
func (c *MCPClient) ListResourceTemplates(ctx context.Context, cursor string) (*protocol.ListResourceTemplatesResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	var params *protocol.ListResourceTemplatesRequest
	if cursor != "" {
		params = &protocol.ListResourceTemplatesRequest{Cursor: cursor}
	}

	resp, err := c.sendRequest(ctx, "resources/templates/list", params)
	if err != nil {
		return nil, err
	}

	var result protocol.ListResourceTemplatesResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource templates list: %w", err)
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

// SubscribeResource 订阅资源更新
func (c *MCPClient) SubscribeResource(ctx context.Context, uri string) error {
	if !c.initialized {
		return fmt.Errorf("client not initialized")
	}

	params := protocol.SubscribeParams{
		URI: uri,
	}

	_, err := c.sendRequest(ctx, "resources/subscribe", params)
	return err
}

// UnsubscribeResource 取消订阅资源更新
func (c *MCPClient) UnsubscribeResource(ctx context.Context, uri string) error {
	if !c.initialized {
		return fmt.Errorf("client not initialized")
	}

	params := protocol.UnsubscribeParams{
		URI: uri,
	}

	_, err := c.sendRequest(ctx, "resources/unsubscribe", params)
	return err
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

// SetElicitationHandler 设置elicitation处理器
func (c *MCPClient) SetElicitationHandler(handler ElicitationHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.elicitationHandler = handler
}

// SetSamplingHandler 设置sampling处理器
func (c *MCPClient) SetSamplingHandler(handler SamplingHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.samplingHandler = handler
}

// SetRootsProvider 设置根目录提供器
func (c *MCPClient) SetRootsProvider(provider RootsProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rootsProvider = provider
}

// SetRoots 设置静态根目录列表
func (c *MCPClient) SetRoots(roots ...protocol.Root) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rootsProvider = func(ctx context.Context) ([]protocol.Root, error) {
		return roots, nil
	}
}

// NotifyRootsChanged 通知服务器根目录列表已变更
func (c *MCPClient) NotifyRootsChanged(ctx context.Context) error {
	return c.SendNotification(ctx, "notifications/roots/list_changed", &protocol.RootsListChangedNotification{})
}

// Close 关闭客户端连接
func (c *MCPClient) Close() error {
	// 1. 取消 context,通知 goroutine 退出
	if c.cancel != nil {
		c.cancel()
	}

	// 2. 等待 goroutine 完全退出
	c.wg.Wait()

	// 3. 关闭传输层
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

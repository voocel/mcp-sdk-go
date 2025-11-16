package client

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

type ClientInfo struct {
	Name    string
	Version string
}

// ClientOptions 配置客户端的行为
type ClientOptions struct {
	// CreateMessageHandler 处理来自服务器的 sampling/createMessage 请求
	//
	// 设置为非 nil 值会使客户端声明 sampling 能力
	CreateMessageHandler func(context.Context, *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error)

	// ElicitationHandler 处理来自服务器的 elicitation/create 请求
	//
	// 设置为非 nil 值会使客户端声明 elicitation 能力
	ElicitationHandler func(context.Context, *protocol.ElicitationCreateParams) (*protocol.ElicitationResult, error)

	// 来自服务器的通知处理器
	ToolListChangedHandler      func(context.Context, *protocol.ToolsListChangedNotification)
	PromptListChangedHandler    func(context.Context, *protocol.PromptListChangedParams)
	ResourceListChangedHandler  func(context.Context, *protocol.ResourceListChangedParams)
	ResourceUpdatedHandler      func(context.Context, *protocol.ResourceUpdatedNotificationParams)
	LoggingMessageHandler       func(context.Context, *protocol.LoggingMessageParams)
	ProgressNotificationHandler func(context.Context, *protocol.ProgressNotificationParams)

	// KeepAlive 定义定期 "ping" 请求的间隔
	// 如果对等方未能响应 keepalive 检查发起的 ping,会话将自动关闭
	KeepAlive time.Duration
}

type Client struct {
	info     *ClientInfo
	opts     ClientOptions
	mu       sync.Mutex
	roots    []*protocol.Root
	sessions []*ClientSession
}

func NewClient(info *ClientInfo, opts *ClientOptions) *Client {
	if info == nil {
		panic("nil ClientInfo")
	}
	c := &Client{
		info:  info,
		roots: make([]*protocol.Root, 0),
	}
	if opts != nil {
		c.opts = *opts
	}
	return c
}

type ClientSessionOptions struct{}

// capabilities 返回客户端的能力声明
func (c *Client) capabilities() *protocol.ClientCapabilities {
	caps := &protocol.ClientCapabilities{
		Roots: &protocol.RootsCapability{
			ListChanged: true,
		},
	}
	if c.opts.CreateMessageHandler != nil {
		caps.Sampling = &protocol.SamplingCapability{}
	}
	if c.opts.ElicitationHandler != nil {
		caps.Elicitation = &protocol.ElicitationCapability{}
	}
	return caps
}

// Connect 通过给定的 transport 开始 MCP 会话
// 返回的会话已初始化并可以使用
//
// 通常,客户端负责在不再需要时关闭连接
// 但是,如果连接被服务器关闭,调用或通知将返回包装 ErrConnectionClosed 的错误
func (c *Client) Connect(ctx context.Context, t transport.Transport, _ *ClientSessionOptions) (*ClientSession, error) {
	conn, err := t.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("transport connect failed: %w", err)
	}

	cs := &ClientSession{
		conn:             conn,
		client:           c,
		waitErr:          make(chan error, 1),
		pending:          make(map[string]*pendingRequest),
		incomingRequests: make(map[string]context.CancelFunc),
	}

	c.mu.Lock()
	c.sessions = append(c.sessions, cs)
	c.mu.Unlock()

	go func() {
		err := cs.handleMessages(ctx)
		cs.waitErr <- err
		close(cs.waitErr)
	}()

	// 执行初始化握手
	initParams := &protocol.InitializeParams{
		ProtocolVersion: protocol.MCPVersion,
		ClientInfo: protocol.ClientInfo{
			Name:    c.info.Name,
			Version: c.info.Version,
		},
		Capabilities: *c.capabilities(),
	}

	var initResult protocol.InitializeResult
	if err := cs.sendRequest(ctx, protocol.MethodInitialize, initParams, &initResult); err != nil {
		_ = cs.Close()
		return nil, fmt.Errorf("initialize failed: %w", err)
	}

	if !protocol.IsVersionSupported(initResult.ProtocolVersion) {
		_ = cs.Close()
		return nil, fmt.Errorf("unsupported protocol version: %s (supported: %v)",
			initResult.ProtocolVersion, protocol.GetSupportedVersions())
	}

	cs.state.InitializeResult = &initResult

	if err := cs.sendNotification(ctx, protocol.NotificationInitialized, &protocol.InitializedParams{}); err != nil {
		_ = cs.Close()
		return nil, fmt.Errorf("send initialized notification failed: %w", err)
	}

	if c.opts.KeepAlive > 0 {
		cs.startKeepalive(c.opts.KeepAlive)
	}

	return cs, nil
}

// AddRoot 添加一个根目录并通知所有会话
func (c *Client) AddRoot(root *protocol.Root) {
	c.mu.Lock()
	c.roots = append(c.roots, root)
	sessions := make([]*ClientSession, len(c.sessions))
	copy(sessions, c.sessions)
	c.mu.Unlock()

	// 通知所有会话根目录列表已更改
	for _, cs := range sessions {
		_ = cs.NotifyRootsListChanged(context.Background())
	}
}

// RemoveRoot 移除一个根目录并通知所有会话
func (c *Client) RemoveRoot(uri string) {
	c.mu.Lock()
	var changed bool
	for i, root := range c.roots {
		if root.URI == uri {
			c.roots = append(c.roots[:i], c.roots[i+1:]...)
			changed = true
			break
		}
	}
	sessions := make([]*ClientSession, len(c.sessions))
	copy(sessions, c.sessions)
	c.mu.Unlock()

	// 只有在根目录真的变化时才通知
	if changed {
		for _, cs := range sessions {
			_ = cs.NotifyRootsListChanged(context.Background())
		}
	}
}

// ListRoots 列出所有根目录
func (c *Client) ListRoots() []*protocol.Root {
	c.mu.Lock()
	defer c.mu.Unlock()
	roots := make([]*protocol.Root, len(c.roots))
	copy(roots, c.roots)
	return roots
}

// ClientSession 是与 MCP 服务器的逻辑连接
// 可用于向服务器发送请求或通知
// 通过调用 Client.Connect 创建会话
//
// 调用 ClientSession.Close 关闭连接,或使用 ClientSession.Wait 等待服务器终止
type ClientSession struct {
	// 确保 onClose 最多被调用一次
	calledOnClose atomic.Bool
	onClose       func()

	conn    transport.Connection
	client  *Client
	waitErr chan error

	// keepalive
	keepaliveCancel context.CancelFunc

	// 会话状态
	state clientSessionState

	// 待处理的请求
	mu               sync.Mutex
	pending          map[string]*pendingRequest          // 客户端发送的请求
	incomingRequests map[string]context.CancelFunc       // 服务器发送的请求(用于取消)
	nextID           int64
}

type clientSessionState struct {
	InitializeResult *protocol.InitializeResult
}

type pendingRequest struct {
	method   string
	response chan *protocol.JSONRPCMessage
	err      chan error
}

// InitializeResult 返回初始化结果
func (cs *ClientSession) InitializeResult() *protocol.InitializeResult {
	return cs.state.InitializeResult
}

func (cs *ClientSession) ID() string {
	return cs.conn.SessionID()
}

func (cs *ClientSession) Close() error {
	if cs.keepaliveCancel != nil {
		cs.keepaliveCancel()
	}

	// 清理所有 pending 请求(在关闭连接之前)
	cs.mu.Lock()
	pending := cs.pending
	cs.pending = make(map[string]*pendingRequest)
	incomingRequests := cs.incomingRequests
	cs.incomingRequests = make(map[string]context.CancelFunc)
	cs.mu.Unlock()

	// 通知所有客户端发出的请求连接已关闭
	for _, req := range pending {
		select {
		case req.err <- fmt.Errorf("connection closed"):
		default:
		}
	}

	// 取消所有服务器发来的正在处理的请求
	for _, cancel := range incomingRequests {
		cancel()
	}

	err := cs.conn.Close()

	if cs.onClose != nil && cs.calledOnClose.CompareAndSwap(false, true) {
		cs.onClose()
	}

	cs.client.mu.Lock()
	for i, s := range cs.client.sessions {
		if s == cs {
			cs.client.sessions = append(cs.client.sessions[:i], cs.client.sessions[i+1:]...)
			break
		}
	}
	cs.client.mu.Unlock()

	return err
}

// Wait 等待连接被服务器关闭 通常,客户端应该负责关闭连接
func (cs *ClientSession) Wait() error {
	return <-cs.waitErr
}

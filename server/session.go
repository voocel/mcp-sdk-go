package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// ServerSession 表示一个服务器会话, 每个客户端连接对应一个 ServerSession
type ServerSession struct {
	calledOnClose atomic.Bool
	onClose       func()

	server *Server
	conn   Connection // 底层连接(来自transport)

	mu      sync.Mutex
	state   ServerSessionState
	waitErr chan error
}

// ServerSessionState 会话状态
type ServerSessionState struct {
	// InitializeParams 来自 initialize 请求的参数
	InitializeParams *protocol.InitializeParams

	// InitializedParams 来自 notifications/initialized 的参数
	InitializedParams *protocol.InitializedParams

	// LogLevel 日志级别
	LogLevel protocol.LoggingLevel
}

// Connection 表示底层传输连接
type Connection interface {
	// SendNotification 发送通知到客户端
	SendNotification(ctx context.Context, method string, params interface{}) error

	// SendRequest 发送请求到客户端并等待响应
	SendRequest(ctx context.Context, method string, params interface{}, result interface{}) error

	Close() error

	SessionID() string
}

func (ss *ServerSession) ID() string {
	if ss.conn != nil {
		return ss.conn.SessionID()
	}
	return ""
}

func (ss *ServerSession) Close() error {
	if ss.calledOnClose.CompareAndSwap(false, true) {
		if ss.onClose != nil {
			ss.onClose()
		}
	}
	if ss.conn != nil {
		return ss.conn.Close()
	}
	return nil
}

// Wait 等待会话结束, 返回会话结束的错误
func (ss *ServerSession) Wait() error {
	if ss.waitErr == nil {
		return nil
	}
	return <-ss.waitErr
}

// updateState 更新会话状态
func (ss *ServerSession) updateState(mut func(*ServerSessionState)) {
	ss.mu.Lock()
	mut(&ss.state)
	ss.mu.Unlock()
}

// hasInitialized 检查是否已收到 initialized 通知
func (ss *ServerSession) hasInitialized() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.state.InitializedParams != nil
}

// NotifyProgress 发送进度通知到客户端
func (ss *ServerSession) NotifyProgress(ctx context.Context, params *protocol.ProgressNotificationParams) error {
	return ss.conn.SendNotification(ctx, protocol.NotificationProgress, params)
}

// Log 发送日志消息到客户端
func (ss *ServerSession) Log(ctx context.Context, params *protocol.LoggingMessageParams) error {
	ss.mu.Lock()
	logLevel := ss.state.LogLevel
	ss.mu.Unlock()

	// 如果客户端未设置日志级别,不发送日志
	if logLevel == "" {
		return nil
	}

	// TODO: 实现日志级别过滤
	return ss.conn.SendNotification(ctx, protocol.NotificationLoggingMessage, params)
}

// Ping 发送 ping 请求到客户端
func (ss *ServerSession) Ping(ctx context.Context) error {
	return ss.conn.SendRequest(ctx, protocol.MethodPing, &protocol.PingParams{}, &protocol.EmptyResult{})
}

// ListRoots 列出客户端根目录
func (ss *ServerSession) ListRoots(ctx context.Context) (*protocol.ListRootsResult, error) {
	var result protocol.ListRootsResult
	err := ss.conn.SendRequest(ctx, protocol.MethodRootsList, &protocol.ListRootsParams{}, &result)
	return &result, err
}

// CreateMessage 发送采样请求到客户端
func (ss *ServerSession) CreateMessage(ctx context.Context, params *protocol.CreateMessageParams) (*protocol.CreateMessageResult, error) {
	var result protocol.CreateMessageResult
	err := ss.conn.SendRequest(ctx, protocol.MethodSamplingCreateMessage, params, &result)
	return &result, err
}

// Elicit 发送 elicitation 请求到客户端,请求用户输入
func (ss *ServerSession) Elicit(ctx context.Context, params *protocol.ElicitationCreateParams) (*protocol.ElicitationResult, error) {
	var result protocol.ElicitationResult
	err := ss.conn.SendRequest(ctx, protocol.MethodElicitationCreate, params, &result)
	return &result, err
}

// InitializeParams 返回初始化参数
func (ss *ServerSession) InitializeParams() *protocol.InitializeParams {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.state.InitializeParams
}

// CallToolRequest 表示工具调用请求,允许工具处理函数发送通知
type CallToolRequest struct {
	// Session 当前会话
	Session *ServerSession

	// Params 原始参数
	Params *protocol.CallToolParams
}

// ToolHandler 工具处理函数
// 接收 CallToolRequest,可以通过 req.Session 发送通知
type ToolHandler func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error)

// ========== connAdapter: 将 transport.Connection 适配到 server.Connection ==========

// connAdapter 将 transport.Connection 适配为 server.Connection
type connAdapter struct {
	conn transport.Connection
}

func newConnAdapter(conn transport.Connection) *connAdapter {
	return &connAdapter{conn: conn}
}

func (a *connAdapter) SendNotification(ctx context.Context, method string, params interface{}) error {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}

	msg := &protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  json.RawMessage(paramsBytes),
	}

	return a.conn.Write(ctx, msg)
}

func (a *connAdapter) SendRequest(ctx context.Context, method string, params interface{}, result interface{}) error {
	// TODO: 实现请求/响应机制
	// 需要生成 ID,发送请求,等待响应
	return fmt.Errorf("SendRequest not implemented yet")
}

func (a *connAdapter) Close() error {
	return a.conn.Close()
}

func (a *connAdapter) SessionID() string {
	return a.conn.SessionID()
}

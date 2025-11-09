package transport

import (
	"context"
	"errors"

	"github.com/voocel/mcp-sdk-go/protocol"
)

var ErrConnectionClosed = errors.New("connection closed")

// Transport 用于创建客户端和服务器之间的双向连接
type Transport interface {
	// Connect 返回逻辑 JSON-RPC 连接
	// 它会被 Server.Connect 或 Client.Connect 精确调用一次
	Connect(ctx context.Context) (Connection, error)
}

// Connection 是一个逻辑双向 JSON-RPC 连接
type Connection interface {
	// Read 从连接读取下一条要处理的消息
	//
	// Connection 必须允许 Read 与 Close 并发调用
	// 特别是,调用 Close 应该解除正在等待输入的 Read
	Read(ctx context.Context) (*protocol.JSONRPCMessage, error)

	// Write 向连接写入新消息
	//
	// Write 可以并发调用,因为调用或响应可能在用户代码中并发发生
	Write(ctx context.Context, msg *protocol.JSONRPCMessage) error

	// Close 关闭连接
	// 当 Read 或 Write 失败时会隐式调用
	//
	// Close 可能被多次调用,可能并发调用
	Close() error

	// SessionID 返回会话 ID
	// 如果没有会话 ID,返回空字符串
	SessionID() string
}

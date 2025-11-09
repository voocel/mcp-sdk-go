package streamable

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// StreamableTransport 实现 transport.Transport 接口
// 用于 Streamable HTTP 传输. 使用 stream 和 deliver 回调模式
type StreamableTransport struct {
	sessionID string
	closed    atomic.Bool

	// streams 管理逻辑流
	streamsMu      sync.Mutex
	streams        map[string]*stream // 按 stream ID 索引
	requestStreams map[string]string  // 请求 ID -> stream ID
}

// stream 表示一个逻辑流
type stream struct {
	id string

	mu       sync.Mutex
	deliver  func(data []byte, final bool) error // 回调函数,用于发送数据
	requests map[string]struct{}                 // 未完成的请求 ID
}

func NewStreamableTransport(sessionID string) *StreamableTransport {
	t := &StreamableTransport{
		sessionID:      sessionID,
		streams:        make(map[string]*stream),
		requestStreams: make(map[string]string),
	}

	// 创建默认的 standalone SSE stream
	t.streams[""] = &stream{
		id:       "",
		requests: make(map[string]struct{}),
	}

	return t
}

func (t *StreamableTransport) Connect(ctx context.Context) (transport.Connection, error) {
	return &streamableConn{
		transport: t,
	}, nil
}

type streamableConn struct {
	transport *StreamableTransport
}

func (c *streamableConn) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	// Streamable HTTP 不使用 Read 方法
	// 消息直接在 handlePost 中处理
	return nil, fmt.Errorf("Read not supported in Streamable HTTP transport")
}

func (c *streamableConn) Write(ctx context.Context, msg *protocol.JSONRPCMessage) error {
	if c.transport.closed.Load() {
		return transport.ErrConnectionClosed
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	var (
		relatedRequest string // 相关的请求 ID
		responseTo     string // 如果是响应,这是响应的请求 ID
	)

	isResponse := !msg.IsNotification() && (msg.Result != nil || msg.Error != nil)

	if isResponse {
		responseTo = msg.GetIDString()
		relatedRequest = responseTo
	}

	// 查找对应的 stream
	var s *stream
	c.transport.streamsMu.Lock()
	if relatedRequest != "" {
		if streamID, ok := c.transport.requestStreams[relatedRequest]; ok {
			s = c.transport.streams[streamID]
		}
	} else {
		// 默认使用 standalone SSE stream
		s = c.transport.streams[""]
	}

	// 如果是响应,删除请求映射
	if responseTo != "" {
		delete(c.transport.requestStreams, responseTo)
	}
	c.transport.streamsMu.Unlock()

	if s == nil {
		// stream 不存在,可能已经关闭
		return fmt.Errorf("stream not found")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查 stream 是否完成
	if responseTo != "" {
		delete(s.requests, responseTo)
		// 如果所有请求都已响应,stream 完成
		if len(s.requests) == 0 && s.id != "" {
			// 删除 stream (除了 standalone stream)
			c.transport.streamsMu.Lock()
			delete(c.transport.streams, s.id)
			c.transport.streamsMu.Unlock()
		}
	}

	final := len(s.requests) == 0 && s.id != ""

	if s.deliver != nil {
		return s.deliver(data, final)
	}

	// 没有 deliver 函数,消息无法发送
	return nil
}

func (c *streamableConn) Close() error {
	// 对于 Streamable HTTP,不要关闭 channels
	// 因为同一个 session 可能会有多个请求
	// channels 会在 session 被删除时由 HTTPHandler 关闭
	return nil
}

func (c *streamableConn) SessionID() string {
	return c.transport.sessionID
}

// RegisterStream 注册一个新的 stream
func (t *StreamableTransport) RegisterStream(streamID string, requests map[string]struct{}, deliver func(data []byte, final bool) error) {
	t.streamsMu.Lock()
	defer t.streamsMu.Unlock()

	s := &stream{
		id:       streamID,
		deliver:  deliver,
		requests: requests,
	}
	t.streams[streamID] = s

	// 注册请求映射
	for reqID := range requests {
		t.requestStreams[reqID] = streamID
	}
}

func (t *StreamableTransport) UnregisterStream(streamID string) {
	t.streamsMu.Lock()
	defer t.streamsMu.Unlock()

	if s, ok := t.streams[streamID]; ok {
		s.mu.Lock()
		s.deliver = nil
		s.mu.Unlock()
		delete(t.streams, streamID)
	}
}

func (t *StreamableTransport) Close() error {
	t.closed.Store(true)
	return nil
}

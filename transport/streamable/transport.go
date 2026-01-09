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

// StreamableTransport implements the transport.Transport interface.
// Used for Streamable HTTP transport where messages are delivered via callbacks.
type StreamableTransport struct {
	sessionID string
	closed    atomic.Bool

	mu             sync.Mutex
	streams        map[string]*stream
	requestStreams map[string]string // requestID -> streamID
}

// stream represents a logical SSE stream
type stream struct {
	id       string
	deliver  func(data []byte, final bool) error
	requests map[string]struct{}
}

func NewStreamableTransport(sessionID string) *StreamableTransport {
	return &StreamableTransport{
		sessionID:      sessionID,
		streams:        make(map[string]*stream),
		requestStreams: make(map[string]string),
	}
}

func (t *StreamableTransport) Connect(ctx context.Context) (transport.Connection, error) {
	return &streamableConn{transport: t}, nil
}

type streamableConn struct {
	transport *StreamableTransport
}

func (c *streamableConn) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	// Streamable HTTP processes messages directly in handlePost
	return nil, fmt.Errorf("read not supported in Streamable HTTP transport")
}

func (c *streamableConn) Write(ctx context.Context, msg *protocol.JSONRPCMessage) error {
	if c.transport.closed.Load() {
		return transport.ErrConnectionClosed
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	isResponse := !msg.IsNotification() && (msg.Result != nil || msg.Error != nil)
	var responseTo string
	if isResponse {
		responseTo = msg.GetIDString()
	}

	c.transport.mu.Lock()
	defer c.transport.mu.Unlock()

	// Find the stream for this message
	var s *stream
	if responseTo != "" {
		if streamID, ok := c.transport.requestStreams[responseTo]; ok {
			s = c.transport.streams[streamID]
			delete(c.transport.requestStreams, responseTo)
		}
	}

	if s == nil {
		return nil // No stream to deliver to
	}

	// Check if stream is complete
	delete(s.requests, responseTo)
	final := len(s.requests) == 0

	if final {
		delete(c.transport.streams, s.id)
	}

	if s.deliver != nil {
		return s.deliver(data, final)
	}
	return nil
}

func (c *streamableConn) Close() error {
	return nil
}

func (c *streamableConn) SessionID() string {
	return c.transport.sessionID
}

// RegisterStream registers a new stream with its delivery callback.
func (t *StreamableTransport) RegisterStream(streamID string, requestID string, deliver func(data []byte, final bool) error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := &stream{
		id:       streamID,
		deliver:  deliver,
		requests: map[string]struct{}{requestID: {}},
	}
	t.streams[streamID] = s
	t.requestStreams[requestID] = streamID
}

func (t *StreamableTransport) UnregisterStream(streamID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.streams, streamID)
}

func (t *StreamableTransport) Close() error {
	t.closed.Store(true)
	return nil
}

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
// Used for Streamable HTTP transport. Uses stream and deliver callback pattern.
type StreamableTransport struct {
	sessionID string
	closed    atomic.Bool

	// streams manages logical streams
	streamsMu      sync.Mutex
	streams        map[string]*stream // Indexed by stream ID
	requestStreams map[string]string  // Request ID -> stream ID
}

// stream represents a logical stream
type stream struct {
	id string

	mu       sync.Mutex
	deliver  func(data []byte, final bool) error // Callback function for sending data
	requests map[string]struct{}                 // Outstanding request IDs
}

func NewStreamableTransport(sessionID string) *StreamableTransport {
	t := &StreamableTransport{
		sessionID:      sessionID,
		streams:        make(map[string]*stream),
		requestStreams: make(map[string]string),
	}

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
	// Streamable HTTP does not use Read method
	// Messages are processed directly in handlePost
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

	var (
		relatedRequest string // Related request ID
		responseTo     string // If it's a response, this is the request ID being responded to
	)

	isResponse := !msg.IsNotification() && (msg.Result != nil || msg.Error != nil)

	if isResponse {
		responseTo = msg.GetIDString()
		relatedRequest = responseTo
	}

	// Find the corresponding stream
	var s *stream
	c.transport.streamsMu.Lock()
	if relatedRequest != "" {
		if streamID, ok := c.transport.requestStreams[relatedRequest]; ok {
			s = c.transport.streams[streamID]
		}
	} else {
		// Default to standalone SSE stream
		s = c.transport.streams[""]
	}

	// If it's a response, delete the request mapping
	if responseTo != "" {
		delete(c.transport.requestStreams, responseTo)
	}
	c.transport.streamsMu.Unlock()

	if s == nil {
		// Stream doesn't exist, may have been closed
		return fmt.Errorf("stream not found")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if stream is complete
	if responseTo != "" {
		delete(s.requests, responseTo)
		// If all requests have been responded to, stream is complete
		if len(s.requests) == 0 && s.id != "" {
			// Delete stream (except standalone stream)
			c.transport.streamsMu.Lock()
			delete(c.transport.streams, s.id)
			c.transport.streamsMu.Unlock()
		}
	}

	final := len(s.requests) == 0 && s.id != ""

	if s.deliver != nil {
		return s.deliver(data, final)
	}

	// No deliver function, message cannot be sent
	return nil
}

func (c *streamableConn) Close() error {
	// For Streamable HTTP, don't close channels
	// because the same session may have multiple requests
	// Channels will be closed by HTTPHandler when session is deleted
	return nil
}

func (c *streamableConn) SessionID() string {
	return c.transport.sessionID
}

// RegisterStream registers a new stream
func (t *StreamableTransport) RegisterStream(streamID string, requests map[string]struct{}, deliver func(data []byte, final bool) error) {
	t.streamsMu.Lock()
	defer t.streamsMu.Unlock()

	s := &stream{
		id:       streamID,
		deliver:  deliver,
		requests: requests,
	}
	t.streams[streamID] = s

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

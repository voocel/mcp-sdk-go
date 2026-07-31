// Package transport defines the client-side transport abstraction of the
// stateless MCP 2026-07-28 protocol: a request goes out, a stream of
// notifications followed by one response comes back. There is no server-side
// transport interface — server transports consume server.Handle directly.
package transport

import (
	"context"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// Request is one outgoing JSON-RPC message plus the transport metadata headers
// that accompany it. HTTP transports send Headers on the wire; stdio ignores
// them (all metadata is inline in _meta).
type Request struct {
	Message *protocol.Message
	Headers map[string]string
}

// Stream yields the messages produced by one request: zero or more
// notifications, then the final response, then io.EOF. Closing the stream is
// the transport-level cancellation signal for the request.
type Stream interface {
	Recv() (*protocol.Message, error)
	Close() error
}

// Transport delivers requests to a server.
type Transport interface {
	// Do sends a request and returns its response stream. For notifications
	// (Message without ID) it returns (nil, nil) on successful delivery.
	Do(ctx context.Context, req *Request) (Stream, error)
	Close() error
}

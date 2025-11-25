package transport

import (
	"context"
	"errors"

	"github.com/voocel/mcp-sdk-go/protocol"
)

var ErrConnectionClosed = errors.New("connection closed")

// Transport is used to create bidirectional connections between client and server
type Transport interface {
	// Connect returns a logical JSON-RPC connection
	// It will be called exactly once by Server.Connect or Client.Connect
	Connect(ctx context.Context) (Connection, error)
}

// Connection is a logical bidirectional JSON-RPC connection
type Connection interface {
	// Read reads the next message to be processed from the connection
	//
	// Connection must allow Read to be called concurrently with Close.
	// In particular, calling Close should unblock a Read waiting for input.
	Read(ctx context.Context) (*protocol.JSONRPCMessage, error)

	// Write writes a new message to the connection
	//
	// Write can be called concurrently because calls or responses may occur concurrently in user code.
	Write(ctx context.Context, msg *protocol.JSONRPCMessage) error

	// Close closes the connection.
	// Called implicitly when Read or Write fails.
	//
	// Close may be called multiple times, possibly concurrently.
	Close() error

	// SessionID returns the session ID.
	// Returns empty string if there is no session ID.
	SessionID() string
}

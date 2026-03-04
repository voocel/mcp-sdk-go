package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// LoggingTransport is a [Transport] decorator that logs all JSON-RPC messages to an [io.Writer].
type LoggingTransport struct {
	Transport Transport
	Writer    io.Writer
}

// Connect connects the underlying transport, returning a [Connection] that logs all messages.
func (t *LoggingTransport) Connect(ctx context.Context) (Connection, error) {
	delegate, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &loggingConn{delegate: delegate, w: t.Writer}, nil
}

type loggingConn struct {
	delegate Connection
	mu       sync.Mutex
	w        io.Writer
}

func (c *loggingConn) SessionID() string { return c.delegate.SessionID() }

func (c *loggingConn) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	msg, err := c.delegate.Read(ctx)
	if err != nil {
		c.log("read error: %v\n", err)
	} else {
		data, jerr := json.Marshal(msg)
		if jerr != nil {
			c.log("read marshal error: %v\n", jerr)
		} else {
			c.log("read: %s\n", data)
		}
	}
	return msg, err
}

func (c *loggingConn) Write(ctx context.Context, msg *protocol.JSONRPCMessage) error {
	err := c.delegate.Write(ctx, msg)
	if err != nil {
		c.log("write error: %v\n", err)
	} else {
		data, jerr := json.Marshal(msg)
		if jerr != nil {
			c.log("write marshal error: %v\n", jerr)
		} else {
			c.log("write: %s\n", data)
		}
	}
	return err
}

func (c *loggingConn) Close() error {
	return c.delegate.Close()
}

func (c *loggingConn) log(format string, args ...any) {
	c.mu.Lock()
	fmt.Fprintf(c.w, format, args...)
	c.mu.Unlock()
}

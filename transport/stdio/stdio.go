package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

type StdioTransport struct{}

func (*StdioTransport) Connect(ctx context.Context) (transport.Connection, error) {
	return newStdioConn(), nil
}

type stdioConn struct {
	scanner *bufio.Scanner
	mu      sync.Mutex
	closed  atomic.Bool
}

func newStdioConn() *stdioConn {
	return &stdioConn{
		scanner: bufio.NewScanner(os.Stdin),
	}
}

func (c *stdioConn) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	if c.closed.Load() {
		return nil, transport.ErrConnectionClosed
	}

	msgChan := make(chan *protocol.JSONRPCMessage, 1)
	errChan := make(chan error, 1)

	go func() {
		if !c.scanner.Scan() {
			if err := c.scanner.Err(); err != nil {
				errChan <- fmt.Errorf("scanner error: %w", err)
			} else {
				errChan <- io.EOF
			}
			return
		}

		data := c.scanner.Bytes()
		if len(data) == 0 {
			errChan <- fmt.Errorf("empty message")
			return
		}

		var msg protocol.JSONRPCMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			errChan <- fmt.Errorf("invalid JSON-RPC message: %w", err)
			return
		}

		msgChan <- &msg
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errChan:
		return nil, err
	case msg := <-msgChan:
		return msg, nil
	}
}

func (c *stdioConn) Write(ctx context.Context, msg *protocol.JSONRPCMessage) error {
	if c.closed.Load() {
		return transport.ErrConnectionClosed
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if _, err := os.Stdout.Write(data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	if _, err := os.Stdout.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

func (c *stdioConn) Close() error {
	c.closed.Store(true)
	return nil
}

func (c *stdioConn) SessionID() string {
	return ""
}

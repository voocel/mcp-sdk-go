package stdio

import (
	"context"
	"encoding/json"
	"fmt"
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
	mu      sync.Mutex
	closed  atomic.Bool

	done     chan struct{}
	incoming chan *protocol.JSONRPCMessage
	errs     chan error
}

func newStdioConn() *stdioConn {
	c := &stdioConn{
		done:     make(chan struct{}),
		incoming: make(chan *protocol.JSONRPCMessage, 16),
		errs:     make(chan error, 1),
	}

	go c.readLoop()
	return c
}

func (c *stdioConn) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	if c.closed.Load() {
		return nil, transport.ErrConnectionClosed
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, transport.ErrConnectionClosed
	case err := <-c.errs:
		return nil, err
	case msg, ok := <-c.incoming:
		if !ok {
			return nil, transport.ErrConnectionClosed
		}
		return msg, nil
	}
}

func (c *stdioConn) readLoop() {
	defer func() {
		close(c.incoming)
	}()

	decoder := json.NewDecoder(os.Stdin)

	for {
		select {
		case <-c.done:
			return
		default:
		}

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			select {
			case c.errs <- err:
			default:
			}
			return
		}
		if len(raw) == 0 {
			select {
			case c.errs <- fmt.Errorf("empty message"):
			default:
			}
			return
		}

		var msg protocol.JSONRPCMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			select {
			case c.errs <- fmt.Errorf("invalid JSON-RPC message: %w", err):
			default:
			}
			return
		}

		select {
		case c.incoming <- &msg:
		case <-c.done:
			return
		}
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
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(c.done)
	return nil
}

func (c *stdioConn) SessionID() string {
	return ""
}

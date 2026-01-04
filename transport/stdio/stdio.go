package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

type StdioTransport struct {
	// MaxMessageBytes limits the maximum size of a single message; 0 means unlimited.
	MaxMessageBytes int
}

func (t *StdioTransport) Connect(ctx context.Context) (transport.Connection, error) {
	return newStdioConn(t.MaxMessageBytes), nil
}

type stdioConn struct {
	maxMessageBytes int
	mu              sync.Mutex
	closed          atomic.Bool

	done     chan struct{}
	incoming chan *protocol.JSONRPCMessage
	errs     chan error
}

func newStdioConn(maxMessageBytes int) *stdioConn {
	c := &stdioConn{
		maxMessageBytes: maxMessageBytes,
		done:            make(chan struct{}),
		incoming:        make(chan *protocol.JSONRPCMessage, 16),
		errs:            make(chan error, 1),
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

	if c.maxMessageBytes > 0 {
		reader := bufio.NewReader(os.Stdin)
		for {
			select {
			case <-c.done:
				return
			default:
			}

			raw, err := readRawMessage(reader, c.maxMessageBytes)
			if err != nil {
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

func readRawMessage(r *bufio.Reader, maxBytes int) (json.RawMessage, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("invalid max bytes: %d", maxBytes)
	}

	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(chunk) > 0 {
			buf = append(buf, chunk...)
			if len(buf) > maxBytes {
				return nil, fmt.Errorf("message too large: limit %d bytes", maxBytes)
			}
		}

		if err == nil {
			return json.RawMessage(buf), nil
		}

		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}

		if errors.Is(err, io.EOF) {
			if len(buf) == 0 {
				return nil, io.EOF
			}
			return json.RawMessage(buf), nil
		}

		return nil, err
	}
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

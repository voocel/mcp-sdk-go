package client

import (
	"context"
	"sync"

	"github.com/voocel/mcp-sdk-go/transport"
)

// reconnectingTransport 提供自动重连能力，配合具体传输的工厂函数使用
type reconnectingTransport struct {
	mu      sync.RWMutex
	current transport.Transport
	factory func(ctx context.Context) (transport.Transport, error)
}

func newReconnectingTransport(factory func(ctx context.Context) (transport.Transport, error)) *reconnectingTransport {
	return &reconnectingTransport{factory: factory}
}

func (r *reconnectingTransport) ensure(ctx context.Context) error {
	r.mu.RLock()
	if r.current != nil {
		r.mu.RUnlock()
		return nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil {
		return nil
	}

	t, err := r.factory(ctx)
	if err != nil {
		return err
	}
	r.current = t
	return nil
}

func (r *reconnectingTransport) Send(ctx context.Context, data []byte) error {
	if err := r.ensure(ctx); err != nil {
		return err
	}

	r.mu.RLock()
	t := r.current
	r.mu.RUnlock()
	return t.Send(ctx, data)
}

func (r *reconnectingTransport) Receive(ctx context.Context) ([]byte, error) {
	if err := r.ensure(ctx); err != nil {
		return nil, err
	}

	r.mu.RLock()
	t := r.current
	r.mu.RUnlock()
	return t.Receive(ctx)
}

func (r *reconnectingTransport) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.current == nil {
		return nil
	}
	err := r.current.Close()
	r.current = nil
	return err
}

func (r *reconnectingTransport) Reconnect(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.current != nil {
		_ = r.current.Close()
		r.current = nil
	}

	t, err := r.factory(ctx)
	if err != nil {
		return err
	}
	r.current = t
	return nil
}

var _ transport.Transport = (*reconnectingTransport)(nil)

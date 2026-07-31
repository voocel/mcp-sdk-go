// Package mem provides an in-process transport that connects a client
// directly to a server's Handle function — for tests and embedding.
package mem

import (
	"context"
	"io"
	"sync"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// Handler is the structural seam of the server side (satisfied by
// *server.Server).
type Handler interface {
	Handle(ctx context.Context, msg *protocol.Message, emit func(*protocol.Message) error)
}

type Transport struct {
	h Handler
}

func New(h Handler) *Transport { return &Transport{h: h} }

func (t *Transport) Do(ctx context.Context, req *transport.Request) (transport.Stream, error) {
	if req.Message.Kind() == protocol.KindNotification {
		go t.h.Handle(context.WithoutCancel(ctx), req.Message, func(*protocol.Message) error { return nil })
		return nil, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	s := &stream{ch: make(chan *protocol.Message, 16), cancel: cancel}
	go func() {
		defer close(s.ch)
		t.h.Handle(ctx, req.Message, func(m *protocol.Message) error {
			select {
			case s.ch <- m:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	return s, nil
}

func (t *Transport) Close() error { return nil }

type stream struct {
	ch        chan *protocol.Message
	cancel    context.CancelFunc
	closeOnce sync.Once
}

func (s *stream) Recv() (*protocol.Message, error) {
	m, ok := <-s.ch
	if !ok {
		return nil, io.EOF
	}
	return m, nil
}

// Close cancels the in-flight request, mirroring the HTTP semantics where
// closing the response stream is the cancellation signal.
func (s *stream) Close() error {
	s.closeOnce.Do(s.cancel)
	return nil
}

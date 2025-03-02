package transport

import (
	"context"
)

type Transport interface {
	Send(ctx context.Context, data []byte) error

	Receive(ctx context.Context) ([]byte, error)

	Close() error
}

type Handler interface {
	HandleMessage(ctx context.Context, data []byte) ([]byte, error)
}

type Server interface {
	Serve(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

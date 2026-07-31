package server

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// RawHandler is the uniform shape every method resolves to at dispatch time.
type RawHandler func(ctx context.Context, req *Request) (protocol.Result, error)

// Middleware wraps a RawHandler. Middleware is applied at dispatch time, so
// registration order of tools and middleware does not matter.
type Middleware func(RawHandler) RawHandler

// Use appends middleware. The first Use'd middleware is outermost.
func (s *Server) Use(m ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.middleware = append(s.middleware, m...)
}

// Recovery converts handler panics into -32603 errors. The stack trace goes
// to slog, never to the wire.
func Recovery() Middleware {
	return func(next RawHandler) RawHandler {
		return func(ctx context.Context, req *Request) (res protocol.Result, err error) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("mcp handler panic", "method", req.Method(), "panic", r, "stack", string(debug.Stack()))
					res, err = nil, protocol.Errorf(protocol.CodeInternal, "internal error")
				}
			}()
			return next(ctx, req)
		}
	}
}

// Timeout bounds each request. subscriptions/listen is exempt: its lifetime
// is the stream itself.
func Timeout(d time.Duration) Middleware {
	return func(next RawHandler) RawHandler {
		return func(ctx context.Context, req *Request) (protocol.Result, error) {
			if req.Method() == protocol.MethodSubscriptionsListen {
				return next(ctx, req)
			}
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next(ctx, req)
		}
	}
}

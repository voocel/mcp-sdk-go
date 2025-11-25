package server

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
)

type Middleware func(ToolHandler) ToolHandler

// Use adds middleware to the Server. Middleware is executed in the order added (onion model).
func (s *Server) Use(middleware ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.middlewares = append(s.middlewares, middleware...)

	for name, st := range s.tools {
		wrappedHandler := applyMiddleware(st.handler, middleware)
		s.tools[name].handler = wrappedHandler
	}
}

// applyMiddleware applies the middleware chain
func applyMiddleware(handler ToolHandler, middlewares []Middleware) ToolHandler {
	// Apply middleware from back to front (forming the onion model)
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// LoggingMiddleware is a logging middleware
func LoggingMiddleware(logger *slog.Logger) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			start := time.Now()
			toolName := req.Params.Name

			logger.Info("tool call started",
				slog.String("tool", toolName),
				slog.Any("arguments", req.Params.Arguments),
			)

			result, err := next(ctx, req)

			duration := time.Since(start)

			if err != nil {
				logger.Error("tool call failed",
					slog.String("tool", toolName),
					slog.Duration("duration", duration),
					slog.String("error", err.Error()),
				)
			} else {
				logger.Info("tool call completed",
					slog.String("tool", toolName),
					slog.Duration("duration", duration),
					slog.Bool("isError", result.IsError),
				)
			}

			return result, err
		}
	}
}

// RecoveryMiddleware is a recovery middleware
func RecoveryMiddleware() Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (result *protocol.CallToolResult, err error) {
			defer func() {
				if r := recover(); r != nil {
					// Capture panic
					stack := debug.Stack()
					err = fmt.Errorf("panic recovered: %v\n%s", r, stack)
					result = ErrorResult("Internal server error", err)
				}
			}()

			return next(ctx, req)
		}
	}
}

// TimeoutMiddleware is a timeout middleware
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			// Create context with timeout
			timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			// Use channel to receive result
			resultCh := make(chan struct {
				result *protocol.CallToolResult
				err    error
			}, 1)

			go func() {
				result, err := next(timeoutCtx, req)
				resultCh <- struct {
					result *protocol.CallToolResult
					err    error
				}{result, err}
			}()

			select {
			case res := <-resultCh:
				return res.result, res.err
			case <-timeoutCtx.Done():
				return nil, TimeoutError(
					fmt.Sprintf("tool execution exceeded %v", timeout),
					WithDetail("tool", req.Params.Name),
				)
			}
		}
	}
}

// MetricsMiddleware is a metrics middleware
func MetricsMiddleware(collector MetricsCollector) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			start := time.Now()
			toolName := req.Params.Name

			result, err := next(ctx, req)

			duration := time.Since(start)

			// Record metrics
			collector.RecordToolCall(toolName, duration, err == nil)

			return result, err
		}
	}
}

// MetricsCollector is the metrics collector interface
type MetricsCollector interface {
	RecordToolCall(tool string, duration time.Duration, success bool)
}

// RateLimitMiddleware is a rate limiting middleware
func RateLimitMiddleware(limiter RateLimiter) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			toolName := req.Params.Name

			// Check rate limit
			if !limiter.Allow(toolName) {
				return nil, NewToolError(
					ErrTooManyRequest,
					fmt.Sprintf("rate limit exceeded for tool %s", toolName),
					WithDetail("tool", toolName),
				)
			}

			return next(ctx, req)
		}
	}
}

// RateLimiter is the rate limiter interface
type RateLimiter interface {
	Allow(tool string) bool
}

// AuthMiddleware is an authentication middleware
func AuthMiddleware(validator AuthValidator) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			// Extract auth info from context or meta
			authInfo := extractAuthInfo(ctx, req)

			// Validate permissions
			if !validator.Validate(authInfo, req.Params.Name) {
				return nil, UnauthorizedError(
					fmt.Sprintf("not authorized to call tool %s", req.Params.Name),
					WithDetail("tool", req.Params.Name),
				)
			}

			return next(ctx, req)
		}
	}
}

// AuthValidator is the authentication validator interface
type AuthValidator interface {
	Validate(authInfo interface{}, tool string) bool
}

// extractAuthInfo extracts auth info from the request (can be from ctx or req.Params.Meta)
func extractAuthInfo(ctx context.Context, req *CallToolRequest) interface{} {
	if req.Params.Meta != nil {
		if auth, ok := req.Params.Meta["auth"]; ok {
			return auth
		}
	}
	return nil
}

// RetryMiddleware is a retry middleware
func RetryMiddleware(maxRetries int, shouldRetry func(error) bool) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			var lastErr error
			var result *protocol.CallToolResult

			for attempt := 0; attempt <= maxRetries; attempt++ {
				result, lastErr = next(ctx, req)

				// If success or should not retry, return directly
				if lastErr == nil || !shouldRetry(lastErr) {
					return result, lastErr
				}

				if attempt < maxRetries {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
						// Exponential backoff
					}
				}
			}

			return result, lastErr
		}
	}
}

// ValidationMiddleware is a parameter validation middleware
func ValidationMiddleware(validator ParamsValidator) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			if err := validator.Validate(req.Params.Name, req.Params.Arguments); err != nil {
				return nil, InvalidParamsError(
					err.Error(),
					WithDetail("tool", req.Params.Name),
				)
			}

			return next(ctx, req)
		}
	}
}

type ParamsValidator interface {
	Validate(tool string, arguments map[string]any) error
}

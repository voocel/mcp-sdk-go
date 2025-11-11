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

// Use 添加中间件到 Server 中间件按添加顺序执行（洋葱模型）
func (s *Server) Use(middleware ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.middlewares = append(s.middlewares, middleware...)

	for name, st := range s.tools {
		wrappedHandler := applyMiddleware(st.handler, middleware)
		s.tools[name].handler = wrappedHandler
	}
}

// applyMiddleware 应用中间件链
func applyMiddleware(handler ToolHandler, middlewares []Middleware) ToolHandler {
	// 从后向前应用中间件（形成洋葱模型）
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// LoggingMiddleware 日志中间件
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

// RecoveryMiddleware 恢复中间件
func RecoveryMiddleware() Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (result *protocol.CallToolResult, err error) {
			defer func() {
				if r := recover(); r != nil {
					// 捕获 panic
					stack := debug.Stack()
					err = fmt.Errorf("panic recovered: %v\n%s", r, stack)
					result = ErrorResult("Internal server error", err)
				}
			}()

			return next(ctx, req)
		}
	}
}

// TimeoutMiddleware 超时中间件
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			// 创建带超时的 context
			timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			// 使用 channel 接收结果
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

// MetricsMiddleware 指标中间件
func MetricsMiddleware(collector MetricsCollector) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			start := time.Now()
			toolName := req.Params.Name

			result, err := next(ctx, req)

			duration := time.Since(start)

			// 记录指标
			collector.RecordToolCall(toolName, duration, err == nil)

			return result, err
		}
	}
}

// MetricsCollector 指标收集器接口
type MetricsCollector interface {
	RecordToolCall(tool string, duration time.Duration, success bool)
}

// RateLimitMiddleware 速率限制中间件
func RateLimitMiddleware(limiter RateLimiter) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			toolName := req.Params.Name

			// 检查速率限制
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

// RateLimiter 速率限制器接口
type RateLimiter interface {
	Allow(tool string) bool
}

// AuthMiddleware 认证中间件
func AuthMiddleware(validator AuthValidator) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			// 从 context 或 meta 中提取认证信息
			authInfo := extractAuthInfo(ctx, req)

			// 验证权限
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

// AuthValidator 认证验证器接口
type AuthValidator interface {
	Validate(authInfo interface{}, tool string) bool
}

// extractAuthInfo 从请求中提取认证信息(可以从ctx或从req.Params.Meta中提取)
func extractAuthInfo(ctx context.Context, req *CallToolRequest) interface{} {
	if req.Params.Meta != nil {
		if auth, ok := req.Params.Meta["auth"]; ok {
			return auth
		}
	}
	return nil
}

// RetryMiddleware 重试中间件
func RetryMiddleware(maxRetries int, shouldRetry func(error) bool) Middleware {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
			var lastErr error
			var result *protocol.CallToolResult

			for attempt := 0; attempt <= maxRetries; attempt++ {
				result, lastErr = next(ctx, req)

				// 如果成功或不应重试，直接返回
				if lastErr == nil || !shouldRetry(lastErr) {
					return result, lastErr
				}

				if attempt < maxRetries {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
						// 指数退避
					}
				}
			}

			return result, lastErr
		}
	}
}

// ValidationMiddleware 参数验证中间件
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

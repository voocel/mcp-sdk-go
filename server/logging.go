package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// slog level to MCP LoggingLevel mapping constants.
const (
	LevelDebug     = slog.LevelDebug                        // debug
	LevelInfo      = slog.LevelInfo                         // info
	LevelNotice    = (slog.LevelInfo + slog.LevelWarn) / 2  // notice
	LevelWarning   = slog.LevelWarn                         // warning
	LevelError     = slog.LevelError                        // error
	LevelCritical  = slog.LevelError + 4                    // critical
	LevelAlert     = slog.LevelError + 8                    // alert
	LevelEmergency = slog.LevelError + 12                   // emergency
)

var slogToMCP = map[slog.Level]protocol.LoggingLevel{
	LevelDebug:     protocol.LogLevelDebug,
	LevelInfo:      protocol.LogLevelInfo,
	LevelNotice:    protocol.LogLevelNotice,
	LevelWarning:   protocol.LogLevelWarning,
	LevelError:     protocol.LogLevelError,
	LevelCritical:  protocol.LogLevelCritical,
	LevelAlert:     protocol.LogLevelAlert,
	LevelEmergency: protocol.LogLevelEmergency,
}

var mcpToSlog map[protocol.LoggingLevel]slog.Level

func init() {
	mcpToSlog = make(map[protocol.LoggingLevel]slog.Level, len(slogToMCP))
	for sl, ml := range slogToMCP {
		mcpToSlog[ml] = sl
	}
}

// SlogLevelToMCP converts a slog level to MCP LoggingLevel using range matching.
func SlogLevelToMCP(sl slog.Level) protocol.LoggingLevel {
	switch {
	case sl >= LevelEmergency:
		return protocol.LogLevelEmergency
	case sl >= LevelAlert:
		return protocol.LogLevelAlert
	case sl >= LevelCritical:
		return protocol.LogLevelCritical
	case sl >= LevelError:
		return protocol.LogLevelError
	case sl >= LevelWarning:
		return protocol.LogLevelWarning
	case sl >= LevelNotice:
		return protocol.LogLevelNotice
	case sl >= LevelInfo:
		return protocol.LogLevelInfo
	default:
		return protocol.LogLevelDebug
	}
}

// MCPLevelToSlog converts an MCP LoggingLevel to a slog level.
func MCPLevelToSlog(ll protocol.LoggingLevel) slog.Level {
	if sl, ok := mcpToSlog[ll]; ok {
		return sl
	}
	return LevelDebug
}

// LoggingHandlerOptions configures a LoggingHandler.
type LoggingHandlerOptions struct {
	// LoggerName sets the "logger" field in log notifications.
	LoggerName string
	// MinInterval rate-limits log message sending; messages exceeding the rate are dropped.
	// Zero means no rate limiting.
	MinInterval time.Duration
}

// LoggingHandler implements slog.Handler, bridging Go slog to MCP log notifications.
//
// Usage:
//
//	handler := NewLoggingHandler(session, &LoggingHandlerOptions{LoggerName: "myapp"})
//	logger := slog.New(handler)
//	logger.Info("something happened", "key", "value")
//	// automatically sends MCP notifications/message via session.Log()
type LoggingHandler struct {
	opts     LoggingHandlerOptions
	ss       *ServerSession
	mu       *sync.Mutex   // pointer so WithAttrs/WithGroup clones share the lock
	lastSent time.Time
	buf      *bytes.Buffer
	handler  slog.Handler  // internal JSON handler for serialization
}

// NewLoggingHandler creates a LoggingHandler that sends logs to the client via the given ServerSession.
func NewLoggingHandler(ss *ServerSession, opts *LoggingHandlerOptions) *LoggingHandler {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
	})
	lh := &LoggingHandler{
		ss:      ss,
		mu:      new(sync.Mutex),
		buf:     &buf,
		handler: jsonHandler,
	}
	if opts != nil {
		lh.opts = *opts
	}
	return lh
}

// Enabled reports whether the handler is enabled for the given level by comparing against the session's MCP log level.
func (h *LoggingHandler) Enabled(_ context.Context, level slog.Level) bool {
	h.ss.mu.Lock()
	mcpLevel := h.ss.state.LogLevel
	h.ss.mu.Unlock()
	if mcpLevel == "" {
		return false
	}
	return level >= MCPLevelToSlog(mcpLevel)
}

// WithAttrs implements slog.Handler.
func (h *LoggingHandler) WithAttrs(as []slog.Attr) slog.Handler {
	h2 := *h
	h2.handler = h.handler.WithAttrs(as)
	return &h2
}

// WithGroup implements slog.Handler.
func (h *LoggingHandler) WithGroup(name string) slog.Handler {
	h2 := *h
	h2.handler = h.handler.WithGroup(name)
	return &h2
}

// Handle serializes the slog.Record to JSON and sends it as an MCP notification via ServerSession.Log().
func (h *LoggingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Rate limit check: eagerly update lastSent to prevent concurrent bypass.
	h.mu.Lock()
	if h.opts.MinInterval > 0 && time.Since(h.lastSent) < h.opts.MinInterval {
		h.mu.Unlock()
		return nil
	}
	h.lastSent = time.Now()
	h.mu.Unlock()

	// Serialize log record to JSON (buffer operations must be atomic).
	var data json.RawMessage
	var err error
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.buf.Reset()
		err = h.handler.Handle(ctx, r)
		data = json.RawMessage(slices.Clone(h.buf.Bytes()))
	}()
	if err != nil {
		return err
	}

	params := &protocol.LoggingMessageParams{
		Logger: h.opts.LoggerName,
		Level:  SlogLevelToMCP(r.Level),
		Data:   data,
	}
	return h.ss.Log(ctx, params)
}

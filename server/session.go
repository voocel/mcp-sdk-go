package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// ServerSession represents a server session, one ServerSession per client connection
type ServerSession struct {
	calledOnClose atomic.Bool
	onClose       func()

	server *Server
	conn   Connection // Underlying connection (from transport)

	// keepalive
	keepaliveCancel context.CancelFunc

	mu              sync.Mutex
	state           ServerSessionState
	waitErr         chan error
	pendingRequests map[string]context.CancelFunc // Track pending requests for cancellation
}

// ServerSessionState represents session state
type ServerSessionState struct {
	// InitializeParams are the parameters from the initialize request
	InitializeParams *protocol.InitializeParams

	// InitializedParams are the parameters from notifications/initialized
	InitializedParams *protocol.InitializedParams

	// LogLevel is the logging level
	LogLevel protocol.LoggingLevel
}

// Connection represents the underlying transport connection
type Connection interface {
	// SendNotification sends a notification to the client
	SendNotification(ctx context.Context, method string, params interface{}) error

	// SendRequest sends a request to the client and waits for a response
	SendRequest(ctx context.Context, method string, params interface{}, result interface{}) error

	Close() error

	SessionID() string
}

func (ss *ServerSession) ID() string {
	if ss.conn != nil {
		return ss.conn.SessionID()
	}
	return ""
}

func (ss *ServerSession) sameSession(id string) bool {
	if id == "" {
		return true
	}
	if ss == nil {
		return false
	}
	return ss.ID() == id
}

func (ss *ServerSession) Close() error {
	if ss.keepaliveCancel != nil {
		ss.keepaliveCancel()
	}

	// Cancel all pending requests
	ss.mu.Lock()
	pendingRequests := ss.pendingRequests
	ss.pendingRequests = make(map[string]context.CancelFunc)
	ss.mu.Unlock()

	for _, cancel := range pendingRequests {
		cancel()
	}

	if ss.calledOnClose.CompareAndSwap(false, true) {
		if ss.onClose != nil {
			ss.onClose()
		}
	}
	if ss.conn != nil {
		return ss.conn.Close()
	}
	return nil
}

// Wait waits for the session to end and returns the error that caused it to end
func (ss *ServerSession) Wait() error {
	if ss.waitErr == nil {
		return nil
	}
	return <-ss.waitErr
}

// updateState updates the session state
func (ss *ServerSession) updateState(mut func(*ServerSessionState)) {
	ss.mu.Lock()
	mut(&ss.state)
	ss.mu.Unlock()
}

// hasInitialized checks if the initialized notification has been received
func (ss *ServerSession) hasInitialized() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.state.InitializedParams != nil
}

// NotifyProgress sends a progress notification to the client
func (ss *ServerSession) NotifyProgress(ctx context.Context, params *protocol.ProgressNotificationParams) error {
	if params == nil {
		return ss.conn.SendNotification(ctx, protocol.NotificationProgress, params)
	}
	if taskID, ok := taskIDFromContext(ctx); ok {
		copied := *params
		copied.Meta = mergeMap(copied.Meta, relatedTaskMeta(taskID))
		return ss.conn.SendNotification(ctx, protocol.NotificationProgress, &copied)
	}
	return ss.conn.SendNotification(ctx, protocol.NotificationProgress, params)
}

// Log sends a log message to the client
func (ss *ServerSession) Log(ctx context.Context, params *protocol.LoggingMessageParams) error {
	ss.mu.Lock()
	logLevel := ss.state.LogLevel
	ss.mu.Unlock()

	// If the client has not set a log level, do not send logs
	if logLevel == "" {
		return nil
	}

	// Filter by log level
	if !protocol.ShouldLog(params.Level, logLevel) {
		return nil
	}

	if params == nil {
		return ss.conn.SendNotification(ctx, protocol.NotificationLoggingMessage, params)
	}
	if taskID, ok := taskIDFromContext(ctx); ok {
		copied := *params
		copied.Meta = mergeMap(copied.Meta, relatedTaskMeta(taskID))
		return ss.conn.SendNotification(ctx, protocol.NotificationLoggingMessage, &copied)
	}
	return ss.conn.SendNotification(ctx, protocol.NotificationLoggingMessage, params)
}

// Ping sends a ping request to the client
func (ss *ServerSession) Ping(ctx context.Context) error {
	return ss.conn.SendRequest(ctx, protocol.MethodPing, &protocol.PingParams{}, &protocol.EmptyResult{})
}

// ListRoots lists the client's root directories
func (ss *ServerSession) ListRoots(ctx context.Context) (*protocol.ListRootsResult, error) {
	var result protocol.ListRootsResult
	err := ss.conn.SendRequest(ctx, protocol.MethodRootsList, &protocol.ListRootsParams{}, &result)
	return &result, err
}

// CreateMessage sends a sampling request to the client
func (ss *ServerSession) CreateMessage(ctx context.Context, params *protocol.CreateMessageParams) (*protocol.CreateMessageResult, error) {
	var result protocol.CreateMessageResult
	sendParams := any(params)
	if params != nil {
		if taskID, ok := taskIDFromContext(ctx); ok {
			copied := *params
			copied.Meta = mergeMap(copied.Meta, relatedTaskMeta(taskID))
			sendParams = &copied
		}
	}
	err := ss.conn.SendRequest(ctx, protocol.MethodSamplingCreateMessage, sendParams, &result)
	return &result, err
}

// Elicit sends an elicitation request to the client, requesting user input
func (ss *ServerSession) Elicit(ctx context.Context, params *protocol.ElicitationCreateParams) (*protocol.ElicitationResult, error) {
	var result protocol.ElicitationResult
	sendParams := any(params)
	if params != nil {
		if taskID, ok := taskIDFromContext(ctx); ok {
			// protocol.ElicitationCreateParams currently doesn't include _meta,
			// but task-related messages must include related-task metadata.
			wrapped := struct {
				Meta map[string]any `json:"_meta,omitempty"`
				*protocol.ElicitationCreateParams
			}{
				Meta:                    relatedTaskMeta(taskID),
				ElicitationCreateParams: params,
			}
			sendParams = &wrapped
		}
	}
	err := ss.conn.SendRequest(ctx, protocol.MethodElicitationCreate, sendParams, &result)
	return &result, err
}

// InitializeParams returns the initialization parameters
func (ss *ServerSession) InitializeParams() *protocol.InitializeParams {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.state.InitializeParams
}

// CallToolRequest represents a tool call request, allowing tool handlers to send notifications
type CallToolRequest struct {
	// Session is the current session
	Session *ServerSession

	// Params are the original parameters
	Params *protocol.CallToolParams
}

// ToolHandler is a tool handler function.
// It receives a CallToolRequest and can send notifications via req.Session.
type ToolHandler func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error)

// ========== connAdapter: Adapts transport.Connection to server.Connection ==========

// pendingRequest represents a pending request
type pendingRequest struct {
	method   string
	response chan *protocol.JSONRPCMessage
	err      chan error
}

// connAdapter adapts transport.Connection to server.Connection
type connAdapter struct {
	conn transport.Connection

	mu      sync.Mutex
	pending map[string]*pendingRequest
	nextID  int64
}

func newConnAdapter(conn transport.Connection) *connAdapter {
	return &connAdapter{
		conn:    conn,
		pending: make(map[string]*pendingRequest),
	}
}

func (a *connAdapter) SendNotification(ctx context.Context, method string, params interface{}) error {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}

	msg := &protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  json.RawMessage(paramsBytes),
	}

	return a.conn.Write(ctx, msg)
}

func (a *connAdapter) SendRequest(ctx context.Context, method string, params interface{}, result interface{}) error {
	a.mu.Lock()
	a.nextID++
	id := strconv.FormatInt(a.nextID, 10)
	a.mu.Unlock()

	idJSON, _ := json.Marshal(id)
	msg := &protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      idJSON,
		Method:  method,
	}

	if params != nil {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params: %w", err)
		}
		msg.Params = paramsJSON
	}

	pending := &pendingRequest{
		method:   method,
		response: make(chan *protocol.JSONRPCMessage, 1),
		err:      make(chan error, 1),
	}

	a.mu.Lock()
	a.pending[id] = pending
	a.mu.Unlock()

	if err := a.conn.Write(ctx, msg); err != nil {
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		return fmt.Errorf("failed to write request: %w", err)
	}

	select {
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		return ctx.Err()
	case err := <-pending.err:
		return err
	case resp := <-pending.response:
		if resp.Error != nil {
			return fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		if result != nil && resp.Result != nil {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("failed to unmarshal result: %w", err)
			}
		}

		return nil
	}
}

func (a *connAdapter) Close() error {
	// Clean up all pending requests
	a.mu.Lock()
	pending := a.pending
	a.pending = make(map[string]*pendingRequest)
	a.mu.Unlock()

	// Notify all pending requests that the connection is closed
	for _, req := range pending {
		select {
		case req.err <- fmt.Errorf("connection closed"):
		default:
		}
	}

	return a.conn.Close()
}

// handleResponse handles response messages from the client
func (a *connAdapter) handleResponse(msg *protocol.JSONRPCMessage) {
	if msg.ID == nil {
		return
	}

	var id string
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return
	}

	a.mu.Lock()
	pending, ok := a.pending[id]
	if ok {
		delete(a.pending, id)
	}
	a.mu.Unlock()

	if !ok {
		return
	}

	if msg.Error != nil {
		pending.err <- fmt.Errorf("RPC error %d: %s", msg.Error.Code, msg.Error.Message)
	} else {
		pending.response <- msg
	}
}

func (a *connAdapter) SessionID() string {
	return a.conn.SessionID()
}

// startKeepalive starts the keepalive mechanism
func (ss *ServerSession) startKeepalive(interval time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	ss.keepaliveCancel = cancel

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(ctx, interval)
				err := ss.Ping(pingCtx)
				cancel()

				if err != nil {
					// Ping failed, close the connection
					_ = ss.Close()
					return
				}
			}
		}
	}()
}

package client

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

type ClientInfo struct {
	Name    string
	Version string
}

// ClientOptions configures client behavior
type ClientOptions struct {
	// CreateMessageHandler handles sampling/createMessage requests from the server
	//
	// Setting this to a non-nil value causes the client to declare sampling capability
	CreateMessageHandler func(context.Context, *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error)

	// ElicitationHandler handles elicitation/create requests from the server
	//
	// Setting this to a non-nil value causes the client to declare elicitation capability
	ElicitationHandler func(context.Context, *protocol.ElicitationCreateParams) (*protocol.ElicitationResult, error)

	// Notification handlers from server
	ToolListChangedHandler      func(context.Context, *protocol.ToolsListChangedNotification)
	PromptListChangedHandler    func(context.Context, *protocol.PromptListChangedParams)
	ResourceListChangedHandler  func(context.Context, *protocol.ResourceListChangedParams)
	ResourceUpdatedHandler      func(context.Context, *protocol.ResourceUpdatedNotificationParams)
	LoggingMessageHandler       func(context.Context, *protocol.LoggingMessageParams)
	ProgressNotificationHandler func(context.Context, *protocol.ProgressNotificationParams)

	// TaskStatusHandler handles notifications/tasks/status from the server (MCP 2025-11-25)
	TaskStatusHandler func(context.Context, *protocol.TaskStatusNotificationParams)

	// KeepAlive defines the interval for periodic "ping" requests
	// If the peer fails to respond to a keepalive-initiated ping, the session will automatically close
	KeepAlive time.Duration

	// Tasks capability options (MCP 2025-11-25)
	TasksEnabled bool // Enable tasks support for sampling and elicitation

	// SamplingToolsEnabled enables tool use in sampling requests (MCP 2025-11-25)
	SamplingToolsEnabled bool
}

type Client struct {
	info     *ClientInfo
	opts     ClientOptions
	mu       sync.Mutex
	roots    []*protocol.Root
	sessions []*ClientSession
}

func NewClient(info *ClientInfo, opts *ClientOptions) *Client {
	if info == nil {
		panic("nil ClientInfo")
	}
	c := &Client{
		info:  info,
		roots: make([]*protocol.Root, 0),
	}
	if opts != nil {
		c.opts = *opts
	}
	return c
}

type ClientSessionOptions struct{}

// capabilities returns the client's capability declaration
func (c *Client) capabilities() *protocol.ClientCapabilities {
	caps := &protocol.ClientCapabilities{
		Roots: &protocol.RootsCapability{
			ListChanged: true,
		},
	}
	if c.opts.CreateMessageHandler != nil {
		caps.Sampling = &protocol.SamplingCapability{}
		// Add tool use support if enabled (MCP 2025-11-25)
		if c.opts.SamplingToolsEnabled {
			caps.Sampling.Tools = &struct{}{}
		}
	}
	if c.opts.ElicitationHandler != nil {
		caps.Elicitation = &protocol.ElicitationCapability{}
	}
	// Add Tasks capability (MCP 2025-11-25)
	if c.opts.TasksEnabled {
		caps.Tasks = &protocol.ClientTasksCapability{
			List:   &struct{}{},
			Cancel: &struct{}{},
		}
		if c.opts.CreateMessageHandler != nil || c.opts.ElicitationHandler != nil {
			caps.Tasks.Requests = &protocol.ClientTaskRequestsCapability{}
			if c.opts.CreateMessageHandler != nil {
				caps.Tasks.Requests.Sampling = &protocol.SamplingTaskCapability{
					CreateMessage: &struct{}{},
				}
			}
			if c.opts.ElicitationHandler != nil {
				caps.Tasks.Requests.Elicitation = &protocol.ElicitationTaskCapability{
					Create: &struct{}{},
				}
			}
		}
	}
	return caps
}

// Connect starts an MCP session via the given transport
// The returned session is initialized and ready to use
//
// Typically, the client is responsible for closing the connection when no longer needed
// However, if the connection is closed by the server, calls or notifications will return errors wrapping ErrConnectionClosed
func (c *Client) Connect(ctx context.Context, t transport.Transport, _ *ClientSessionOptions) (*ClientSession, error) {
	conn, err := t.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("transport connect failed: %w", err)
	}

	cs := &ClientSession{
		conn:             conn,
		client:           c,
		waitErr:          make(chan error, 1),
		pending:          make(map[string]*pendingRequest),
		incomingRequests: make(map[string]context.CancelFunc),
	}

	c.mu.Lock()
	c.sessions = append(c.sessions, cs)
	c.mu.Unlock()

	go func() {
		err := cs.handleMessages(ctx)
		cs.waitErr <- err
		close(cs.waitErr)
	}()

	// Perform initialization handshake
	initParams := &protocol.InitializeParams{
		ProtocolVersion: protocol.MCPVersion,
		ClientInfo: protocol.ClientInfo{
			Name:    c.info.Name,
			Version: c.info.Version,
		},
		Capabilities: *c.capabilities(),
	}

	var initResult protocol.InitializeResult
	if err := cs.sendRequest(ctx, protocol.MethodInitialize, initParams, &initResult); err != nil {
		_ = cs.Close()
		return nil, fmt.Errorf("initialize failed: %w", err)
	}

	if !protocol.IsVersionSupported(initResult.ProtocolVersion) {
		_ = cs.Close()
		return nil, fmt.Errorf("unsupported protocol version: %s (supported: %v)",
			initResult.ProtocolVersion, protocol.GetSupportedVersions())
	}

	cs.state.InitializeResult = &initResult

	if updater, ok := conn.(interface {
		SessionUpdated(*protocol.InitializeResult)
	}); ok {
		updater.SessionUpdated(&initResult)
	}

	if err := cs.sendNotification(ctx, protocol.NotificationInitialized, &protocol.InitializedParams{}); err != nil {
		_ = cs.Close()
		return nil, fmt.Errorf("send initialized notification failed: %w", err)
	}

	if c.opts.KeepAlive > 0 {
		cs.startKeepalive(c.opts.KeepAlive)
	}

	return cs, nil
}

// AddRoot adds a root directory and notifies all sessions
func (c *Client) AddRoot(root *protocol.Root) {
	c.mu.Lock()
	c.roots = append(c.roots, root)
	sessions := make([]*ClientSession, len(c.sessions))
	copy(sessions, c.sessions)
	c.mu.Unlock()

	// Notify all sessions that the roots list has changed
	for _, cs := range sessions {
		_ = cs.NotifyRootsListChanged(context.Background())
	}
}

// RemoveRoot removes a root directory and notifies all sessions
func (c *Client) RemoveRoot(uri string) {
	c.mu.Lock()
	var changed bool
	for i, root := range c.roots {
		if root.URI == uri {
			c.roots = append(c.roots[:i], c.roots[i+1:]...)
			changed = true
			break
		}
	}
	sessions := make([]*ClientSession, len(c.sessions))
	copy(sessions, c.sessions)
	c.mu.Unlock()

	// Only notify if the roots actually changed
	if changed {
		for _, cs := range sessions {
			_ = cs.NotifyRootsListChanged(context.Background())
		}
	}
}

// ListRoots lists all root directories
func (c *Client) ListRoots() []*protocol.Root {
	c.mu.Lock()
	defer c.mu.Unlock()
	roots := make([]*protocol.Root, len(c.roots))
	copy(roots, c.roots)
	return roots
}

// ClientSession is a logical connection to an MCP server
// Can be used to send requests or notifications to the server
// Sessions are created by calling Client.Connect
//
// Call ClientSession.Close to close the connection, or use ClientSession.Wait to wait for server termination
type ClientSession struct {
	// Ensure onClose is called at most once
	calledOnClose atomic.Bool
	onClose       func()

	conn    transport.Connection
	client  *Client
	waitErr chan error

	// keepalive
	keepaliveCancel context.CancelFunc

	// Session state
	state clientSessionState

	// Pending requests
	mu               sync.Mutex
	pending          map[string]*pendingRequest    // Requests sent by client
	incomingRequests map[string]context.CancelFunc // Requests sent by server (for cancellation)
	nextID           int64
}

type clientSessionState struct {
	InitializeResult *protocol.InitializeResult
}

type pendingRequest struct {
	method   string
	response chan *protocol.JSONRPCMessage
	err      chan error
}

// InitializeResult returns the initialization result
func (cs *ClientSession) InitializeResult() *protocol.InitializeResult {
	return cs.state.InitializeResult
}

func (cs *ClientSession) ID() string {
	return cs.conn.SessionID()
}

func (cs *ClientSession) Close() error {
	if cs.keepaliveCancel != nil {
		cs.keepaliveCancel()
	}

	// Clean up all pending requests (before closing connection)
	cs.mu.Lock()
	pending := cs.pending
	cs.pending = make(map[string]*pendingRequest)
	incomingRequests := cs.incomingRequests
	cs.incomingRequests = make(map[string]context.CancelFunc)
	cs.mu.Unlock()

	// Notify all client-initiated requests that connection is closed
	for _, req := range pending {
		select {
		case req.err <- fmt.Errorf("connection closed"):
		default:
		}
	}

	// Cancel all server-initiated requests currently being processed
	for _, cancel := range incomingRequests {
		cancel()
	}

	err := cs.conn.Close()

	if cs.onClose != nil && cs.calledOnClose.CompareAndSwap(false, true) {
		cs.onClose()
	}

	cs.client.mu.Lock()
	for i, s := range cs.client.sessions {
		if s == cs {
			cs.client.sessions = append(cs.client.sessions[:i], cs.client.sessions[i+1:]...)
			break
		}
	}
	cs.client.mu.Unlock()

	return err
}

// Wait waits for the connection to be closed by the server. Typically, the client should be responsible for closing the connection
func (cs *ClientSession) Wait() error {
	return <-cs.waitErr
}

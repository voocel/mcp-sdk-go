package streamable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// StreamableClientTransport connects to a Streamable HTTP MCP endpoint.
type StreamableClientTransport struct {
	Endpoint   string
	HTTPClient *http.Client
	MaxRetries int
}

var errSessionMissing = errors.New("session not found")

type ClientOption func(*StreamableClientTransport)

// WithHTTPClient sets the HTTP client used by the transport.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(t *StreamableClientTransport) {
		t.HTTPClient = client
	}
}

// WithMaxRetries sets the maximum number of SSE reconnect attempts.
func WithMaxRetries(n int) ClientOption {
	return func(t *StreamableClientTransport) {
		t.MaxRetries = n
	}
}

// NewStreamableClientTransport creates a StreamableClientTransport.
func NewStreamableClientTransport(endpoint string, options ...ClientOption) (*StreamableClientTransport, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	if _, err := url.Parse(endpoint); err != nil {
		return nil, fmt.Errorf("invalid endpoint: %w", err)
	}
	t := &StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: http.DefaultClient,
		MaxRetries: 5,
	}
	for _, opt := range options {
		opt(t)
	}
	return t, nil
}

func (t *StreamableClientTransport) Connect(ctx context.Context) (transport.Connection, error) {
	client := t.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	maxRetries := t.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	connCtx, cancel := context.WithCancel(detachContext(ctx))
	return &streamableClientConn{
		endpoint:   t.Endpoint,
		client:     client,
		incoming:   make(chan *protocol.JSONRPCMessage, 10),
		done:       make(chan struct{}),
		failed:     make(chan struct{}),
		maxRetries: maxRetries,
		ctx:        connCtx,
		cancel:     cancel,
	}, nil
}

type streamableClientConn struct {
	endpoint string
	client   *http.Client
	ctx      context.Context
	cancel   context.CancelFunc

	incoming   chan *protocol.JSONRPCMessage
	maxRetries int
	done       chan struct{}

	failOnce sync.Once
	failErr  error
	failed   chan struct{}

	closeOnce sync.Once
	closeErr  error

	mu          sync.Mutex
	initResult  *protocol.InitializeResult
	sessionID   string
	initialized atomic.Bool
}

func (c *streamableClientConn) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

func (c *streamableClientConn) SessionUpdated(result *protocol.InitializeResult) {
	c.mu.Lock()
	c.initResult = result
	c.mu.Unlock()
	if c.initialized.CompareAndSwap(false, true) {
		c.connectStandaloneSSE()
	}
}

func (c *streamableClientConn) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	if err := c.failure(); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.failed:
		return nil, c.failure()
	case <-c.done:
		return nil, transport.ErrConnectionClosed
	case msg := <-c.incoming:
		return msg, nil
	}
}

func (c *streamableClientConn) Write(ctx context.Context, msg *protocol.JSONRPCMessage) error {
	if err := c.failure(); err != nil {
		return err
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	c.setMCPHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		c.fail(errSessionMissing)
		return errSessionMissing
	}
	if isTransientHTTPStatus(resp.StatusCode) {
		resp.Body.Close()
		return fmt.Errorf("transient error: %s", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		err := fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		c.fail(err)
		return err
	}

	if sessionID := resp.Header.Get(MCPSessionIDHeader); sessionID != "" {
		c.mu.Lock()
		prev := c.sessionID
		if prev == "" {
			c.sessionID = sessionID
		}
		c.mu.Unlock()
		if prev != "" && prev != sessionID {
			resp.Body.Close()
			err := fmt.Errorf("mismatching session IDs %q and %q", prev, sessionID)
			c.fail(err)
			return err
		}
	}

	isCall := msg.Method != "" && msg.ID != nil
	if !isCall {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
			return nil
		}
		body, _ := io.ReadAll(resp.Body)
		if len(bytes.TrimSpace(body)) == 0 {
			return nil
		}
		var response protocol.JSONRPCMessage
		if err := json.Unmarshal(body, &response); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
		c.sendIncoming(&response)
		return nil
	}

	contentType := strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0])
	switch contentType {
	case "application/json", "":
		go c.handleJSON(resp)
	case "text/event-stream":
		forCallID := msg.GetIDString()
		go c.handleSSE(ctx, "streamable response", resp, forCallID)
	default:
		resp.Body.Close()
		return fmt.Errorf("unsupported content type %q", contentType)
	}
	return nil
}

func (c *streamableClientConn) Close() error {
	c.closeOnce.Do(func() {
		req, err := http.NewRequestWithContext(c.ctx, http.MethodDelete, c.endpoint, nil)
		if err != nil {
			c.closeErr = err
		} else {
			c.setMCPHeaders(req)
			if _, err := c.client.Do(req); err != nil && !errors.Is(err, context.Canceled) {
				c.closeErr = err
			}
		}
		c.cancel()
		close(c.done)
	})
	return c.closeErr
}

func (c *streamableClientConn) connectStandaloneSSE() {
	resp, err := c.connectSSE(c.ctx, "", 0, true)
	if err != nil {
		if c.ctx.Err() == nil {
			c.fail(fmt.Errorf("standalone SSE request failed: %w", err))
		}
		return
	}
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return
	}
	if err := c.checkResponse("standalone SSE stream", resp); err != nil {
		c.fail(err)
		return
	}
	go c.handleSSE(c.ctx, "standalone SSE stream", resp, "")
}

func (c *streamableClientConn) handleJSON(resp *http.Response) {
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		c.fail(fmt.Errorf("failed to read response body: %w", err))
		return
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return
	}
	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		c.fail(fmt.Errorf("failed to decode response: %w", err))
		return
	}
	c.sendIncoming(&msg)
}

func (c *streamableClientConn) handleSSE(ctx context.Context, summary string, resp *http.Response, forCallID string) {
	for {
		lastEventID, retryDelay, clientClosed, gotResponse := c.processStream(ctx, summary, resp, forCallID)
		if clientClosed {
			return
		}
		if forCallID != "" && gotResponse {
			return
		}
		if forCallID != "" && lastEventID == "" {
			return
		}
		newResp, err := c.connectSSE(ctx, lastEventID, retryDelay, false)
		if err != nil {
			if ctx.Err() == nil {
				c.fail(fmt.Errorf("%s: failed to reconnect: %w", summary, err))
			}
			return
		}
		if err := c.checkResponse(summary, newResp); err != nil {
			c.fail(err)
			return
		}
		resp = newResp
	}
}

func (c *streamableClientConn) processStream(ctx context.Context, summary string, resp *http.Response, forCallID string) (lastEventID string, retryDelay time.Duration, clientClosed bool, gotResponse bool) {
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	scanEvents(resp.Body, func(evt Event, err error) bool {
		if err != nil {
			if ctx.Err() != nil {
				clientClosed = true
			}
			return false
		}
		if evt.ID != "" {
			lastEventID = evt.ID
		}
		if evt.Retry != "" {
			if n, err := parseRetry(evt.Retry); err == nil {
				retryDelay = n
			}
		}
		if evt.Name != "" && evt.Name != "message" {
			return true
		}
		if len(evt.Data) == 0 {
			return true
		}

		var msg protocol.JSONRPCMessage
		if err := json.Unmarshal(evt.Data, &msg); err != nil {
			c.fail(fmt.Errorf("%s: failed to decode event: %w", summary, err))
			clientClosed = true
			return false
		}
		c.sendIncoming(&msg)
		if forCallID != "" && msg.ID != nil && protocol.IDToString(msg.ID) == forCallID && (msg.Result != nil || msg.Error != nil) {
			gotResponse = true
			return false
		}
		return true
	})

	if ctx.Err() != nil {
		return lastEventID, retryDelay, true, gotResponse
	}
	if forCallID != "" && !gotResponse && lastEventID == "" {
		c.sendIncoming(&protocol.JSONRPCMessage{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.StringToID(forCallID),
			Error: &protocol.JSONRPCError{
				Code:    protocol.InternalError,
				Message: "request terminated without response",
			},
		})
	}
	return lastEventID, retryDelay, false, gotResponse
}

func (c *streamableClientConn) connectSSE(ctx context.Context, lastEventID string, retryDelay time.Duration, initial bool) (*http.Response, error) {
	var finalErr error
	attempt := 0
	if !initial {
		attempt = 1
	}
	delay := calculateReconnectDelay(attempt)
	if retryDelay > 0 {
		delay = retryDelay
	}
	for ; attempt <= c.maxRetries; attempt++ {
		select {
		case <-c.done:
			return nil, fmt.Errorf("connection closed by client during reconnect")
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Accept", "text/event-stream")
			c.setMCPHeaders(req)
			if lastEventID != "" {
				req.Header.Set(LastEventIDHeader, lastEventID)
			}
			resp, err := c.client.Do(req)
			if err != nil {
				finalErr = err
				delay = calculateReconnectDelay(attempt + 1)
				continue
			}
			return resp, nil
		}
	}
	if finalErr != nil {
		return nil, fmt.Errorf("connection failed after %d attempts: %w", c.maxRetries, finalErr)
	}
	return nil, fmt.Errorf("connection aborted after %d attempts", c.maxRetries)
}

func (c *streamableClientConn) checkResponse(summary string, resp *http.Response) error {
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return errSessionMissing
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("%s: %s: %s", summary, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *streamableClientConn) setMCPHeaders(req *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.initResult != nil {
		req.Header.Set(MCPProtocolVersionHeader, c.initResult.ProtocolVersion)
	} else {
		req.Header.Set(MCPProtocolVersionHeader, protocol.MCPVersion)
	}
	if c.sessionID != "" {
		req.Header.Set(MCPSessionIDHeader, c.sessionID)
	}
}

func (c *streamableClientConn) sendIncoming(msg *protocol.JSONRPCMessage) {
	select {
	case c.incoming <- msg:
	case <-c.done:
	}
}

func (c *streamableClientConn) fail(err error) {
	if err == nil {
		return
	}
	c.failOnce.Do(func() {
		c.failErr = err
		close(c.failed)
	})
}

func (c *streamableClientConn) failure() error {
	select {
	case <-c.failed:
		return c.failErr
	default:
		return nil
	}
}

func parseRetry(value string) (time.Duration, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(n) * time.Millisecond, nil
}

const (
	reconnectGrowFactor = 1.5
	reconnectMaxDelay   = 30 * time.Second
)

var reconnectInitialDelay = 1 * time.Second

func calculateReconnectDelay(attempt int) time.Duration {
	if attempt == 0 {
		return 0
	}
	backoffDuration := time.Duration(float64(reconnectInitialDelay) * math.Pow(reconnectGrowFactor, float64(attempt-1)))
	if backoffDuration > reconnectMaxDelay {
		backoffDuration = reconnectMaxDelay
	}
	jitter := time.Duration(rand.Int63n(int64(backoffDuration) + 1))
	return backoffDuration + jitter
}

func isTransientHTTPStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		http.StatusTooManyRequests:
		return true
	}
	return false
}

type detachedContext struct{ context.Context }

func (d detachedContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (d detachedContext) Done() <-chan struct{}       { return nil }
func (d detachedContext) Err() error                  { return nil }

func detachContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return detachedContext{Context: ctx}
}

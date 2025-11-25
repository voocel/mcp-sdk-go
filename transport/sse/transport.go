package sse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

const (
	MCPProtocolVersionHeader = "MCP-Protocol-Version"
	MCPSessionIDHeader       = "MCP-Session-Id"
	DefaultProtocolVersion   = "2025-06-18"
)

type SSETransport struct {
	baseURL         *url.URL
	client          *http.Client
	protocolVersion string
	sessionID       string
}

type Option func(*SSETransport)

func WithProtocolVersion(version string) Option {
	return func(t *SSETransport) {
		t.protocolVersion = version
	}
}

func WithSessionID(sessionID string) Option {
	return func(t *SSETransport) {
		t.sessionID = sessionID
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(t *SSETransport) {
		t.client = client
	}
}

func NewSSETransport(urlStr string, options ...Option) (*SSETransport, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	t := &SSETransport{
		baseURL:         parsedURL,
		client:          &http.Client{},
		protocolVersion: DefaultProtocolVersion,
		sessionID:       generateSessionID(),
	}

	for _, option := range options {
		option(t)
	}

	return t, nil
}

func (t *SSETransport) Connect(ctx context.Context) (transport.Connection, error) {
	conn := &sseConnection{
		transport:     t,
		sessionID:     t.sessionID,
		incoming:      make(chan *protocol.JSONRPCMessage, 10),
		endpointReady: make(chan struct{}),
	}

	if err := conn.startEventStream(ctx); err != nil {
		return nil, err
	}

	// Wait for endpoint to be ready
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	select {
	case <-conn.endpointReady:
		return conn, nil
	case <-ctx.Done():
		conn.Close()
		return nil, ctx.Err()
	case <-timeout.C:
		conn.Close()
		return nil, fmt.Errorf("timeout waiting for endpoint")
	}
}

type sseConnection struct {
	transport *SSETransport
	sessionID string

	endpoint      *url.URL
	endpointReady chan struct{}
	endpointOnce  sync.Once

	incoming  chan *protocol.JSONRPCMessage
	closed    atomic.Bool
	closeOnce sync.Once
	closeFunc func() error

	mu sync.RWMutex
}

func (c *sseConnection) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	if c.closed.Load() {
		return nil, transport.ErrConnectionClosed
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-c.incoming:
		if !ok {
			return nil, transport.ErrConnectionClosed
		}
		return msg, nil
	}
}

func (c *sseConnection) Write(ctx context.Context, msg *protocol.JSONRPCMessage) error {
	if c.closed.Load() {
		return transport.ErrConnectionClosed
	}

	c.mu.RLock()
	endpoint := c.endpoint
	c.mu.RUnlock()

	if endpoint == nil {
		return fmt.Errorf("endpoint not ready")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(MCPProtocolVersionHeader, c.transport.protocolVersion)
	req.Header.Set(MCPSessionIDHeader, c.sessionID)

	resp, err := c.transport.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		// 202 Accepted - Message received, response will be sent via SSE
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var response protocol.JSONRPCMessage
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	select {
	case c.incoming <- &response:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *sseConnection) Close() error {
	var err error
	c.closeOnce.Do(func() {
		if c.closed.CompareAndSwap(false, true) {
			if c.closeFunc != nil {
				err = c.closeFunc()
			}
			close(c.incoming)
		}
	})
	return err
}

func (c *sseConnection) SessionID() string {
	return c.sessionID
}

func (c *sseConnection) startEventStream(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.transport.baseURL.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set(MCPProtocolVersionHeader, c.transport.protocolVersion)
	req.Header.Set(MCPSessionIDHeader, c.sessionID)

	resp, err := c.transport.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to start event stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	serverVersion := resp.Header.Get(MCPProtocolVersionHeader)
	if serverVersion != "" && serverVersion != c.transport.protocolVersion {
		fmt.Printf("Warning: Protocol version mismatch. Client: %s, Server: %s\n",
			c.transport.protocolVersion, serverVersion)
	}

	c.closeFunc = resp.Body.Close

	// Start event processing
	go c.processEvents(ctx, resp.Body)

	return nil
}

func (c *sseConnection) processEvents(ctx context.Context, body io.ReadCloser) {
	defer body.Close()
	defer func() {
		if c.closed.CompareAndSwap(false, true) {
			close(c.incoming)
		}
	}()

	scanner := bufio.NewScanner(body)
	var event, data string

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\n")

		// Empty line indicates end of event
		if line == "" {
			if data != "" {
				if event == "" {
					event = "message"
				}
				c.handleSSEEvent(event, data)
				event = ""
				data = ""
			}
			continue
		}

		// Parse SSE fields
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}

	// Process the last event
	if data != "" {
		if event == "" {
			event = "message"
		}
		c.handleSSEEvent(event, data)
	}

	if err := scanner.Err(); err != nil && !c.closed.Load() {
		fmt.Printf("SSE scanner error: %v\n", err)
	}
}

// handleSSEEvent handles SSE events
func (c *sseConnection) handleSSEEvent(event, data string) {
	switch event {
	case "endpoint":
		// Parse endpoint URL
		endpoint, err := c.transport.baseURL.Parse(data)
		if err != nil {
			fmt.Printf("Error parsing endpoint URL: %v\n", err)
			return
		}

		// Security check: ensure endpoint has same origin as baseURL
		if endpoint.Host != c.transport.baseURL.Host {
			fmt.Printf("Endpoint origin does not match connection origin\n")
			return
		}

		c.mu.Lock()
		c.endpoint = endpoint
		c.mu.Unlock()

		// Notify endpoint is ready
		c.endpointOnce.Do(func() {
			close(c.endpointReady)
		})

	case "message":
		// Parse JSON-RPC message
		var msg protocol.JSONRPCMessage
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			fmt.Printf("Invalid JSON-RPC message: %v\n", err)
			return
		}

		select {
		case c.incoming <- &msg:
		default:
			// Buffer full, drop message
			fmt.Printf("Message buffer full, dropping message\n")
		}
	}
}

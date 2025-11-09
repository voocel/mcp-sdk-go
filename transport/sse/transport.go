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

	// 等待 endpoint 就绪
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
		// 202 Accepted - 消息已接收,响应将通过 SSE 发送
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

	// 启动事件处理
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

		// 空行表示事件结束
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

		// 解析 SSE 字段
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}

	// 处理最后一个事件
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

// handleSSEEvent 处理 SSE 事件
func (c *sseConnection) handleSSEEvent(event, data string) {
	switch event {
	case "endpoint":
		// 解析 endpoint URL
		endpoint, err := c.transport.baseURL.Parse(data)
		if err != nil {
			fmt.Printf("Error parsing endpoint URL: %v\n", err)
			return
		}

		// 安全检查:确保 endpoint 与 baseURL 同源
		if endpoint.Host != c.transport.baseURL.Host {
			fmt.Printf("Endpoint origin does not match connection origin\n")
			return
		}

		c.mu.Lock()
		c.endpoint = endpoint
		c.mu.Unlock()

		// 通知 endpoint 就绪
		c.endpointOnce.Do(func() {
			close(c.endpointReady)
		})

	case "message":
		// 解析 JSON-RPC 消息
		var msg protocol.JSONRPCMessage
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			fmt.Printf("Invalid JSON-RPC message: %v\n", err)
			return
		}

		select {
		case c.incoming <- &msg:
		default:
			// 缓冲区满,丢弃消息
			fmt.Printf("Message buffer full, dropping message\n")
		}
	}
}

package sse

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

const (
	MCPProtocolVersionHeader = "MCP-Protocol-Version"
	MCPSessionIDHeader       = "MCP-Session-Id"
	DefaultProtocolVersion   = "2025-06-18"
)

type Transport struct {
	baseURL         *url.URL
	endpoint        *url.URL
	client          *http.Client
	messageBuffer   chan []byte
	closeOnce       sync.Once
	closeFunc       func() error
	mu              sync.RWMutex
	sessionID       string
	protocolVersion string
	closed          bool
	endpointChan    chan struct{}
	started         bool
}

type Option func(*Transport)

func WithProtocolVersion(version string) Option {
	return func(t *Transport) {
		t.protocolVersion = version
	}
}

func WithSessionID(sessionID string) Option {
	return func(t *Transport) {
		t.sessionID = sessionID
	}
}

func New(urlStr string, options ...Option) *Transport {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		// 如果解析失败，使用原始字符串作为fallback
		parsedURL = &url.URL{Scheme: "http", Host: "localhost", Path: urlStr}
	}

	t := &Transport{
		baseURL:         parsedURL,
		client:          &http.Client{},
		messageBuffer:   make(chan []byte, 100),
		protocolVersion: DefaultProtocolVersion,
		endpointChan:    make(chan struct{}),
	}

	for _, option := range options {
		option(t)
	}

	// 生成session ID如果没有提供
	if t.sessionID == "" {
		t.sessionID = generateSessionID()
	}

	return t
}

func generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func (t *Transport) Send(ctx context.Context, data []byte) error {
	t.mu.RLock()
	endpoint := t.endpoint
	t.mu.RUnlock()

	if endpoint == nil {
		return fmt.Errorf("endpoint not received, call Connect first")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 添加MCP标准头部
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(MCPProtocolVersionHeader, t.protocolVersion)
	req.Header.Set(MCPSessionIDHeader, t.sessionID)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode == http.StatusAccepted {
		// 202 Accepted - 消息已接收，响应将通过SSE发送
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	// 对于同步响应，读取并缓存
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	select {
	case t.messageBuffer <- body:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (t *Transport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-t.messageBuffer:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	}
}

func (t *Transport) startEventStream(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", t.baseURL.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 添加SSE标准头部
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set(MCPProtocolVersionHeader, t.protocolVersion)
	req.Header.Set(MCPSessionIDHeader, t.sessionID)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to start event stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	// 检查协议版本兼容性
	serverVersion := resp.Header.Get(MCPProtocolVersionHeader)
	if serverVersion != "" && serverVersion != t.protocolVersion {
		// 记录警告，但不阻止连接
		fmt.Printf("Warning: Protocol version mismatch. Client: %s, Server: %s\n",
			t.protocolVersion, serverVersion)
	}

	t.closeFunc = resp.Body.Close
	go t.processEvents(ctx, resp.Body)

	return nil
}

func (t *Transport) processEvents(ctx context.Context, body io.ReadCloser) {
	defer body.Close()
	defer func() {
		t.mu.Lock()
		if !t.closed {
			close(t.messageBuffer)
			t.closed = true
		}
		t.mu.Unlock()
	}()

	scanner := bufio.NewScanner(body)
	var event, data string

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\n")

		// 空行表示事件结束
		if line == "" {
			if data != "" {
				// 如果没有指定事件类型，默认为message
				if event == "" {
					event = "message"
				}
				t.handleSSEEvent(event, data)
				event = ""
				data = ""
			}
			continue
		}

		// 解析SSE字段
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		// 忽略其他字段如id:, retry:等
	}

	// 处理最后一个事件（如果没有以空行结尾）
	if data != "" {
		if event == "" {
			event = "message"
		}
		t.handleSSEEvent(event, data)
	}

	if err := scanner.Err(); err != nil && !t.closed {
		fmt.Printf("SSE scanner error: %v\n", err)
	}
}

func (t *Transport) handleSSEEvent(event, data string) {
	switch event {
	case "endpoint":
		// 解析endpoint URL
		endpoint, err := t.baseURL.Parse(data)
		if err != nil {
			fmt.Printf("Error parsing endpoint URL: %v\n", err)
			return
		}
		// 安全检查：确保endpoint与baseURL同源
		if endpoint.Host != t.baseURL.Host {
			fmt.Printf("Endpoint origin does not match connection origin\n")
			return
		}

		t.mu.Lock()
		t.endpoint = endpoint
		t.mu.Unlock()

		// 通知endpoint已接收
		select {
		case <-t.endpointChan:
			// 已经关闭
		default:
			close(t.endpointChan)
		}

	case "message":
		// 验证JSON格式并缓存消息
		var msg protocol.JSONRPCMessage
		if err := json.Unmarshal([]byte(data), &msg); err == nil {
			select {
			case t.messageBuffer <- []byte(data):
			default:
				// 缓冲区满，丢弃消息
				fmt.Printf("Message buffer full, dropping message\n")
			}
		} else {
			fmt.Printf("Invalid JSON-RPC message: %v\n", err)
		}
	}
}

func (t *Transport) Connect(ctx context.Context) error {
	if t.started {
		return fmt.Errorf("transport already started")
	}

	// 启动SSE流
	if err := t.startEventStream(ctx); err != nil {
		return err
	}

	// 等待endpoint接收
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	select {
	case <-t.endpointChan:
		// endpoint已接收
		t.started = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout.C:
		return fmt.Errorf("timeout waiting for endpoint")
	}
}

func (t *Transport) Close() error {
	var err error

	t.closeOnce.Do(func() {
		if t.closeFunc != nil {
			err = t.closeFunc()
		}
		t.mu.Lock()
		if !t.closed {
			close(t.messageBuffer)
			t.closed = true
		}
		t.mu.Unlock()
	})

	return err
}

func (t *Transport) GetSessionID() string {
	return t.sessionID
}

type Server struct {
	handler         transport.Handler
	httpServer      *http.Server
	sessions        map[string]*Session
	mu              sync.RWMutex
	protocolVersion string
}

type Session struct {
	ID         string
	Client     chan []byte
	LastActive time.Time
	mu         sync.RWMutex
}

func NewServer(addr string, handler transport.Handler) *Server {
	s := &Server{
		handler:         handler,
		sessions:        make(map[string]*Session),
		protocolVersion: DefaultProtocolVersion,
	}

	mux := http.NewServeMux()
	// SSE连接端点 - 支持根路径和/sse路径
	mux.HandleFunc("/", s.handleSSE)
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/message", s.handleMessage)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// 启动会话清理goroutine
	go s.cleanupSessions()

	return s
}

// validateProtocolVersion 验证协议版本头部
func (s *Server) validateProtocolVersion(r *http.Request) error {
	clientVersion := r.Header.Get(MCPProtocolVersionHeader)

	// 如果没有头部，根据规范假设为最新版本 (这是允许的)
	if clientVersion == "" {
		return nil // 服务器应假设为 2025-06-18
	}

	// 只有当客户端明确发送了版本头部时，才验证其有效性
	supportedVersions := []string{
		"2025-06-18",
		"2025-03-26",
		"2024-11-05",
	}

	for _, version := range supportedVersions {
		if clientVersion == version {
			return nil
		}
	}

	// 只有明确不支持的版本才返回错误
	return fmt.Errorf("unsupported protocol version: %s", clientVersion)
}

func (s *Server) cleanupSessions() {
	ticker := time.NewTicker(time.Minute * 5)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, session := range s.sessions {
			session.mu.RLock()
			if now.Sub(session.LastActive) > time.Minute*10 {
				close(session.Client)
				delete(s.sessions, id)
			}
			session.mu.RUnlock()
		}
		s.mu.Unlock()
	}
}

func (s *Server) getOrCreateSession(sessionID string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionID == "" {
		sessionID = generateSessionID()
	}

	session, exists := s.sessions[sessionID]
	if !exists {
		session = &Session{
			ID:         sessionID,
			Client:     make(chan []byte, 100),
			LastActive: time.Now(),
		}
		s.sessions[sessionID] = session
	} else {
		session.mu.Lock()
		session.LastActive = time.Now()
		session.mu.Unlock()
	}

	return session
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if err := s.validateProtocolVersion(r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sessionID := r.Header.Get(MCPSessionIDHeader)
	session := s.getOrCreateSession(sessionID)

	// 设置SSE响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set(MCPSessionIDHeader, session.ID)

	// 确保支持flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// 发送endpoint事件 - 统一使用/message端点
	endpointURL := fmt.Sprintf("/message?sessionId=%s", session.ID)

	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	flusher.Flush()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-session.Client:
			if !ok {
				return
			}

			// 发送SSE格式的消息事件
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()

			// 更新会话活动时间
			session.mu.Lock()
			session.LastActive = time.Now()
			session.mu.Unlock()
		}
	}
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	if err := s.validateProtocolVersion(r); err != nil {
		s.sendJSONRPCError(w, "", protocol.InvalidParams, err.Error(), nil)
		return
	}

	// 从查询参数获取sessionId
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		s.sendJSONRPCError(w, "", protocol.InvalidParams, "Missing sessionId parameter", nil)
		return
	}

	s.mu.RLock()
	session, exists := s.sessions[sessionID]
	s.mu.RUnlock()

	if !exists {
		s.sendJSONRPCError(w, "", protocol.InvalidParams, "Invalid session ID", nil)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendJSONRPCError(w, "", protocol.ParseError, "Failed to read request body", nil)
		return
	}

	// 验证JSON-RPC格式
	var message protocol.JSONRPCMessage
	if err := json.Unmarshal(body, &message); err != nil {
		s.sendJSONRPCError(w, "", protocol.ParseError, "Invalid JSON-RPC format", nil)
		return
	}

	// 立即返回202 Accepted，响应将通过SSE发送
	w.Header().Set(MCPSessionIDHeader, session.ID)
	w.WriteHeader(http.StatusAccepted)

	// 在goroutine中处理消息，避免阻塞HTTP响应
	go func() {
		response, err := s.handler.HandleMessage(r.Context(), body)
		if err != nil {
			errorResp := protocol.JSONRPCMessage{
				JSONRPC: "2.0",
				ID:      message.ID,
				Error: &protocol.JSONRPCError{
					Code:    protocol.InternalError,
					Message: err.Error(),
				},
			}
			if errorData, err := json.Marshal(errorResp); err == nil {
				response = errorData
			}
		}

		// 只有非通知消息才发送响应
		if response != nil {
			select {
			case session.Client <- response:
				// 响应已发送
			default:
				// 缓冲区满，记录错误
				fmt.Printf("Session %s buffer full, dropping response\n", sessionID)
			}
		}

		session.mu.Lock()
		session.LastActive = time.Now()
		session.mu.Unlock()
	}()
}

func (s *Server) sendJSONRPCError(w http.ResponseWriter, id string, code int, message string, data interface{}) {
	errorResp := protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      protocol.StringToID(id),
		Error: &protocol.JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(errorResp)
}

func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.httpServer.Shutdown(context.Background())
	}()

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	for _, session := range s.sessions {
		close(session.Client)
	}
	s.sessions = make(map[string]*Session)
	s.mu.Unlock()

	return s.httpServer.Shutdown(ctx)
}

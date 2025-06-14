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
	"strings"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

const (
	MCPProtocolVersionHeader = "MCP-Protocol-Version"
	MCPSessionIDHeader       = "MCP-Session-Id"
	DefaultProtocolVersion   = "2024-11-05"
)

type Transport struct {
	url             string
	client          *http.Client
	messageBuffer   chan []byte
	closeOnce       sync.Once
	closeFunc       func() error
	mu              sync.Mutex
	sessionID       string
	protocolVersion string
	closed          bool
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

func New(url string, options ...Option) *Transport {
	t := &Transport{
		url:             url,
		client:          &http.Client{},
		messageBuffer:   make(chan []byte, 100),
		protocolVersion: DefaultProtocolVersion,
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
	t.mu.Lock()
	defer t.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(data))
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

	// 处理不同类型的响应
	contentType := resp.Header.Get("Content-Type")

	if resp.StatusCode == http.StatusAccepted {
		// 202 Accepted for notifications/responses
		return nil
	} else if resp.StatusCode != http.StatusOK {
		// 处理错误响应
		body, _ := io.ReadAll(resp.Body)

		// 尝试解析JSON-RPC错误
		var errorResp protocol.JSONRPCMessage
		if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != nil {
			return fmt.Errorf("MCP error %d: %s", errorResp.Error.Code, errorResp.Error.Message)
		}

		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	if strings.Contains(contentType, "text/event-stream") {
		// 服务器开始SSE流，启动事件处理
		t.closeFunc = resp.Body.Close
		go t.processEvents(ctx, resp.Body)
	} else if strings.Contains(contentType, "application/json") {
		// 单个JSON响应
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		select {
		case t.messageBuffer <- body:
		case <-ctx.Done():
			return ctx.Err()
		}
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
	req, err := http.NewRequestWithContext(ctx, "GET", t.url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 添加MCP标准头部
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set(MCPProtocolVersionHeader, t.protocolVersion)
	req.Header.Set(MCPSessionIDHeader, t.sessionID)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to start event stream: %w", err)
	}

	if resp.StatusCode == http.StatusMethodNotAllowed {
		resp.Body.Close()
		return fmt.Errorf("server does not support SSE streams")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	// 检查协议版本兼容性
	serverVersion := resp.Header.Get(MCPProtocolVersionHeader)
	if serverVersion != "" && serverVersion != t.protocolVersion {
		// 可以记录警告，但不阻止连接
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
	var buffer strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if buffer.Len() > 0 {
				eventData := buffer.String()

				if strings.HasPrefix(eventData, "data:") {
					data := strings.TrimPrefix(eventData, "data:")
					data = strings.TrimSpace(data)

					// 验证JSON格式
					var msg protocol.JSONRPCMessage
					if err := json.Unmarshal([]byte(data), &msg); err == nil {
						select {
						case t.messageBuffer <- []byte(data):
						case <-ctx.Done():
							return
						}
					}
				}

				buffer.Reset()
			}
			continue
		}

		buffer.WriteString(line)
		buffer.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("SSE scanner error: %v\n", err)
	}
}

func (t *Transport) Connect(ctx context.Context) error {
	return t.startEventStream(ctx)
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
	mux.HandleFunc("/", s.handleRequest)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// 启动会话清理goroutine
	go s.cleanupSessions()

	return s
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

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// 验证协议版本
	clientVersion := r.Header.Get(MCPProtocolVersionHeader)
	if clientVersion != "" && clientVersion != s.protocolVersion {
		if clientVersion == "2025-03-26" || clientVersion == "2024-11-05" {
			// 兼容支持的版本
		} else {
			http.Error(w, "Unsupported protocol version", http.StatusBadRequest)
			return
		}
	}

	// 添加协议版本响应头
	w.Header().Set(MCPProtocolVersionHeader, s.protocolVersion)

	if r.Method == "GET" {
		s.handleSSE(w, r)
	} else if r.Method == "POST" {
		s.handleMessage(w, r)
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
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

	// 发送初始flush
	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 监听客户端断开连接
	closeNotify := w.(http.CloseNotifier).CloseNotify()

	for {
		select {
		case <-closeNotify:
			return
		case <-ctx.Done():
			return
		case msg, ok := <-session.Client:
			if !ok {
				return
			}

			// 发送SSE格式的数据
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()

			// 更新会话活动时间
			session.mu.Lock()
			session.LastActive = time.Now()
			session.mu.Unlock()
		}
	}
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(MCPSessionIDHeader)
	session := s.getOrCreateSession(sessionID)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendJSONRPCError(w, nil, protocol.ParseError, "Failed to read request body", nil)
		return
	}

	// 验证JSON-RPC格式
	var message protocol.JSONRPCMessage
	if err := json.Unmarshal(body, &message); err != nil {
		s.sendJSONRPCError(w, nil, protocol.ParseError, "Invalid JSON-RPC format", nil)
		return
	}

	// 处理消息
	response, err := s.handler.HandleMessage(r.Context(), body)
	if err != nil {
		s.sendJSONRPCError(w, message.ID, protocol.InternalError, err.Error(), nil)
		return
	}

	// 确定响应类型
	var responseMessage protocol.JSONRPCMessage
	if err := json.Unmarshal(response, &responseMessage); err == nil {
		if responseMessage.Method != "" && responseMessage.ID == nil {
			// 这是一个通知，返回202 Accepted
			w.Header().Set(MCPSessionIDHeader, session.ID)
			w.WriteHeader(http.StatusAccepted)
			return
		}
	}

	// 检查是否应该通过SSE发送响应
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/event-stream") && message.Method != "" && message.ID != nil {
		// 通过SSE发送响应
		select {
		case session.Client <- response:
		default:
			// 如果缓冲区满，直接返回HTTP响应
		}

		// 启动SSE流
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set(MCPSessionIDHeader, session.ID)

		flusher, ok := w.(http.Flusher)
		if ok {
			flusher.Flush()
		}
	} else {
		// 直接返回JSON响应
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(MCPSessionIDHeader, session.ID)
		w.WriteHeader(http.StatusOK)
		w.Write(response)
	}

	// 更新会话活动时间
	session.mu.Lock()
	session.LastActive = time.Now()
	session.mu.Unlock()
}

func (s *Server) sendJSONRPCError(w http.ResponseWriter, id *string, code int, message string, data interface{}) {
	errorResp := protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
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
	// 关闭所有会话
	s.mu.Lock()
	for _, session := range s.sessions {
		close(session.Client)
	}
	s.sessions = make(map[string]*Session)
	s.mu.Unlock()

	return s.httpServer.Shutdown(ctx)
}

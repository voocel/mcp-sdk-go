package streamable

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

	"github.com/voocel/mcp-sdk-go/transport"
)

const (
	MCPProtocolVersionHeader = "MCP-Protocol-Version"
	MCPSessionIDHeader       = "Mcp-Session-Id"
	DefaultProtocolVersion   = "2025-06-18"
	LastEventIDHeader        = "Last-Event-ID"
)

// Transport 实现Streamable HTTP客户端传输
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
		client:          &http.Client{Timeout: 30 * time.Second},
		messageBuffer:   make(chan []byte, 100),
		protocolVersion: DefaultProtocolVersion,
	}

	for _, option := range options {
		option(t)
	}

	if t.sessionID == "" {
		t.sessionID = generateSessionID()
	}

	return t
}

// generateSessionID 生成安全的会话ID
func generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// Send 发送消息
func (t *Transport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("transport is closed")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 设置必需的头部
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(MCPProtocolVersionHeader, t.protocolVersion)
	if t.sessionID != "" {
		req.Header.Set(MCPSessionIDHeader, t.sessionID)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode == http.StatusAccepted {
		// 202 Accepted for notifications/responses
		return nil
	}

	if resp.StatusCode == http.StatusNotFound {
		// 会话已过期，清除会话ID
		t.sessionID = ""
		return fmt.Errorf("session expired")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	// 检查是否有新的会话ID
	if newSessionID := resp.Header.Get(MCPSessionIDHeader); newSessionID != "" {
		t.sessionID = newSessionID
	}

	// 处理响应类型
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		// SSE流响应
		go t.processSSEStream(ctx, resp.Body)
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

// Receive 接收消息
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

// Close 关闭传输
func (t *Transport) Close() error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		defer t.mu.Unlock()

		t.closed = true
		close(t.messageBuffer)

		if t.closeFunc != nil {
			t.closeFunc()
		}
	})
	return nil
}

// StartEventStream 启动事件流（用于接收服务器主动发送的消息）
func (t *Transport) StartEventStream(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", t.url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set(MCPProtocolVersionHeader, t.protocolVersion)
	if t.sessionID != "" {
		req.Header.Set(MCPSessionIDHeader, t.sessionID)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to start event stream: %w", err)
	}

	if resp.StatusCode == http.StatusMethodNotAllowed {
		resp.Body.Close()
		return fmt.Errorf("server does not support event streams")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	t.closeFunc = resp.Body.Close
	go t.processSSEStream(ctx, resp.Body)

	return nil
}

// processSSEStream 处理SSE流
func (t *Transport) processSSEStream(ctx context.Context, body io.ReadCloser) {
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
	var eventBuilder strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// 空行表示事件结束
			if eventBuilder.Len() > 0 {
				data := eventBuilder.String()
				eventBuilder.Reset()

				select {
				case t.messageBuffer <- []byte(data):
				case <-ctx.Done():
					return
				}
			}
			continue
		}

		if strings.HasPrefix(line, "id: ") {
			// 记录事件ID但当前实现中不使用
			_ = strings.TrimPrefix(line, "id: ")
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if eventBuilder.Len() > 0 {
				eventBuilder.WriteString("\n")
			}
			eventBuilder.WriteString(data)
		} else if strings.HasPrefix(line, ": ") {
			// 心跳或注释，忽略
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		// 可以记录错误，但在这里不能返回错误
		fmt.Printf("SSE stream error: %v\n", err)
	}
}

// GetSessionID 获取当前会话ID
func (t *Transport) GetSessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

// Server 实现Streamable HTTP服务器传输
type Server struct {
	handler         transport.Handler
	httpServer      *http.Server
	sessions        map[string]*Session
	mu              sync.RWMutex
	protocolVersion string
	addr            string
}

// Session 表示一个传输会话
type Session struct {
	ID         string
	EventID    uint64
	Client     chan []byte
	LastActive time.Time
	mu         sync.RWMutex
}

func (s *Session) generateEventID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EventID++
	return fmt.Sprintf("%s-%d", s.ID, s.EventID)
}

// NewServer 创建新的 Streamable HTTP 传输服务器
func NewServer(addr string, handler transport.Handler) *Server {
	return &Server{
		handler:         handler,
		sessions:        make(map[string]*Session),
		protocolVersion: DefaultProtocolVersion,
		addr:            addr,
	}
}

// Serve 启动服务器
func (s *Server) Serve(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.httpServer = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	// 定期清理过期会话
	go s.cleanupSessions(ctx)

	// 监听取消信号
	go func() {
		<-ctx.Done()
		s.Shutdown(context.Background())
	}()

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	return nil
}

// Shutdown 关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 关闭所有会话
	for _, session := range s.sessions {
		close(session.Client)
	}
	s.sessions = make(map[string]*Session)

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// cleanupSessions 清理过期会话
func (s *Server) cleanupSessions(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for id, session := range s.sessions {
				if now.Sub(session.LastActive) > 30*time.Minute {
					close(session.Client)
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

// handleRequest 处理HTTP请求
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// 安全检查：验证Origin头
	if origin := r.Header.Get("Origin"); origin != "" {
		if !s.isValidOrigin(origin) {
			http.Error(w, "Invalid origin", http.StatusForbidden)
			return
		}
	}

	switch r.Method {
	case http.MethodPost:
		s.handlePOST(w, r)
	case http.MethodGet:
		s.handleGET(w, r)
	case http.MethodDelete:
		s.handleDELETE(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePOST 处理POST请求
func (s *Server) handlePOST(w http.ResponseWriter, r *http.Request) {
	// 检查Accept头
	accept := r.Header.Get("Accept")
	if !strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/event-stream") {
		http.Error(w, "Unsupported Accept header", http.StatusBadRequest)
		return
	}

	// 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// 检查消息类型
	hasRequests := s.hasJSONRPCRequests(body)

	// 处理会话
	sessionID := r.Header.Get(MCPSessionIDHeader)
	var session *Session
	if hasRequests {
		session = s.getOrCreateSession(sessionID)
	}

	// 交给处理器处理
	responseData, err := s.handler.HandleMessage(r.Context(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 如果只有通知/响应，返回202
	if !hasRequests {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// 检查是否需要流式响应
	needsStreaming := s.needsStreaming(responseData)

	if needsStreaming && strings.Contains(accept, "text/event-stream") {
		// 返回SSE流
		s.startSSEStream(w, r, session, responseData)
	} else {
		// 返回单个JSON响应
		w.Header().Set("Content-Type", "application/json")
		if session != nil {
			w.Header().Set(MCPSessionIDHeader, session.ID)
		}
		w.Write(responseData)
	}
}

// handleGET 处理GET请求
func (s *Server) handleGET(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")
	if !strings.Contains(accept, "text/event-stream") {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.Header.Get(MCPSessionIDHeader)
	session := s.getOrCreateSession(sessionID)

	// 检查恢复请求
	lastEventID := r.Header.Get(LastEventIDHeader)
	if lastEventID != "" {
		fmt.Printf("Resuming stream from event ID: %s\n", lastEventID)
		// TODO: 实现消息重放逻辑
	}

	s.startSSEStream(w, r, session, nil)
}

// handleDELETE 处理DELETE请求
func (s *Server) handleDELETE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(MCPSessionIDHeader)
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if session, exists := s.sessions[sessionID]; exists {
		close(session.Client)
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

// getOrCreateSession 获取或创建会话
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
		session.LastActive = time.Now()
	}

	return session
}

// startSSEStream 启动SSE流
func (s *Server) startSSEStream(w http.ResponseWriter, r *http.Request, session *Session, initialData []byte) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Last-Event-ID")

	if session != nil {
		w.Header().Set(MCPSessionIDHeader, session.ID)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// 发送初始数据
	if initialData != nil {
		eventID := session.generateEventID()
		fmt.Fprintf(w, "id: %s\ndata: %s\n\n", eventID, initialData)
		flusher.Flush()
	}

	// 保持连接
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-session.Client:
			if !ok {
				return
			}
			eventID := session.generateEventID()
			fmt.Fprintf(w, "id: %s\ndata: %s\n\n", eventID, data)
			flusher.Flush()
		case <-time.After(30 * time.Second):
			// 心跳
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// hasJSONRPCRequests 检查是否包含JSON-RPC请求
func (s *Server) hasJSONRPCRequests(data []byte) bool {
	// 尝试解析为单个消息
	var message map[string]interface{}
	if err := json.Unmarshal(data, &message); err == nil {
		_, hasMethod := message["method"]
		_, hasID := message["id"]
		return hasMethod && hasID
	}

	// 尝试解析为批量消息
	var messages []map[string]interface{}
	if err := json.Unmarshal(data, &messages); err == nil {
		for _, msg := range messages {
			_, hasMethod := msg["method"]
			_, hasID := msg["id"]
			if hasMethod && hasID {
				return true
			}
		}
	}

	return false
}

// needsStreaming 判断是否需要流式响应
func (s *Server) needsStreaming(data []byte) bool {
	return len(data) > 1024
}

// isValidOrigin 验证来源
func (s *Server) isValidOrigin(origin string) bool {
	return strings.Contains(origin, "localhost") ||
		strings.Contains(origin, "127.0.0.1") ||
		strings.Contains(origin, "::1")
}

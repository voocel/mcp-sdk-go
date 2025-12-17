package sse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport"
)

type HTTPHandler struct {
	serverFactory func(*http.Request) *server.Server
	sessions      map[string]*serverSession
	mu            sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type serverSession struct {
	ID         string
	Transport  *serverTransport
	LastActive time.Time
	mu         sync.RWMutex
}

type serverTransport struct {
	sessionID string
	events    chan []byte
	incoming  chan *protocol.JSONRPCMessage
	closed    bool
	mu        sync.Mutex
}

func NewHTTPHandler(serverFactory func(*http.Request) *server.Server) *HTTPHandler {
	ctx, cancel := context.WithCancel(context.Background())

	h := &HTTPHandler{
		serverFactory: serverFactory,
		sessions:      make(map[string]*serverSession),
		ctx:           ctx,
		cancel:        cancel,
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.cleanupSessions()
	}()

	return h
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.checkProtocolVersion(r)

	switch r.Method {
	case http.MethodGet:
		// SSE connection
		h.handleSSE(w, r)
	case http.MethodPost:
		// Message sending
		h.handleMessage(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// checkProtocolVersion checks the protocol version and logs a warning if unsupported
// It does not reject the connection to maintain compatibility with future protocol versions
func (h *HTTPHandler) checkProtocolVersion(r *http.Request) {
	clientVersion := r.Header.Get(MCPProtocolVersionHeader)

	// If no header, assume compatible
	if clientVersion == "" {
		return
	}

	// Check supported versions
	supportedVersions := []string{
		"2025-11-25",
		"2025-06-18",
		"2025-03-26",
		"2024-11-05",
	}

	for _, version := range supportedVersions {
		if clientVersion == version {
			return
		}
	}

	// Log warning but don't reject connection
	log.Printf("[MCP] Warning: client requested unsupported protocol version: %s (supported: %v)", clientVersion, supportedVersions)
}

// handleSSE handles SSE connections
func (h *HTTPHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(MCPSessionIDHeader)
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	session := h.getOrCreateSession(sessionID, r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set(MCPSessionIDHeader, session.ID)
	w.Header().Set(MCPProtocolVersionHeader, DefaultProtocolVersion)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send endpoint event
	endpointURL := fmt.Sprintf("/message?sessionId=%s", session.ID)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	flusher.Flush()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-session.Transport.events:
			if !ok {
				return
			}

			// Send message event in SSE format
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", event)
			flusher.Flush()

			session.mu.Lock()
			session.LastActive = time.Now()
			session.mu.Unlock()
		}
	}
}

// handleMessage handles message sending
func (h *HTTPHandler) handleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		h.sendJSONRPCError(w, "", protocol.InvalidParams, "Missing sessionId parameter", nil)
		return
	}

	h.mu.RLock()
	session, exists := h.sessions[sessionID]
	h.mu.RUnlock()

	if !exists {
		h.sendJSONRPCError(w, "", protocol.InvalidParams, "Invalid session ID", nil)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendJSONRPCError(w, "", protocol.ParseError, "Failed to read request body", nil)
		return
	}

	var message protocol.JSONRPCMessage
	if err := json.Unmarshal(body, &message); err != nil {
		h.sendJSONRPCError(w, "", protocol.ParseError, "Invalid JSON-RPC format", nil)
		return
	}

	// Return 202 Accepted immediately, response will be sent via SSE
	w.Header().Set(MCPSessionIDHeader, session.ID)
	w.WriteHeader(http.StatusAccepted)

	select {
	case session.Transport.incoming <- &message:
		// Message sent
	default:
		// Buffer full
		fmt.Printf("Session %s buffer full, dropping message\n", sessionID)
	}

	session.mu.Lock()
	session.LastActive = time.Now()
	session.mu.Unlock()
}

func (h *HTTPHandler) getOrCreateSession(sessionID string, r *http.Request) *serverSession {
	h.mu.Lock()
	defer h.mu.Unlock()

	session, exists := h.sessions[sessionID]
	if !exists {
		transport := &serverTransport{
			sessionID: sessionID,
			events:    make(chan []byte, 100),
			incoming:  make(chan *protocol.JSONRPCMessage, 10),
		}

		session = &serverSession{
			ID:         sessionID,
			Transport:  transport,
			LastActive: time.Now(),
		}

		h.sessions[sessionID] = session

		go h.handleServerSession(r.Context(), session, r)
	} else {
		session.mu.Lock()
		session.LastActive = time.Now()
		session.mu.Unlock()
	}

	return session
}

// handleServerSession handles the server session
func (h *HTTPHandler) handleServerSession(ctx context.Context, session *serverSession, r *http.Request) {
	mcpServer := h.serverFactory(r)

	serverSession, err := mcpServer.Connect(ctx, session.Transport, nil)
	if err != nil {
		fmt.Printf("Failed to connect server session: %v\n", err)
		return
	}
	defer serverSession.Close()

	if err := serverSession.Wait(); err != nil {
		fmt.Printf("Server session error: %v\n", err)
	}
}

// cleanupSessions cleans up expired sessions
func (h *HTTPHandler) cleanupSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.mu.Lock()
			now := time.Now()
			for id, session := range h.sessions {
				session.mu.RLock()
				if now.Sub(session.LastActive) > 10*time.Minute {
					session.Transport.Close()
					delete(h.sessions, id)
				}
				session.mu.RUnlock()
			}
			h.mu.Unlock()
		}
	}
}

// sendJSONRPCError sends a JSON-RPC error response
func (h *HTTPHandler) sendJSONRPCError(w http.ResponseWriter, id string, code int, message string, data interface{}) {
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

// Shutdown shuts down the handler
func (h *HTTPHandler) Shutdown(ctx context.Context) error {
	if h.cancel != nil {
		h.cancel()
	}

	h.wg.Wait()

	h.mu.Lock()
	for _, session := range h.sessions {
		session.Transport.Close()
	}
	h.sessions = make(map[string]*serverSession)
	h.mu.Unlock()

	return nil
}

func (t *serverTransport) Connect(ctx context.Context) (transport.Connection, error) {
	return &serverConnection{
		transport: t,
	}, nil
}

type serverConnection struct {
	transport *serverTransport
}

func (c *serverConnection) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	c.transport.mu.Lock()
	closed := c.transport.closed
	c.transport.mu.Unlock()

	if closed {
		return nil, transport.ErrConnectionClosed
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-c.transport.incoming:
		if !ok {
			return nil, transport.ErrConnectionClosed
		}
		return msg, nil
	}
}

func (c *serverConnection) Write(ctx context.Context, msg *protocol.JSONRPCMessage) error {
	c.transport.mu.Lock()
	closed := c.transport.closed
	c.transport.mu.Unlock()

	if closed {
		return transport.ErrConnectionClosed
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Send to SSE stream
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.transport.events <- data:
		return nil
	default:
		return fmt.Errorf("event buffer full")
	}
}

func (c *serverConnection) Close() error {
	return c.transport.Close()
}

func (c *serverConnection) SessionID() string {
	return c.transport.sessionID
}

func (t *serverTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.closed {
		t.closed = true
		close(t.incoming)
		close(t.events)
	}

	return nil
}

func generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

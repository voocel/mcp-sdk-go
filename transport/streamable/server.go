package streamable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
)

const (
	MCPProtocolVersionHeader = "MCP-Protocol-Version"
	MCPSessionIDHeader       = "Mcp-Session-Id"
	LastEventIDHeader        = "Last-Event-ID"
	DefaultProtocolVersion   = "2025-11-25"
	DefaultMaxBodyBytes      = 10 << 20 // 10 MiB
)

// HTTPHandler handles Streamable HTTP MCP requests.
type HTTPHandler struct {
	serverFactory   func(*http.Request) *server.Server
	writerFactory   StreamWriterFactory
	protocolVersion string
	maxBodyBytes    int64

	// Origin validation for DNS rebinding protection
	allowedOrigins map[string]bool
	validateOrigin bool

	mu       sync.RWMutex
	sessions map[string]*sessionState
}

type sessionState struct {
	server     *server.Server
	lastActive time.Time
}

// NewHTTPHandler creates a new handler with the given server factory.
func NewHTTPHandler(serverFactory func(*http.Request) *server.Server) *HTTPHandler {
	h := &HTTPHandler{
		serverFactory:   serverFactory,
		writerFactory:   NewSimpleWriterFactory(),
		protocolVersion: DefaultProtocolVersion,
		maxBodyBytes:    DefaultMaxBodyBytes,
		allowedOrigins:  make(map[string]bool),
		validateOrigin:  false,
		sessions:        make(map[string]*sessionState),
	}
	go h.cleanupLoop()
	return h
}

// SetAllowedOrigins enables Origin validation and sets the allowed origins.
// This is required to prevent DNS rebinding attacks per the MCP specification.
// Pass nil or empty slice to disable validation.
func (h *HTTPHandler) SetAllowedOrigins(origins []string) {
	h.allowedOrigins = make(map[string]bool)
	for _, o := range origins {
		h.allowedOrigins[o] = true
	}
	h.validateOrigin = len(origins) > 0
}

// SetWriterFactory sets the factory used to create stream writers.
// Use NewResumableWriterFactory(store) to enable stream resumption.
func (h *HTTPHandler) SetWriterFactory(factory StreamWriterFactory) {
	h.writerFactory = factory
}

// SetMaxBodyBytes sets the maximum request body size.
func (h *HTTPHandler) SetMaxBodyBytes(n int64) {
	h.maxBodyBytes = n
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Origin validation to prevent DNS rebinding attacks (MCP spec requirement)
	if h.validateOrigin && !h.checkOrigin(r) {
		http.Error(w, "Forbidden: invalid origin", http.StatusForbidden)
		return
	}

	w.Header().Set(MCPProtocolVersionHeader, h.protocolVersion)

	switch r.Method {
	case http.MethodPost:
		h.handlePost(w, r)
	case http.MethodGet:
		h.handleGet(w, r)
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// checkOrigin validates the Origin header against allowed origins.
// Returns true if the request is allowed.
func (h *HTTPHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// No Origin header (non-browser request) - allow
		return true
	}
	return h.allowedOrigins[origin]
}

func (h *HTTPHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	// Validate request
	if r.Header.Get(LastEventIDHeader) != "" {
		http.Error(w, "Last-Event-ID not allowed for POST", http.StatusBadRequest)
		return
	}
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
		return
	}

	// Read and parse message
	body, err := h.readBody(w, r)
	if err != nil {
		return
	}

	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "Invalid JSON-RPC message", http.StatusBadRequest)
		return
	}

	// Session handling
	sessionID := r.Header.Get(MCPSessionIDHeader)
	isInitialize := msg.Method == protocol.MethodInitialize

	if isInitialize {
		if sessionID != "" {
			http.Error(w, "Initialize must not include session ID", http.StatusBadRequest)
			return
		}
		sessionID = newSessionID()
	} else if sessionID == "" {
		http.Error(w, "Missing session ID", http.StatusBadRequest)
		return
	}

	// Get or create session
	session, err := h.getOrCreateSession(r, sessionID, isInitialize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Handle notification (no response needed)
	if msg.ID == nil && msg.Method != "" {
		_, _ = session.server.HandleMessage(r.Context(), &msg)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Handle request
	h.handleRequest(w, r, session, sessionID, &msg, isInitialize)
}

func (h *HTTPHandler) handleRequest(w http.ResponseWriter, r *http.Request, session *sessionState, sessionID string, msg *protocol.JSONRPCMessage, isInitialize bool) {
	wantsStream := acceptsEventStream(r)

	// Process message
	response, err := session.server.HandleMessage(r.Context(), msg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if response == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	data, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}

	// Set session ID header if initialize
	if isInitialize {
		w.Header().Set(MCPSessionIDHeader, sessionID)
	}

	// Respond based on client preference
	if wantsStream {
		h.respondSSE(w, r, sessionID, data)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

func (h *HTTPHandler) respondSSE(w http.ResponseWriter, r *http.Request, sessionID string, data []byte) {
	writer := h.writerFactory.Create(sessionID)
	defer writer.Close()

	streamID := newStreamID()
	if _, err := writer.Init(r.Context(), w, streamID, ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = writer.Write(r.Context(), data, true)
}

func (h *HTTPHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	// Validate Accept header per MCP spec
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "Accept header must include text/event-stream", http.StatusBadRequest)
		return
	}

	sessionID := r.Header.Get(MCPSessionIDHeader)
	if sessionID == "" {
		http.Error(w, "Missing session ID", http.StatusBadRequest)
		return
	}

	h.mu.RLock()
	_, ok := h.sessions[sessionID]
	h.mu.RUnlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	lastEventID := r.Header.Get(LastEventIDHeader)
	writer := h.writerFactory.Create(sessionID)
	defer writer.Close()

	streamID := extractStreamID(lastEventID)
	if streamID == "" {
		streamID = newStreamID()
	}

	replay, err := writer.Init(r.Context(), w, streamID, lastEventID)
	if err != nil {
		if errors.Is(err, ErrReplayUnsupported) {
			// Per MCP spec: return 405 if SSE/resumption not supported
			http.Error(w, "Method not allowed: SSE not supported", http.StatusMethodNotAllowed)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Replay stored events
	for _, data := range replay {
		if err := writer.Write(r.Context(), data, false); err != nil {
			return
		}
	}

	// Keep connection open for future events
	<-r.Context().Done()
}

func (h *HTTPHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(MCPSessionIDHeader)
	if sessionID == "" {
		http.Error(w, "Missing session ID", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	_, ok := h.sessions[sessionID]
	if ok {
		delete(h.sessions, sessionID)
	}
	h.mu.Unlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	h.writerFactory.OnSessionClose(r.Context(), sessionID)
	w.WriteHeader(http.StatusOK)
}

// Helper functions

func (h *HTTPHandler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.ContentLength > h.maxBodyBytes {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return nil, errors.New("body too large")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxBodyBytes))
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return nil, err
	}
	return body, nil
}

func (h *HTTPHandler) getOrCreateSession(r *http.Request, sessionID string, isInitialize bool) (*sessionState, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if isInitialize {
		srv := h.serverFactory(r)
		h.sessions[sessionID] = &sessionState{
			server:     srv,
			lastActive: time.Now(),
		}
		return h.sessions[sessionID], nil
	}

	session, ok := h.sessions[sessionID]
	if !ok {
		return nil, errors.New("session not found")
	}
	session.lastActive = time.Now()
	return session, nil
}

func (h *HTTPHandler) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		h.cleanupSessions(30 * time.Minute)
	}
}

func (h *HTTPHandler) cleanupSessions(maxAge time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	for id, session := range h.sessions {
		if now.Sub(session.lastActive) > maxAge {
			delete(h.sessions, id)
			go h.writerFactory.OnSessionClose(context.Background(), id)
		}
	}
}

func acceptsEventStream(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/event-stream")
}

func extractStreamID(lastEventID string) string {
	if lastEventID == "" {
		return ""
	}
	streamID, _, ok := parseEventID(lastEventID)
	if !ok {
		return ""
	}
	return streamID
}

func newSessionID() string {
	return randomHex(16)
}

func newStreamID() string {
	return randomHex(8)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

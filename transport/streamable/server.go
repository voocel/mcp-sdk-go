package streamable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	DefaultProtocolVersion   = "2025-11-25"
)

type HTTPHandler struct {
	serverFactory   func(*http.Request) *server.Server
	protocolVersion string

	mu       sync.RWMutex
	sessions map[string]*sessionInfo
}

type sessionInfo struct {
	server     *server.Server
	transport  *StreamableTransport
	lastActive time.Time
}

func NewHTTPHandler(serverFactory func(*http.Request) *server.Server) *HTTPHandler {
	h := &HTTPHandler{
		serverFactory:   serverFactory,
		protocolVersion: DefaultProtocolVersion,
		sessions:        make(map[string]*sessionInfo),
	}

	// Start session cleanup
	go h.cleanupSessions()

	return h
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(MCPProtocolVersionHeader, h.protocolVersion)

	h.checkProtocolVersion(r)

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

func (h *HTTPHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		http.Error(w, "Invalid content type: must be 'application/json'", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "Invalid JSON-RPC message", http.StatusBadRequest)
		return
	}

	isRequest := msg.ID != nil && msg.Method != ""
	isNotification := msg.ID == nil && msg.Method != ""
	isInitialize := msg.Method == protocol.MethodInitialize

	sessionID := r.Header.Get(MCPSessionIDHeader)

	if isInitialize {
		if sessionID != "" {
			http.Error(w, "Initialize request must not include session ID", http.StatusBadRequest)
			return
		}
		sessionID = NewSessionID()
	} else {
		if sessionID == "" {
			http.Error(w, "Missing session ID", http.StatusBadRequest)
			return
		}
	}

	h.mu.Lock()
	info, exists := h.sessions[sessionID]

	if !exists {
		if !isInitialize {
			h.mu.Unlock()
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}

		// Create new session
		mcpServer := h.serverFactory(r)
		transport := NewStreamableTransport(sessionID)

		// Note: Don't call Connect, as Streamable HTTP doesn't need long-running connections
		// We directly create the session object
		info = &sessionInfo{
			server:     mcpServer,
			transport:  transport,
			lastActive: time.Now(),
		}
		h.sessions[sessionID] = info
	}
	info.lastActive = time.Now()
	h.mu.Unlock()

	// If it's a notification, handle directly and return 202 Accepted
	if isNotification {
		// Notifications don't need responses, handle directly
		_, _ = info.server.HandleMessage(r.Context(), &msg)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// If it's a request, need to wait for response
	if isRequest {
		// Create a stream to track this request
		streamID := NewSessionID()
		requests := make(map[string]struct{})
		requests[msg.GetIDString()] = struct{}{}

		done := make(chan struct{})
		var responseData []byte

		deliver := func(data []byte, final bool) error {
			responseData = data
			if final {
				close(done)
			}
			return nil
		}

		// Register stream
		info.transport.RegisterStream(streamID, requests, deliver)
		defer info.transport.UnregisterStream(streamID)

		// Handle message directly, as each Streamable HTTP request is independent
		response, err := info.server.HandleMessage(r.Context(), &msg)
		if err != nil {
			http.Error(w, fmt.Sprintf("HandleMessage failed: %v", err), http.StatusInternalServerError)
			return
		}

		// If there's a response, send it via Write
		if response != nil {
			conn := &streamableConn{transport: info.transport}
			if err := conn.Write(r.Context(), response); err != nil {
				http.Error(w, fmt.Sprintf("Failed to write response: %v", err), http.StatusInternalServerError)
				return
			}
		}

		// Wait for response
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		select {
		case <-done:
			// Return JSON response
			w.Header().Set("Content-Type", "application/json")
			if isInitialize {
				w.Header().Set(MCPSessionIDHeader, sessionID)
			}
			w.WriteHeader(http.StatusOK)
			w.Write(responseData)

		case <-ctx.Done():
			http.Error(w, "Request timeout", http.StatusRequestTimeout)
		}
	}
}

// handleGet handles GET requests (receive SSE stream)
func (h *HTTPHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(MCPSessionIDHeader)
	if sessionID == "" {
		http.Error(w, "Method not allowed without session", http.StatusMethodNotAllowed)
		return
	}

	h.mu.RLock()
	info, exists := h.sessions[sessionID]
	h.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	// Set deliver callback for standalone SSE stream
	done := make(chan struct{})

	info.transport.streamsMu.Lock()
	standaloneStream := info.transport.streams[""]
	if standaloneStream != nil {
		standaloneStream.mu.Lock()
		standaloneStream.deliver = func(data []byte, final bool) error {
			if err := r.Context().Err(); err != nil {
				return err
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
			return nil
		}
		standaloneStream.mu.Unlock()
	}
	info.transport.streamsMu.Unlock()

	defer func() {
		if standaloneStream != nil {
			standaloneStream.mu.Lock()
			standaloneStream.deliver = nil
			standaloneStream.mu.Unlock()
		}
	}()

	// Keep connection until client disconnects
	select {
	case <-done:
	case <-r.Context().Done():
	}
}

// handleDelete handles DELETE requests (close session)
func (h *HTTPHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(MCPSessionIDHeader)
	if sessionID == "" {
		http.Error(w, "Missing session ID", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	info, exists := h.sessions[sessionID]
	if exists {
		if info.transport != nil {
			info.transport.Close()
		}
		delete(h.sessions, sessionID)
	}
	h.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

// checkProtocolVersion checks the protocol version and logs a warning if unsupported
// It does not reject the connection to maintain compatibility with future protocol versions
func (h *HTTPHandler) checkProtocolVersion(r *http.Request) {
	clientVersion := r.Header.Get(MCPProtocolVersionHeader)
	if clientVersion == "" {
		return
	}

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

func (h *HTTPHandler) cleanupSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		h.mu.Lock()
		now := time.Now()
		for id, info := range h.sessions {
			if now.Sub(info.lastActive) > 30*time.Minute {
				if info.transport != nil {
					info.transport.Close()
				}
				delete(h.sessions, id)
			}
		}
		h.mu.Unlock()
	}
}

func NewSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

package sse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/voocel/mcp-sdk-go/transport"
)

type Transport struct {
	url           string
	client        *http.Client
	messageBuffer chan []byte
	closeOnce     sync.Once
	closeFunc     func() error
	mu            sync.Mutex
}

func New(url string) *Transport {
	return &Transport{
		url:           url,
		client:        &http.Client{},
		messageBuffer: make(chan []byte, 100),
	}
}

func (t *Transport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned non-OK status: %d, body: %s", resp.StatusCode, body)
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

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to start event stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("server returned non-OK status: %d", resp.StatusCode)
	}

	t.closeFunc = resp.Body.Close

	go t.processEvents(ctx, resp.Body)

	return nil
}

func (t *Transport) processEvents(ctx context.Context, body io.ReadCloser) {
	defer body.Close()

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

					select {
					case t.messageBuffer <- []byte(data):
					case <-ctx.Done():
						return
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
		close(t.messageBuffer)
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
		close(t.messageBuffer)
	})

	return err
}

type Server struct {
	handler    transport.Handler
	httpServer *http.Server
	clients    map[string]chan []byte
	mu         sync.RWMutex
}

func NewServer(addr string, handler transport.Handler) *Server {
	s := &Server{
		handler: handler,
		clients: make(map[string]chan []byte),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		s.handleSSE(w, r)
	} else if r.Method == "POST" {
		s.handleMessage(w, r)
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	clientID := r.RemoteAddr

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	messageChan := make(chan []byte, 100)

	s.mu.Lock()
	s.clients[clientID] = messageChan
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, clientID)
		close(messageChan)
		s.mu.Unlock()
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	closeNotify := w.(http.CloseNotifier).CloseNotify()
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	for {
		select {
		case <-closeNotify:
			return
		case <-ctx.Done():
			return
		case msg, ok := <-messageChan:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	response, err := s.handler.HandleMessage(r.Context(), body)
	if err != nil {
		errorResp := struct {
			Error string `json:"error"`
		}{
			Error: err.Error(),
		}
		response, _ = json.Marshal(errorResp)
	}

	s.mu.RLock()
	for _, ch := range s.clients {
		select {
		case ch <- response:
		default:
		}
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(response)
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
	return s.httpServer.Shutdown(ctx)
}

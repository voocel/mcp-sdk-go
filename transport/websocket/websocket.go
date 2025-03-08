package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/voocel/mcp-sdk-go/transport"
)

type Transport struct {
	url            string
	conn           *websocket.Conn
	mu             sync.Mutex
	closeOnce      sync.Once
	messageBuffer  chan []byte
	pingInterval   time.Duration
	receiveTimeout time.Duration
}

type Option func(*Transport)

func WithPingInterval(interval time.Duration) Option {
	return func(t *Transport) {
		t.pingInterval = interval
	}
}

func WithReceiveTimeout(timeout time.Duration) Option {
	return func(t *Transport) {
		t.receiveTimeout = timeout
	}
}

func New(url string, options ...Option) *Transport {
	t := &Transport{
		url:            url,
		messageBuffer:  make(chan []byte, 100),
		pingInterval:   time.Second * 30,
		receiveTimeout: time.Second * 60,
	}

	for _, option := range options {
		option(t)
	}

	return t
}

func (t *Transport) Connect(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Second * 10,
	}
	conn, _, err := dialer.DialContext(ctx, t.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket server: %w", err)
	}

	t.conn = conn
	go t.readMessages(ctx)
	if t.pingInterval > 0 {
		go t.pingPeriodically(ctx)
	}

	return nil
}

func (t *Transport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		return fmt.Errorf("websocket connection not established")
	}

	return t.conn.WriteMessage(websocket.TextMessage, data)
}

func (t *Transport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-t.messageBuffer:
		if !ok {
			return nil, fmt.Errorf("connection closed")
		}
		return msg, nil
	}
}

func (t *Transport) readMessages(ctx context.Context) {
	defer close(t.messageBuffer)

	for {
		_, message, err := t.conn.ReadMessage()
		if err != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case t.messageBuffer <- message:
		}
	}
}

func (t *Transport) pingPeriodically(ctx context.Context) {
	ticker := time.NewTicker(t.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.mu.Lock()
			if t.conn != nil {
				err := t.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(time.Second))
				if err != nil {
					t.conn.Close()
					t.conn = nil
				}
			}
			t.mu.Unlock()
		}
	}
}

func (t *Transport) Close() error {
	var err error

	t.closeOnce.Do(func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.conn != nil {
			err = t.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				err = fmt.Errorf("failed to send close message: %w", err)
			}
			if closeErr := t.conn.Close(); closeErr != nil && err == nil {
				err = closeErr
			}

			t.conn = nil
		}
	})

	return err
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Server struct {
	handler    transport.Handler
	httpServer *http.Server
	clients    map[*websocket.Conn]bool
	mu         sync.RWMutex
}

func NewServer(addr string, handler transport.Handler) *Server {
	s := &Server{
		handler: handler,
		clients: make(map[*websocket.Conn]bool),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWebSocket)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	s.mu.Lock()
	s.clients[conn] = true
	s.mu.Unlock()

	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		response, err := s.handler.HandleMessage(ctx, message)
		if err != nil {
			errorResp := struct {
				Error string `json:"error"`
			}{
				Error: err.Error(),
			}
			response, _ = json.Marshal(errorResp)
		}
		if err := conn.WriteMessage(websocket.TextMessage, response); err != nil {
			break
		}
	}
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
	for conn := range s.clients {
		conn.Close()
	}
	s.clients = make(map[*websocket.Conn]bool)
	s.mu.Unlock()

	return s.httpServer.Shutdown(ctx)
}

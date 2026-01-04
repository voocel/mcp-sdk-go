package client_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport"
)

type inMemoryTransport struct {
	conn transport.Connection
}

func (t *inMemoryTransport) Connect(ctx context.Context) (transport.Connection, error) {
	return t.conn, nil
}

type inMemoryConn struct {
	incoming chan *protocol.JSONRPCMessage
	done     chan struct{}
	closed   atomic.Bool
	peer     *inMemoryConn
	session  string
}

func newInMemoryConn(session string) *inMemoryConn {
	return &inMemoryConn{
		incoming: make(chan *protocol.JSONRPCMessage, 64),
		done:     make(chan struct{}),
		session:  session,
	}
}

func newInMemoryTransportPair() (clientT transport.Transport, serverT transport.Transport) {
	c1 := newInMemoryConn("client")
	c2 := newInMemoryConn("server")
	c1.peer = c2
	c2.peer = c1
	return &inMemoryTransport{conn: c1}, &inMemoryTransport{conn: c2}
}

func (c *inMemoryConn) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	if c.closed.Load() {
		return nil, transport.ErrConnectionClosed
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, transport.ErrConnectionClosed
	case msg := <-c.incoming:
		return msg, nil
	}
}

func (c *inMemoryConn) Write(ctx context.Context, msg *protocol.JSONRPCMessage) error {
	if c.closed.Load() {
		return transport.ErrConnectionClosed
	}
	peer := c.peer
	if peer == nil {
		return transport.ErrConnectionClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-peer.done:
		return transport.ErrConnectionClosed
	case peer.incoming <- msg:
		return nil
	}
}

func (c *inMemoryConn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(c.done)
	return nil
}

func (c *inMemoryConn) SessionID() string {
	return c.session
}

func TestInitializeAndToolCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	}, nil)

	type Input struct {
		Name string `json:"name"`
	}
	type Output struct {
		Greeting string `json:"greeting"`
	}

	server.AddTool[Input, Output](mcpServer, &protocol.Tool{
		Name:        "greet",
		Description: "greet user",
	}, func(ctx context.Context, req *server.CallToolRequest, input Input) (*protocol.CallToolResult, Output, error) {
		return nil, Output{Greeting: "hi " + input.Name}, nil
	})

	clientT, serverT := newInMemoryTransportPair()

	ss, err := mcpServer.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect failed: %v", err)
	}
	defer ss.Close()

	mcpClient := client.NewClient(&client.ClientInfo{Name: "test-client", Version: "0.1.0"}, nil)
	cs, err := mcpClient.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	defer cs.Close()

	if cs.InitializeResult() == nil {
		t.Fatal("initialize result is nil")
	}
	if !protocol.IsVersionSupported(cs.InitializeResult().ProtocolVersion) {
		t.Fatalf("unsupported protocol version: %s", cs.InitializeResult().ProtocolVersion)
	}

	result, err := cs.CallTool(ctx, &protocol.CallToolParams{
		Name:      "greet",
		Arguments: map[string]any{"name": "bob"},
	})
	if err != nil {
		t.Fatalf("call tool failed: %v", err)
	}

	content, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type error: %T", result.StructuredContent)
	}
	if got, ok := content["greeting"].(string); !ok || got != "hi bob" {
		t.Fatalf("unexpected greeting: %v", content["greeting"])
	}
}

func TestCallToolAsTaskAndResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "test-server",
		Version: "1.0.0",
	}, &server.ServerOptions{
		TasksEnabled: true,
	})

	type Input struct {
		Text string `json:"text"`
	}
	type Output struct {
		Text string `json:"text"`
	}

	server.AddTool[Input, Output](mcpServer, &protocol.Tool{
		Name:        "echo",
		Description: "echo text",
		Execution: &protocol.ToolExecution{
			TaskSupport: protocol.TaskSupportOptional,
		},
	}, func(ctx context.Context, req *server.CallToolRequest, input Input) (*protocol.CallToolResult, Output, error) {
		time.Sleep(10 * time.Millisecond)
		return nil, Output{Text: input.Text}, nil
	})

	clientT, serverT := newInMemoryTransportPair()

	ss, err := mcpServer.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect failed: %v", err)
	}
	defer ss.Close()

	statusCh := make(chan protocol.TaskStatusNotificationParams, 8)

	mcpClient := client.NewClient(&client.ClientInfo{Name: "test-client", Version: "0.1.0"}, &client.ClientOptions{
		TasksEnabled: true,
		TaskStatusHandler: func(ctx context.Context, params *protocol.TaskStatusNotificationParams) {
			select {
			case statusCh <- *params:
			default:
			}
		},
	})
	cs, err := mcpClient.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	defer cs.Close()

	taskResult, err := cs.CallToolAsTask(ctx, &protocol.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("call tool as task failed: %v", err)
	}

	taskID := taskResult.Task.TaskID
	if taskID == "" {
		t.Fatal("task id is empty")
	}

	completed := false
	deadline := time.NewTimer(1 * time.Second)
	defer deadline.Stop()

	for !completed {
		select {
		case <-deadline.C:
			t.Fatal("timeout waiting for task completion")
		case status := <-statusCh:
			if status.TaskID == taskID && status.Status == protocol.TaskStatusCompleted {
				completed = true
			}
		}
	}

	var toolResult protocol.CallToolResult
	if err := cs.GetTaskResult(ctx, &protocol.TaskResultParams{TaskID: taskID}, &toolResult); err != nil {
		t.Fatalf("get task result failed: %v", err)
	}

	content, ok := toolResult.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type error: %T", toolResult.StructuredContent)
	}
	if got, ok := content["text"].(string); !ok || got != "hello" {
		t.Fatalf("unexpected result: %v", content["text"])
	}

	meta, ok := toolResult.Meta["io.modelcontextprotocol/related-task"].(map[string]any)
	if !ok {
		t.Fatalf("missing task meta: %v", toolResult.Meta)
	}
	if got, ok := meta["taskId"].(string); !ok || got != taskID {
		t.Fatalf("unexpected task meta: %v", meta["taskId"])
	}
}

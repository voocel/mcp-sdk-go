package stdio_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

const helperEnv = "MCP_STDIO_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		helperMain()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// helperMain runs the test binary as a real MCP stdio server subprocess.
func helperMain() {
	srv := server.New(&server.Options{Impl: protocol.Implementation{Name: "stdio-helper", Version: "1.0"}})
	type echoIn struct {
		Text string `json:"text"`
	}
	server.AddTool(srv, &protocol.Tool{Name: "echo", Description: "echo"},
		func(ctx context.Context, req *server.CallRequest, in echoIn) (protocol.ToolResponse, string, error) {
			return nil, in.Text, nil
		})
	server.AddTool(srv, &protocol.Tool{Name: "count", Description: "progress"},
		func(ctx context.Context, req *server.CallRequest, _ struct{}) (protocol.ToolResponse, string, error) {
			for i := 1; i <= 3; i++ {
				_ = req.ReportProgress(ctx, float64(i), 3, "step")
			}
			return nil, "done", nil
		})
	server.AddTool(srv, &protocol.Tool{Name: "touch", Description: "trigger update"},
		func(ctx context.Context, req *server.CallRequest, _ struct{}) (protocol.ToolResponse, string, error) {
			srv.ResourceUpdated("test://res")
			return nil, "ok", nil
		})
	// Resource subscriptions are only honored when resources are registered.
	srv.AddResource(&protocol.Resource{URI: "test://res", Name: "res"},
		func(ctx context.Context, req *server.ResourceRequest) (protocol.ResourceResponse, error) {
			return protocol.NewReadResourceResult(protocol.NewTextResourceContents("test://res", "v")), nil
		})
	_ = stdio.Serve(context.Background(), srv, nil)
}

// ---- in-process Serve tests over pipes ----

const metaFragment = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`

func callLine(id int, tool, args string) string {
	if args == "" {
		args = "{}"
	}
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{%s,"name":%q,"arguments":%s}}`, id, metaFragment, tool, args)
}

type harness struct {
	in   io.WriteCloser
	msgs chan *protocol.Message
	done chan error
}

func startServe(t *testing.T, srv *server.Server, opts *stdio.Options) *harness {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	o := stdio.Options{Reader: inR, Writer: outW}
	if opts != nil {
		o.MaxMessageBytes = opts.MaxMessageBytes
	}
	h := &harness{in: inW, msgs: make(chan *protocol.Message, 16), done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		_ = outR.Close()
	})
	go func() { h.done <- stdio.Serve(ctx, srv, &o) }()
	go func() {
		dec := json.NewDecoder(outR)
		for {
			var m protocol.Message
			if err := dec.Decode(&m); err != nil {
				return
			}
			h.msgs <- &m
		}
	}()
	return h
}

func (h *harness) send(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(h.in, line+"\n"); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func (h *harness) recv(t *testing.T) *protocol.Message {
	t.Helper()
	select {
	case m := <-h.msgs:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
		return nil
	}
}

func waitClosed(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", what)
	}
}

func testServer(handler func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error)) *server.Server {
	srv := server.New(&server.Options{Impl: protocol.Implementation{Name: "S", Version: "1"}})
	srv.AddTool(&protocol.Tool{Name: "t", InputSchema: protocol.JSONSchema{"type": "object"}}, handler)
	return srv
}

func TestServeRoundTrip(t *testing.T) {
	srv := testServer(func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
		return protocol.NewToolResultText("hello"), nil
	})
	h := startServe(t, srv, nil)

	h.send(t, callLine(1, "t", ""))
	m := h.recv(t)
	if m.ID != protocol.IntID(1) || m.Error != nil {
		t.Fatalf("unexpected response: %+v", m)
	}
	rt, err := protocol.PeekResultType(m.Result)
	if err != nil || rt != protocol.ResultTypeComplete {
		t.Fatalf("resultType = %q, %v", rt, err)
	}

	// CRLF framing must be tolerated.
	h.send(t, callLine(2, "t", "")+"\r")
	if m := h.recv(t); m.ID != protocol.IntID(2) {
		t.Fatalf("crlf response id: %v", m.ID)
	}
}

func TestServeParseError(t *testing.T) {
	srv := testServer(func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
		return protocol.NewToolResultText("x"), nil
	})
	h := startServe(t, srv, nil)

	h.send(t, "this is not json")
	m := h.recv(t)
	if m.Error == nil || m.Error.Code != protocol.CodeParseError {
		t.Fatalf("want -32700, got %+v", m)
	}
	if !m.ID.IsZero() {
		t.Fatalf("parse error must carry null id, got %v", m.ID)
	}
}

func TestServeConcurrency(t *testing.T) {
	gate := make(chan struct{})
	srv := testServer(func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
		if v, _ := req.Params.Arguments["wait"].(bool); v {
			<-gate
		}
		return protocol.NewToolResultText("ok"), nil
	})
	h := startServe(t, srv, nil)

	h.send(t, callLine(1, "t", `{"wait":true}`))
	h.send(t, callLine(2, "t", `{}`))

	// The fast request must complete while the slow one is still blocked.
	if m := h.recv(t); m.ID != protocol.IntID(2) {
		t.Fatalf("first completed id = %v, want 2", m.ID)
	}
	close(gate)
	if m := h.recv(t); m.ID != protocol.IntID(1) {
		t.Fatalf("second completed id = %v, want 1", m.ID)
	}
}

func TestServeShutdownEndsListenGracefully(t *testing.T) {
	srv := testServer(func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
		return protocol.NewToolResultText("x"), nil
	})
	h := startServe(t, srv, nil)

	h.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":5,"method":"subscriptions/listen","params":{%s,"notifications":{"toolsListChanged":true}}}`, metaFragment))
	if ack := h.recv(t); ack.Method != protocol.NotificationSubscriptionsAcknowledged {
		t.Fatalf("first message = %q", ack.Method)
	}

	// Shutdown runs while Serve is still live, so both teardown messages reach
	// the wire; cancelling Serve first would have suppressed them.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if m := h.recv(t); m.Method != protocol.NotificationCancelled {
		t.Fatalf("teardown first message = %+v, want notifications/cancelled", m)
	}
	if m := h.recv(t); m.Kind() != protocol.KindResponse || m.ID != protocol.IntID(5) {
		t.Fatalf("teardown final message = %+v, want the listen result", m)
	}
}

func TestServeCancelled(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	srv := testServer(func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
		if _, ok := req.Params.Arguments["block"]; ok {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		}
		return protocol.NewToolResultText("ok"), nil
	})
	h := startServe(t, srv, nil)

	h.send(t, callLine(1, "t", `{"block":true}`))
	waitClosed(t, started, "handler start")

	note, err := protocol.NewNotification(protocol.NotificationCancelled,
		protocol.CancelledParams{RequestID: protocol.IntID(1)})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(note)
	h.send(t, string(raw))
	waitClosed(t, cancelled, "handler cancellation")

	// No response may be written for the cancelled request; the next message
	// on the wire is the reply to a fresh request.
	h.send(t, callLine(2, "t", `{}`))
	if m := h.recv(t); m.ID != protocol.IntID(2) {
		t.Fatalf("got message for cancelled request: %+v", m)
	}
}

func TestServeCancelWhileSaturated(t *testing.T) {
	started := make(chan struct{})
	srv := server.New(&server.Options{Impl: protocol.Implementation{Name: "S", Version: "1"}, MaxConcurrency: 1})
	srv.AddTool(&protocol.Tool{Name: "t", InputSchema: protocol.JSONSchema{"type": "object"}},
		func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
			if _, ok := req.Params.Arguments["block"]; ok {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return protocol.NewToolResultText("ok"), nil
		})
	h := startServe(t, srv, nil)

	// Saturate the single slot, queue a second request behind it.
	h.send(t, callLine(1, "t", `{"block":true}`))
	waitClosed(t, started, "handler start")
	h.send(t, callLine(2, "t", `{}`))

	// The dispatch loop must still be reading: cancelling the blocker frees
	// the slot and the queued request completes.
	note, err := protocol.NewNotification(protocol.NotificationCancelled,
		protocol.CancelledParams{RequestID: protocol.IntID(1)})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(note)
	h.send(t, string(raw))

	if m := h.recv(t); m.ID != protocol.IntID(2) {
		t.Fatalf("queued request never completed, got: %+v", m)
	}
}

func TestServeEOF(t *testing.T) {
	started := make(chan struct{})
	srv := testServer(func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	h := startServe(t, srv, nil)

	h.send(t, callLine(1, "t", ""))
	waitClosed(t, started, "handler start")
	_ = h.in.Close() // EOF must abort in-flight work and return nil

	select {
	case err := <-h.done:
		if err != nil {
			t.Fatalf("Serve on EOF = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after EOF")
	}
}

func TestServeMessageTooLarge(t *testing.T) {
	srv := testServer(func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
		return protocol.NewToolResultText("x"), nil
	})
	h := startServe(t, srv, &stdio.Options{MaxMessageBytes: 64})

	h.send(t, callLine(1, "t", "")) // well over 64 bytes
	select {
	case err := <-h.done:
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("Serve = %v, want size error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not fail on oversized message")
	}
}

// ---- Command against a real subprocess ----

func TestCommandRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	tr, err := stdio.NewCommand(cmd, &stdio.CommandOptions{Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}

	var progress atomic.Int32
	c := client.New(tr, &client.Options{
		Info:       &protocol.Implementation{Name: "test-client", Version: "1"},
		OnProgress: func(p *protocol.ProgressParams) { progress.Add(1) },
	})
	closed := false
	defer func() {
		if !closed {
			_ = c.Close()
		}
	}()

	lst, err := c.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(lst.Tools) != 3 {
		t.Fatalf("tools = %d, want 3", len(lst.Tools))
	}

	res, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "hi stdio"}})
	if err != nil {
		t.Fatalf("CallTool echo: %v", err)
	}
	if res.StructuredContent != "hi stdio" {
		t.Fatalf("echo structuredContent: %v", res.StructuredContent)
	}

	// Progress notifications route back by progressToken.
	if _, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "count"}); err != nil {
		t.Fatalf("CallTool count: %v", err)
	}
	if got := progress.Load(); got != 3 {
		t.Fatalf("progress notifications = %d, want 3", got)
	}

	// Subscription notifications route back by _meta.subscriptionId.
	sub, err := c.Listen(ctx, protocol.SubscriptionFilter{ResourceSubscriptions: []string{"test://res"}})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if _, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "touch"}); err != nil {
		t.Fatalf("CallTool touch: %v", err)
	}
	select {
	case ev, ok := <-sub.Events():
		if !ok {
			t.Fatalf("subscription closed early: %v", sub.Err())
		}
		if uri, ok := ev.ResourceUpdated(); !ok || uri != "test://res" {
			t.Fatalf("event: %+v", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for subscription event")
	}
	_ = sub.Close()

	// Close shuts stdin; the helper exits on EOF and Wait reports success.
	closed = true
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCommandSubscriptionOverflowEndsStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	tr, err := stdio.NewCommand(cmd, &stdio.CommandOptions{Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	c := client.New(tr, &client.Options{Info: &protocol.Implementation{Name: "test-client", Version: "1"}})
	defer c.Close()

	sub, err := c.Listen(ctx, protocol.SubscriptionFilter{ResourceSubscriptions: []string{"test://res"}})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	// Flood the subscription while nobody reads its events: far more than the
	// stream and subscription buffers hold.
	for range 200 {
		if _, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "touch"}); err != nil {
			t.Fatalf("CallTool touch: %v", err)
		}
	}

	// Notifications must not vanish silently: the stream ends with an error.
	for range sub.Events() {
	}
	if err := sub.Err(); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("Err after overflow = %v, want a subscription overflow error", err)
	}
}

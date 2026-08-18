package mem_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
	"github.com/voocel/mcp-sdk-go/transport/mem"
)

var _ transport.Transport = (*mem.Transport)(nil)

// handlerFunc adapts a function to mem.Handler.
type handlerFunc func(context.Context, *protocol.Message, func(*protocol.Message) error)

func (f handlerFunc) Handle(ctx context.Context, msg *protocol.Message, emit func(*protocol.Message) error) {
	f(ctx, msg, emit)
}

func request(t *testing.T) *transport.Request {
	t.Helper()
	msg, err := protocol.NewRequest(protocol.StringID("1"), protocol.MethodToolsList, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	return &transport.Request{Message: msg}
}

// TestRequestStream covers the contract every transport owes the client: the
// notifications a handler emits arrive in order, then the response, then EOF.
func TestRequestStream(t *testing.T) {
	tr := mem.New(handlerFunc(func(_ context.Context, msg *protocol.Message, emit func(*protocol.Message) error) {
		note, err := protocol.NewNotification(protocol.NotificationProgress,
			protocol.ProgressParams{ProgressToken: protocol.StringID("pt-1"), Progress: 0.5})
		if err != nil {
			t.Error(err)
			return
		}
		if err := emit(note); err != nil {
			t.Error(err)
			return
		}
		out, err := protocol.NewResponse(msg.ID, &protocol.EmptyResult{})
		if err != nil {
			t.Error(err)
			return
		}
		if err := emit(out); err != nil {
			t.Error(err)
		}
	}))
	defer tr.Close()

	stream, err := tr.Do(context.Background(), request(t))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	first, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind() != protocol.KindNotification || first.Method != protocol.NotificationProgress {
		t.Fatalf("first message = %+v, want a progress notification", first)
	}
	second, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if second.Kind() != protocol.KindResponse || second.ID != protocol.StringID("1") {
		t.Fatalf("second message = %+v, want the response", second)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("after the response: err = %v, want io.EOF", err)
	}
}

// TestNotificationDelivery checks the fire-and-forget path: a message without
// an ID reaches the handler but yields no stream.
func TestNotificationDelivery(t *testing.T) {
	delivered := make(chan string, 1)
	tr := mem.New(handlerFunc(func(_ context.Context, msg *protocol.Message, _ func(*protocol.Message) error) {
		delivered <- msg.Method
	}))
	defer tr.Close()

	msg, err := protocol.NewNotification(protocol.NotificationToolsListChanged, nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := tr.Do(context.Background(), &transport.Request{Message: msg})
	if err != nil {
		t.Fatal(err)
	}
	if stream != nil {
		t.Fatalf("stream = %v, want nil for a notification", stream)
	}
	select {
	case method := <-delivered:
		if method != protocol.NotificationToolsListChanged {
			t.Fatalf("delivered %q", method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification never reached the handler")
	}
}

// TestCloseCancelsHandler pins the documented semantics: closing the stream is
// the cancellation signal, mirroring an HTTP client hanging up.
func TestCloseCancelsHandler(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	tr := mem.New(handlerFunc(func(ctx context.Context, _ *protocol.Message, _ func(*protocol.Message) error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
	}))
	defer tr.Close()

	stream, err := tr.Do(context.Background(), request(t))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the handler")
	}
	// Close is idempotent, and a cancelled stream ends rather than hanging.
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("Recv after Close: err = %v, want io.EOF", err)
	}
}

// TestEmitAfterCancellationFails ensures a handler that outlives its stream
// learns about it through emit rather than blocking forever. The sends must
// outnumber the channel buffer: until it fills, emit's select can still take
// the send branch.
func TestEmitAfterCancellationFails(t *testing.T) {
	emitErr := make(chan error, 1)
	tr := mem.New(handlerFunc(func(_ context.Context, msg *protocol.Message, emit func(*protocol.Message) error) {
		out, err := protocol.NewResponse(msg.ID, &protocol.EmptyResult{})
		if err != nil {
			emitErr <- err
			return
		}
		for range 64 {
			if err := emit(out); err != nil {
				emitErr <- err
				return
			}
		}
		emitErr <- nil
	}))
	defer tr.Close()

	stream, err := tr.Do(context.Background(), request(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-emitErr:
		if err == nil {
			t.Fatal("emit kept succeeding on a closed stream")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("emit blocked on a closed stream")
	}
}

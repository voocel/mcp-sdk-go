package streamable

import (
	"context"
	"net/http"
)

// StreamWriter abstracts how SSE events are written to the client.
// This allows separating core HTTP handling from optional features like stream resumption.
type StreamWriter interface {
	// Init prepares the writer for a new stream.
	// It sets response headers and returns any events that should be replayed.
	Init(ctx context.Context, w http.ResponseWriter, streamID string, lastEventID string) (replay [][]byte, err error)

	// Write sends an event to the client.
	// If final is true, this is the last event for this stream.
	Write(ctx context.Context, data []byte, final bool) error

	// Close cleans up any resources associated with this writer.
	Close() error
}

// StreamWriterFactory creates StreamWriter instances for a session.
type StreamWriterFactory interface {
	// Create returns a new StreamWriter for the given session.
	Create(sessionID string) StreamWriter

	// OnSessionClose is called when a session is closed.
	OnSessionClose(ctx context.Context, sessionID string)
}

// SimpleWriter is a basic StreamWriter without resumption support.
type SimpleWriter struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	streamID  string
	retryMs   int64
	initDone  bool
}

// SimpleWriterFactory creates SimpleWriter instances.
type SimpleWriterFactory struct {
	RetryMs int64 // retry hint in milliseconds, 0 to disable
}

// NewSimpleWriterFactory creates a factory for simple writers.
func NewSimpleWriterFactory() *SimpleWriterFactory {
	return &SimpleWriterFactory{RetryMs: 1000}
}

func (f *SimpleWriterFactory) Create(sessionID string) StreamWriter {
	return &SimpleWriter{retryMs: f.RetryMs}
}

func (f *SimpleWriterFactory) OnSessionClose(ctx context.Context, sessionID string) {
	// No cleanup needed for simple writer
}

func (sw *SimpleWriter) Init(ctx context.Context, w http.ResponseWriter, streamID string, lastEventID string) ([][]byte, error) {
	sw.w = w
	sw.streamID = streamID

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrStreamingUnsupported
	}
	sw.flusher = flusher

	// Simple writer does not support replay
	if lastEventID != "" {
		return nil, ErrReplayUnsupported
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	sw.initDone = true

	return nil, nil
}

func (sw *SimpleWriter) Write(ctx context.Context, data []byte, final bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	evt := Event{Name: "message", Data: data}
	if err := writeEvent(sw.w, evt); err != nil {
		return err
	}

	if final && sw.retryMs > 0 {
		_ = writeEvent(sw.w, Event{Retry: formatRetry(sw.retryMs)})
	}

	return nil
}

func (sw *SimpleWriter) Close() error {
	return nil
}


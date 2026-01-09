package streamable

import (
	"context"
	"net/http"
	"sync"
)

// ResumableWriter is a StreamWriter that supports stream resumption.
type ResumableWriter struct {
	store     EventStore
	sessionID string
	streamID  string
	retryMs   int64

	mu          sync.Mutex
	w           http.ResponseWriter
	flusher     http.Flusher
	lastEventID int
	initDone    bool
}

// ResumableWriterFactory creates ResumableWriter instances.
type ResumableWriterFactory struct {
	Store   EventStore
	RetryMs int64

	mu       sync.Mutex
	sessions map[string]bool
}

// NewResumableWriterFactory creates a factory with the given event store.
func NewResumableWriterFactory(store EventStore) *ResumableWriterFactory {
	return &ResumableWriterFactory{
		Store:    store,
		RetryMs:  1000,
		sessions: make(map[string]bool),
	}
}

func (f *ResumableWriterFactory) Create(sessionID string) StreamWriter {
	f.mu.Lock()
	f.sessions[sessionID] = true
	f.mu.Unlock()

	return &ResumableWriter{
		store:       f.Store,
		sessionID:   sessionID,
		retryMs:     f.RetryMs,
		lastEventID: -1,
	}
}

func (f *ResumableWriterFactory) OnSessionClose(ctx context.Context, sessionID string) {
	f.mu.Lock()
	delete(f.sessions, sessionID)
	f.mu.Unlock()

	if f.Store != nil {
		_ = f.Store.SessionClosed(ctx, sessionID)
	}
}

func (rw *ResumableWriter) Init(ctx context.Context, w http.ResponseWriter, streamID string, lastEventID string) ([][]byte, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	rw.w = w
	rw.streamID = streamID

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrStreamingUnsupported
	}
	rw.flusher = flusher

	// Open stream in event store
	if err := rw.store.Open(ctx, rw.sessionID, streamID); err != nil {
		return nil, err
	}

	var replay [][]byte
	if lastEventID != "" {
		sid, idx, ok := parseEventID(lastEventID)
		if !ok {
			return nil, ErrInvalidEventID
		}
		if sid != streamID {
			return nil, ErrInvalidEventID
		}

		events, err := rw.store.After(ctx, rw.sessionID, streamID, idx)
		if err != nil {
			return nil, err
		}
		replay = events
		rw.lastEventID = idx + len(events)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	rw.initDone = true

	return replay, nil
}

func (rw *ResumableWriter) Write(ctx context.Context, data []byte, final bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	rw.mu.Lock()
	defer rw.mu.Unlock()

	// Store event and get ID
	eventID, err := rw.store.Append(ctx, rw.sessionID, rw.streamID, data)
	if err != nil {
		return err
	}

	// Skip if already sent (deduplication)
	if eventID <= rw.lastEventID {
		return nil
	}
	rw.lastEventID = eventID

	evt := Event{
		Name: "message",
		Data: data,
		ID:   formatEventID(rw.streamID, eventID),
	}
	if err := writeEvent(rw.w, evt); err != nil {
		return err
	}

	if final && rw.retryMs > 0 {
		_ = writeEvent(rw.w, Event{Retry: formatRetry(rw.retryMs)})
	}

	return nil
}

func (rw *ResumableWriter) Close() error {
	return nil
}


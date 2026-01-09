package streamable

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Common errors
var (
	ErrStreamingUnsupported = errors.New("streaming unsupported")
	ErrReplayUnsupported    = errors.New("replay unsupported")
	ErrInvalidEventID       = errors.New("invalid event ID")
	ErrEventsPurged         = errors.New("events purged")
)

// Event represents a single SSE event.
type Event struct {
	Name  string
	ID    string
	Data  []byte
	Retry string
}

func (e Event) Empty() bool {
	return e.Name == "" && e.ID == "" && len(e.Data) == 0 && e.Retry == ""
}

func writeEvent(w io.Writer, evt Event) error {
	var b bytes.Buffer
	if evt.Name != "" {
		fmt.Fprintf(&b, "event: %s\n", evt.Name)
	}
	if evt.ID != "" {
		fmt.Fprintf(&b, "id: %s\n", evt.ID)
	}
	if evt.Retry != "" {
		fmt.Fprintf(&b, "retry: %s\n", evt.Retry)
	}
	if len(evt.Data) == 0 {
		b.WriteString("data: \n\n")
	} else {
		for _, line := range bytes.Split(evt.Data, []byte("\n")) {
			fmt.Fprintf(&b, "data: %s\n", line)
		}
		b.WriteString("\n")
	}
	if _, err := w.Write(b.Bytes()); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func scanEvents(r io.Reader, handle func(Event, error) bool) {
	scanner := bufio.NewScanner(r)
	const maxTokenSize = 1 * 1024 * 1024
	scanner.Buffer(nil, maxTokenSize)

	var (
		eventKey = []byte("event")
		idKey    = []byte("id")
		dataKey  = []byte("data")
		retryKey = []byte("retry")
	)

	var (
		evt     Event
		dataBuf *bytes.Buffer
	)

	flushData := func() {
		if dataBuf != nil {
			evt.Data = dataBuf.Bytes()
			dataBuf = nil
		}
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			flushData()
			if !evt.Empty() {
				if !handle(evt, nil) {
					return
				}
			}
			evt = Event{}
			continue
		}
		if line[0] == ':' {
			continue
		}
		before, after, found := bytes.Cut(line, []byte{':'})
		if !found {
			handle(Event{}, fmt.Errorf("malformed line in SSE stream: %q", string(line)))
			return
		}
		if !bytes.Equal(before, dataKey) {
			flushData()
		}
		after = bytes.TrimSpace(after)
		switch {
		case bytes.Equal(before, eventKey):
			evt.Name = string(after)
		case bytes.Equal(before, idKey):
			evt.ID = string(after)
		case bytes.Equal(before, retryKey):
			evt.Retry = string(after)
		case bytes.Equal(before, dataKey):
			if dataBuf != nil {
				dataBuf.WriteByte('\n')
				dataBuf.Write(after)
			} else {
				dataBuf = new(bytes.Buffer)
				dataBuf.Write(after)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		handle(Event{}, err)
		return
	}
	flushData()
	if !evt.Empty() {
		handle(evt, nil)
	}
}

// formatEventID encodes stream ID and index as "<streamID>_<idx>".
func formatEventID(streamID string, idx int) string {
	return fmt.Sprintf("%s_%d", streamID, idx)
}

// parseEventID decodes "<streamID>_<idx>".
func parseEventID(eventID string) (streamID string, idx int, ok bool) {
	parts := strings.Split(eventID, "_")
	if len(parts) != 2 {
		return "", 0, false
	}
	streamID = parts[0]
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 0 {
		return "", 0, false
	}
	return streamID, n, true
}

// formatRetry formats a retry value in milliseconds.
func formatRetry(ms int64) string {
	return strconv.FormatInt(ms, 10)
}

// EventStore tracks data for SSE stream resumption.
// All methods must be safe for concurrent use.
type EventStore interface {
	Open(ctx context.Context, sessionID, streamID string) error
	Append(ctx context.Context, sessionID, streamID string, data []byte) (int, error)
	After(ctx context.Context, sessionID, streamID string, index int) ([][]byte, error)
	SessionClosed(ctx context.Context, sessionID string) error
}

type dataList struct {
	size  int
	first int
	data  [][]byte
}

func (dl *dataList) appendData(d []byte) int {
	dl.data = append(dl.data, d)
	dl.size += len(d)
	return dl.first + len(dl.data) - 1
}

func (dl *dataList) removeFirst() int {
	if len(dl.data) == 0 {
		return 0
	}
	n := len(dl.data[0])
	dl.size -= n
	dl.data[0] = nil
	dl.data = dl.data[1:]
	dl.first++
	return n
}

// MemoryEventStore is an in-memory EventStore with a global size limit.
type MemoryEventStore struct {
	mu       sync.Mutex
	maxBytes int
	nBytes   int
	store    map[string]map[string]*dataList
}

const defaultMaxEventBytes = 10 << 20 // 10 MiB

// NewMemoryEventStore creates a MemoryEventStore with default limits.
func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		maxBytes: defaultMaxEventBytes,
		store:    make(map[string]map[string]*dataList),
	}
}

// MaxBytes returns the maximum number of bytes retained.
func (s *MemoryEventStore) MaxBytes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxBytes
}

// SetMaxBytes sets the maximum number of bytes retained.
func (s *MemoryEventStore) SetMaxBytes(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 {
		s.maxBytes = defaultMaxEventBytes
	} else {
		s.maxBytes = n
	}
	s.purgeLocked()
}

func (s *MemoryEventStore) Open(_ context.Context, sessionID, streamID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initLocked(sessionID, streamID)
	return nil
}

func (s *MemoryEventStore) Append(_ context.Context, sessionID, streamID string, data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dl := s.initLocked(sessionID, streamID)
	idx := dl.appendData(data)
	s.nBytes += len(data)
	s.purgeLocked()
	return idx, nil
}

func (s *MemoryEventStore) After(_ context.Context, sessionID, streamID string, index int) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	streamMap, ok := s.store[sessionID]
	if !ok {
		return nil, fmt.Errorf("unknown session %q", sessionID)
	}
	dl, ok := streamMap[streamID]
	if !ok {
		return nil, fmt.Errorf("unknown stream %q", streamID)
	}
	start := index + 1
	if dl.first > start {
		return nil, ErrEventsPurged
	}
	if start < dl.first {
		start = dl.first
	}
	offset := start - dl.first
	if offset >= len(dl.data) {
		return nil, nil
	}
	out := make([][]byte, len(dl.data[offset:]))
	copy(out, dl.data[offset:])
	return out, nil
}

func (s *MemoryEventStore) SessionClosed(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, dl := range s.store[sessionID] {
		s.nBytes -= dl.size
	}
	delete(s.store, sessionID)
	return nil
}

func (s *MemoryEventStore) initLocked(sessionID, streamID string) *dataList {
	streams, ok := s.store[sessionID]
	if !ok {
		streams = make(map[string]*dataList)
		s.store[sessionID] = streams
	}
	dl, ok := streams[streamID]
	if !ok {
		dl = &dataList{}
		streams[streamID] = dl
	}
	return dl
}

func (s *MemoryEventStore) purgeLocked() {
	if s.maxBytes <= 0 || s.nBytes <= s.maxBytes {
		return
	}
	for s.nBytes > s.maxBytes {
		removed := false
		for _, streams := range s.store {
			for _, dl := range streams {
				if len(dl.data) == 0 {
					continue
				}
				s.nBytes -= dl.removeFirst()
				removed = true
				if s.nBytes <= s.maxBytes {
					return
				}
			}
		}
		if !removed {
			return
		}
	}
}

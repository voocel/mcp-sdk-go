package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// ErrNotFound is returned by Store.Get for unknown (or expired) task IDs.
var ErrNotFound = errors.New("tasks: task not found")

// Store persists task snapshots. Implementations must be safe for concurrent
// use. An external store makes task state visible across server instances;
// note that the input_required rendezvous (tasks/update waking a blocked
// handler) is in-memory, so multi-instance deployments need sticky routing for
// interactive tasks.
type Store interface {
	// Put creates or replaces a task snapshot.
	Put(ctx context.Context, t *DetailedTask) error
	// Get returns a copy of the task, or ErrNotFound.
	Get(ctx context.Context, taskID string) (*DetailedTask, error)
	// Delete removes a task; deleting an unknown task is not an error.
	Delete(ctx context.Context, taskID string) error
}

// MemStore is the in-process Store. Tasks expire TTLMs after their last
// update; expired entries are evicted lazily.
type MemStore struct {
	mu sync.Mutex
	m  map[string]memEntry
}

type memEntry struct {
	task    DetailedTask
	expires time.Time // zero = never
}

func NewMemStore() *MemStore {
	return &MemStore{m: make(map[string]memEntry)}
}

func (s *MemStore) Put(ctx context.Context, t *DetailedTask) error {
	e := memEntry{task: *cloneTask(t)}
	// Only terminal tasks expire: evicting a live task would silently drop
	// its eventual result. ttlMs counts from createdAt per the draft, so the
	// eviction time is anchored there, not at the write.
	if t.Status.Terminal() && t.TTLMs != nil && *t.TTLMs >= 0 {
		anchor := time.Now()
		if created, err := time.Parse(time.RFC3339Nano, t.CreatedAt); err == nil {
			anchor = created
		}
		e.expires = anchor.Add(time.Duration(*t.TTLMs) * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.m[t.TaskID] = e
	return nil
}

func (s *MemStore) Get(ctx context.Context, taskID string) (*DetailedTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[taskID]
	if !ok {
		return nil, ErrNotFound
	}
	if !e.expires.IsZero() && time.Now().After(e.expires) {
		delete(s.m, taskID)
		return nil, ErrNotFound
	}
	return cloneTask(&e.task), nil
}

// cloneTask deep-copies the mutable fields — including json.RawMessage bytes
// nested in input requests and errors — so callers mutating a snapshot cannot
// corrupt the stored entry through shared backing arrays.
func cloneTask(t *DetailedTask) *DetailedTask {
	cp := *t
	if t.Meta.ServerInfo != nil {
		si := *t.Meta.ServerInfo
		cp.Meta.ServerInfo = &si
	}
	if t.Meta.Extra != nil {
		cp.Meta.Extra = make(map[string]json.RawMessage, len(t.Meta.Extra))
		for k, v := range t.Meta.Extra {
			cp.Meta.Extra[k] = bytes.Clone(v)
		}
	}
	if t.InputRequests != nil {
		cp.InputRequests = make(protocol.InputRequests, len(t.InputRequests))
		for k, v := range t.InputRequests {
			v.Params = bytes.Clone(v.Params)
			cp.InputRequests[k] = v
		}
	}
	cp.Result = bytes.Clone(t.Result)
	if t.TTLMs != nil {
		ttl := *t.TTLMs
		cp.TTLMs = &ttl
	}
	if t.Error != nil {
		e := *t.Error
		e.Data = bytes.Clone(e.Data)
		cp.Error = &e
	}
	return &cp
}

func (s *MemStore) Delete(ctx context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, taskID)
	return nil
}

// sweepLocked drops expired entries so a write-heavy server does not
// accumulate garbage between Gets.
func (s *MemStore) sweepLocked() {
	now := time.Now()
	for id, e := range s.m {
		if !e.expires.IsZero() && now.After(e.expires) {
			delete(s.m, id)
		}
	}
}

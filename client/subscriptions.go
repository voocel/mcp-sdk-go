package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// Event is one notification delivered on a subscription stream.
type Event struct {
	Method string
	Params json.RawMessage
}

// ResourceUpdated decodes the event as notifications/resources/updated.
func (e Event) ResourceUpdated() (uri string, ok bool) {
	if e.Method != protocol.NotificationResourcesUpdated {
		return "", false
	}
	var p protocol.ResourceUpdatedParams
	if err := json.Unmarshal(e.Params, &p); err != nil {
		return "", false
	}
	return p.URI, true
}

// Subscription is one open subscriptions/listen stream.
type Subscription struct {
	ack    protocol.SubscriptionFilter
	events chan Event
	stream transport.Stream
	done   chan struct{}
	once   sync.Once

	mu  sync.Mutex
	err error
}

// Ack returns the filter subset the server agreed to honor. Compare it with
// what you requested to detect unsupported notification types.
func (s *Subscription) Ack() protocol.SubscriptionFilter { return s.ack }

// Events yields notifications until the stream ends. The channel closes on
// graceful server teardown, cancellation and disconnects alike; check Err
// afterwards.
func (s *Subscription) Events() <-chan Event { return s.events }

// Err reports why Events closed: nil for graceful teardown or client Close,
// otherwise the transport error. The spec leaves reconnection to the caller
// (re-issue Listen; the server holds no subscription state).
func (s *Subscription) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close cancels the subscription. On HTTP this closes the response stream —
// the wire-level cancellation signal; on stdio the transport sends
// notifications/cancelled. Events still buffered are discarded.
func (s *Subscription) Close() error {
	s.once.Do(func() { close(s.done) })
	return s.stream.Close()
}

// Listen opens a subscriptions/listen stream. It blocks until the server's
// acknowledgment (the required first message) arrives.
func (c *Client) Listen(ctx context.Context, filter protocol.SubscriptionFilter) (*Subscription, error) {
	pm, err := paramsToMap(protocol.ListenParams{Notifications: filter})
	if err != nil {
		return nil, err
	}
	id := protocol.IntID(c.nextID.Add(1))
	if err := c.stampMeta(pm, id); err != nil {
		return nil, err
	}
	rawParams, err := json.Marshal(pm)
	if err != nil {
		return nil, err
	}
	msg := &protocol.Message{JSONRPC: protocol.JSONRPCVersion, ID: id, Method: protocol.MethodSubscriptionsListen, Params: rawParams}
	headers, err := c.headers(protocol.MethodSubscriptionsListen, pm)
	if err != nil {
		return nil, err
	}
	stream, err := c.t.Do(ctx, &transport.Request{Message: msg, Headers: headers})
	if err != nil {
		return nil, err
	}

	first, err := stream.Recv()
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("client: subscription stream ended before acknowledgment: %w", err)
	}
	if first.Kind() == protocol.KindError {
		stream.Close()
		return nil, first.Error
	}
	if first.Method != protocol.NotificationSubscriptionsAcknowledged {
		stream.Close()
		return nil, fmt.Errorf("client: expected acknowledgment, got %q", first.Method)
	}
	var ack protocol.AckParams
	if err := json.Unmarshal(first.Params, &ack); err != nil {
		stream.Close()
		return nil, err
	}

	sub := &Subscription{
		ack:    ack.Notifications,
		events: make(chan Event, 16),
		stream: stream,
		done:   make(chan struct{}),
	}
	go sub.run()
	return sub, nil
}

func (s *Subscription) run() {
	defer close(s.events)
	// Graceful teardown and stream errors must release the transport stream
	// even if the caller never calls Close (Close's own Close is idempotent).
	defer s.stream.Close()
	for {
		m, err := s.stream.Recv()
		if err != nil {
			if err != io.EOF {
				s.mu.Lock()
				s.err = err
				s.mu.Unlock()
			}
			return
		}
		switch m.Kind() {
		case protocol.KindNotification:
			if m.Method == protocol.NotificationCancelled {
				// Server-initiated teardown marker, not a change event; how
				// the stream ends follows as a result or an error.
				continue
			}
			// Blocking send is the natural backpressure; done unblocks it when
			// the caller closes without draining, so run never leaks.
			select {
			case s.events <- Event{Method: m.Method, Params: m.Params}:
			case <-s.done:
				return
			}
		case protocol.KindResponse:
			// Graceful server teardown: the empty complete result.
			return
		case protocol.KindError:
			s.mu.Lock()
			s.err = m.Error
			s.mu.Unlock()
			return
		}
	}
}

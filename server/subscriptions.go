package server

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sync"

	"github.com/voocel/mcp-sdk-go/protocol"
)

const (
	topicTools     = "tools"
	topicPrompts   = "prompts"
	topicResources = "resources"
)

func topicResource(uri string) string { return "resource:" + uri }

// hub fans notifications out to active subscriptions/listen streams. All
// subscription state is scoped to the listen request's goroutine, which keeps
// the server stateless in the protocol sense.
type hub struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

type subscriber struct {
	topics map[string]struct{}
	ch     chan hubEvent
	over   chan struct{} // closed on overflow; the stream must end visibly
	once   sync.Once
}

type hubEvent struct {
	method string
	params map[string]any
}

func newHub() *hub {
	return &hub{subs: make(map[*subscriber]struct{})}
}

func (h *hub) subscribe(topics []string, buf int) *subscriber {
	sub := &subscriber{
		topics: make(map[string]struct{}, len(topics)),
		ch:     make(chan hubEvent, buf),
		over:   make(chan struct{}),
	}
	for _, t := range topics {
		sub.topics[t] = struct{}{}
	}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *hub) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
}

// publish delivers to every subscriber of topic. Sends never block: when a
// slow subscriber's buffer is full its subscription is marked overflowed and
// the listen stream terminates with a visible error instead of dropping the
// event silently — a dropped notification (e.g. a task's only input_required)
// could otherwise stall a subscription-only client forever. The client
// re-listens and re-syncs.
func (h *hub) publish(topic, method string, params any) error {
	var pm map[string]any
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("server: publish %s: params do not marshal: %w", method, err)
		}
		if err := json.Unmarshal(raw, &pm); err != nil {
			return fmt.Errorf("server: publish %s: params must marshal to a JSON object: %w", method, err)
		}
	}
	ev := hubEvent{method: method, params: pm}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.subs {
		if _, ok := sub.topics[topic]; !ok {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			sub.once.Do(func() { close(sub.over) })
		}
	}
	return nil
}

const subscriberBuffer = 64

func (s *Server) handleListen(ctx context.Context, req *Request) (protocol.Result, error) {
	var p protocol.ListenParams
	if err := unmarshalParams(req.rawParams, &p); err != nil {
		return nil, err
	}

	honored, topics, err := s.honorFilter(req, &p.Notifications)
	if err != nil {
		return nil, err
	}
	sub := s.hub.subscribe(topics, subscriberBuffer)
	defer s.hub.unsubscribe(sub)

	// The acknowledgment must be the first message on the stream.
	ack, err := protocol.NewNotification(protocol.NotificationSubscriptionsAcknowledged, protocol.AckParams{
		Meta:          protocol.NotificationMeta{SubscriptionID: req.id},
		Notifications: honored,
	})
	if err != nil {
		return nil, err
	}
	if err := req.emit(ack); err != nil {
		return nil, err
	}

	for {
		select {
		case <-ctx.Done():
			// Torn down from the outside, so no cancelled notification (see
			// emitCancelled); the empty result closes the stream.
			return listenEnded(req), nil
		case <-s.closing:
			// Server-initiated teardown: the spec requires the cancelled
			// notification, and asks for a complete result to mark the end as
			// graceful rather than a dropped connection.
			emitCancelled(req, "server shutting down")
			return listenEnded(req), nil
		case <-sub.over:
			// A notification was lost; ending the stream with an error is the
			// only honest outcome — the client re-listens and re-syncs.
			emitCancelled(req, "subscription overflowed")
			return nil, protocol.Errorf(protocol.CodeInternal,
				"subscription overflowed: notifications were dropped; re-listen and re-sync")
		case ev := <-sub.ch:
			msg, err := notificationWithSubscriptionID(ev, req.id)
			if err != nil {
				// An event we cannot encode is a dropped notification; like
				// overflow, it must end the stream visibly, not vanish.
				emitCancelled(req, "notification could not be encoded")
				return nil, protocol.Errorf(protocol.CodeInternal,
					"failed to encode subscription notification %s: %v", ev.method, err)
			}
			if err := req.emit(msg); err != nil {
				return listenEnded(req), nil
			}
		}
	}
}

// listenEnded is the empty complete result that gracefully ends a listen
// stream; its _meta carries the subscription ID.
func listenEnded(req *Request) *protocol.ListenResult {
	res := &protocol.ListenResult{}
	res.Meta.SubscriptionID = req.id
	return res
}

// trackStream registers an active listen stream and returns its release
// function.
func (s *Server) trackStream() func() {
	done := make(chan struct{})
	s.streamMu.Lock()
	s.streams[done] = struct{}{}
	s.streamMu.Unlock()
	return func() {
		s.streamMu.Lock()
		delete(s.streams, done)
		s.streamMu.Unlock()
		close(done)
	}
}

// Shutdown ends every subscriptions/listen stream the way the spec prescribes
// for server-initiated teardown (notifications/cancelled, then a complete
// result) and waits until each has been handed to its transport, or ctx ends.
// Streams opened afterwards end immediately. Other requests are untouched:
// transports drain those themselves.
//
// Call it before the transport stops: http.Server.Shutdown waits for handlers
// that listen streams would never finish, and stdio.Serve stops writing once
// its context is cancelled.
func (s *Server) Shutdown(ctx context.Context) error {
	s.closeOnce.Do(func() { close(s.closing) })
	for {
		var pending chan struct{}
		s.streamMu.Lock()
		for done := range s.streams {
			pending = done
			break
		}
		s.streamMu.Unlock()
		if pending == nil {
			return nil
		}
		select {
		case <-pending:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// emitCancelled announces server-initiated teardown. The spec requires this
// notification when the server ends a subscription and forbids it otherwise,
// so it never fires when the client or transport closed the stream instead.
func emitCancelled(req *Request, reason string) {
	msg, err := protocol.NewNotification(protocol.NotificationCancelled, protocol.CancelledParams{
		Meta:      protocol.NotificationMeta{SubscriptionID: req.id},
		RequestID: req.id,
		Reason:    reason,
	})
	if err == nil {
		_ = req.emit(msg)
	}
}

// honorFilter intersects the requested filter with what this server supports
// and returns the honored subset plus the hub topics to subscribe. Per the
// spec, the ack includes only notification types the server actually supports
// (e.g. promptsListChanged is omitted when no prompts are registered) — it
// reads the same registrations capabilities derive from (OnDiscover edits
// presentation only). An extension may reject the whole request via its
// Topics hook (e.g. a missing client capability).
func (s *Server) honorFilter(req *Request, f *protocol.SubscriptionFilter) (protocol.SubscriptionFilter, []string, error) {
	s.mu.RLock()
	hasTools := s.toolsDeclared
	hasPrompts := s.promptsDeclared
	hasResources := s.resourcesDeclared
	var exts []Extension
	if len(f.Extra) > 0 {
		for _, e := range s.extensions {
			if e.Topics != nil {
				exts = append(exts, e)
			}
		}
	}
	s.mu.RUnlock()

	var honored protocol.SubscriptionFilter
	var topics []string
	if f.ToolsListChanged && hasTools {
		honored.ToolsListChanged = true
		topics = append(topics, topicTools)
	}
	if f.PromptsListChanged && hasPrompts {
		honored.PromptsListChanged = true
		topics = append(topics, topicPrompts)
	}
	if f.ResourcesListChanged && hasResources {
		honored.ResourcesListChanged = true
		topics = append(topics, topicResources)
	}
	if len(f.ResourceSubscriptions) > 0 && hasResources {
		honored.ResourceSubscriptions = f.ResourceSubscriptions
		for _, uri := range f.ResourceSubscriptions {
			topics = append(topics, topicResource(uri))
		}
	}
	for key, value := range f.Extra {
		for _, e := range exts {
			ts, ok, err := e.Topics(req, key, value)
			if err != nil {
				return protocol.SubscriptionFilter{}, nil, err
			}
			if !ok {
				continue
			}
			if len(ts) > 0 {
				if honored.Extra == nil {
					honored.Extra = make(map[string]json.RawMessage)
				}
				honored.Extra[key] = value
				topics = append(topics, ts...)
			}
			break
		}
	}
	return honored, topics, nil
}

// notificationWithSubscriptionID stamps the subscription ID into the event's
// params _meta and builds the notification message.
func notificationWithSubscriptionID(ev hubEvent, subID protocol.RequestID) (*protocol.Message, error) {
	params := maps.Clone(ev.params)
	if params == nil {
		params = make(map[string]any)
	}
	meta, _ := params["_meta"].(map[string]any)
	if meta == nil {
		meta = make(map[string]any)
	} else {
		meta = maps.Clone(meta)
	}
	idRaw, err := json.Marshal(subID)
	if err != nil {
		return nil, err
	}
	meta[protocol.MetaSubscriptionID] = json.RawMessage(idRaw)
	params["_meta"] = meta
	return protocol.NewNotification(ev.method, params)
}

package streamhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// Header names shared with the client package (duplicated to avoid a
// dependency; validated identical by tests).
const (
	headerProtocolVersion = "Mcp-Protocol-Version"
	headerMethod          = "Mcp-Method"
	headerName            = "Mcp-Name"
)

type TransportOptions struct {
	HTTPClient *http.Client
	// MaxRetries bounds retries of the initial POST on network errors and
	// transient statuses (502/503/504). Default 3. Only read-only methods are
	// ever retried (see retryable): when the outcome of an attempt is unknown,
	// re-sending a side-effecting call such as tools/call could execute it
	// twice, so those errors surface to the caller instead. There is no stream
	// resumption in this protocol: a broken response stream loses the request,
	// and the caller re-issues it with a new ID.
	MaxRetries int
	// MaxResponseBytes caps a JSON response body (default 32 MiB).
	MaxResponseBytes int64
}

// Transport is the client end of Streamable HTTP: POST per request, response
// as JSON or SSE, stream close as cancellation.
type Transport struct {
	endpoint string
	hc       *http.Client
	opts     TransportOptions
}

func New(endpoint string, opts *TransportOptions) *Transport {
	t := &Transport{endpoint: endpoint, hc: http.DefaultClient}
	if opts != nil {
		t.opts = *opts
	}
	if t.opts.HTTPClient != nil {
		t.hc = t.opts.HTTPClient
	}
	if t.opts.MaxRetries < 0 {
		t.opts.MaxRetries = 0
	} else if t.opts.MaxRetries == 0 {
		t.opts.MaxRetries = 3
	}
	if t.opts.MaxResponseBytes <= 0 {
		t.opts.MaxResponseBytes = 32 << 20
	}
	return t
}

func (t *Transport) Do(ctx context.Context, req *transport.Request) (transport.Stream, error) {
	body, err := json.Marshal(req.Message)
	if err != nil {
		return nil, err
	}
	isNotification := req.Message.Kind() == protocol.KindNotification

	retries := 0
	if retryable(req.Message.Method) {
		retries = t.opts.MaxRetries
	}
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt); err != nil {
				return nil, err
			}
		}

		// Cancelling the stream must abort the HTTP request; derive a
		// cancellable context owned by the stream.
		reqCtx, cancel := context.WithCancel(ctx)
		hreq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, t.endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			return nil, err
		}
		hreq.Header.Set("Content-Type", "application/json")
		hreq.Header.Set("Accept", "application/json, text/event-stream")
		for k, v := range req.Headers {
			hreq.Header.Set(k, v)
		}

		resp, err := t.hc.Do(hreq)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}

		if isTransientStatus(resp.StatusCode) {
			drain(resp)
			cancel()
			lastErr = fmt.Errorf("streamhttp: transient http %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode == http.StatusAccepted {
			drain(resp)
			cancel()
			if !isNotification {
				return nil, fmt.Errorf("streamhttp: unexpected 202 for a request")
			}
			return nil, nil
		}

		ct := resp.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "text/event-stream") {
			return newSSEStream(resp, cancel), nil
		}

		data, err := io.ReadAll(io.LimitReader(resp.Body, t.opts.MaxResponseBytes))
		drain(resp)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		var m protocol.Message
		if json.Unmarshal(data, &m) == nil && m.Kind() != protocol.KindInvalid {
			return &singleStream{msg: &m}, nil
		}
		return nil, fmt.Errorf("streamhttp: http %d: %s", resp.StatusCode, truncate(data, 256))
	}
	return nil, lastErr
}

func (t *Transport) Close() error {
	t.hc.CloseIdleConnections()
	return nil
}

// retryable reports whether a method may be re-sent when the outcome of a
// previous attempt is unknown (network error, transient status, lost
// response body). Only side-effect-free core methods qualify; everything
// else — tools/call above all — fails fast and leaves the retry decision to
// the caller, who knows the tool's semantics.
func retryable(method string) bool {
	switch method {
	case protocol.MethodDiscover,
		protocol.MethodToolsList,
		protocol.MethodPromptsList,
		protocol.MethodPromptsGet,
		protocol.MethodResourcesList,
		protocol.MethodResourcesTemplatesList,
		protocol.MethodResourcesRead,
		protocol.MethodCompletionComplete,
		protocol.MethodSubscriptionsListen:
		return true
	}
	return false
}

func isTransientStatus(code int) bool {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func sleepBackoff(ctx context.Context, attempt int) error {
	d := min(time.Duration(500*(1<<attempt))*time.Millisecond, 5*time.Second)
	d += time.Duration(rand.Int64N(int64(d / 4))) // jitter
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}

// singleStream yields one message, then EOF.
type singleStream struct {
	msg *protocol.Message
}

func (s *singleStream) Recv() (*protocol.Message, error) {
	if s.msg == nil {
		return nil, io.EOF
	}
	m := s.msg
	s.msg = nil
	return m, nil
}

func (s *singleStream) Close() error { return nil }

// sseStream parses the SSE response body. Closing it closes the HTTP stream,
// which is the wire-level cancellation signal.
type sseStream struct {
	resp   *http.Response
	cancel context.CancelFunc
	ch     chan sseItem
}

type sseItem struct {
	msg *protocol.Message
	err error
}

func newSSEStream(resp *http.Response, cancel context.CancelFunc) *sseStream {
	s := &sseStream{resp: resp, cancel: cancel, ch: make(chan sseItem, 16)}
	go func() {
		defer close(s.ch)
		// Release the connection when the server ends the stream naturally;
		// without this only an explicit Close would free it.
		defer resp.Body.Close()
		scanSSE(resp.Body, func(evt event, err error) bool {
			if err != nil {
				s.ch <- sseItem{err: err}
				return false
			}
			var m protocol.Message
			if uerr := json.Unmarshal(evt.Data, &m); uerr != nil {
				s.ch <- sseItem{err: fmt.Errorf("streamhttp: invalid SSE payload: %w", uerr)}
				return false
			}
			s.ch <- sseItem{msg: &m}
			return true
		})
	}()
	return s
}

func (s *sseStream) Recv() (*protocol.Message, error) {
	item, ok := <-s.ch
	if !ok {
		return nil, io.EOF
	}
	return item.msg, item.err
}

func (s *sseStream) Close() error {
	s.cancel()
	_ = s.resp.Body.Close()
	// Drain so the goroutine exits.
	for range s.ch {
	}
	return nil
}

var _ transport.Transport = (*Transport)(nil)

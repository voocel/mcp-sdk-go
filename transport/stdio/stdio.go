// Package stdio implements the MCP stdio transport: newline-delimited JSON,
// nothing non-MCP on stdout, notifications/cancelled for cancellation, prompt
// exit on EOF. Serve is the server end; Command is the client end.
package stdio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// Handler is the structural seam of the server side (satisfied by
// *server.Server).
type Handler interface {
	Handle(ctx context.Context, msg *protocol.Message, emit func(*protocol.Message) error)
}

const (
	defaultMaxMessageBytes = 16 << 20
	defaultMaxConcurrency  = 64
)

type Options struct {
	// Reader and Writer default to os.Stdin and os.Stdout. Injecting them
	// makes the transport testable and reusable over sockets/pipes.
	Reader io.Reader
	Writer io.Writer
	// MaxMessageBytes caps one line (default 16 MiB).
	MaxMessageBytes int64
	// MaxConcurrency caps concurrently dispatched requests (default 64).
	MaxConcurrency int
}

// Serve reads messages until EOF or ctx cancellation, dispatching each
// request in its own goroutine so a slow tool never blocks cancellation or
// other requests. It returns nil on clean EOF.
func Serve(ctx context.Context, h Handler, opts *Options) error {
	var o Options
	if opts != nil {
		o = *opts
	}
	if o.Reader == nil {
		o.Reader = os.Stdin
	}
	if o.Writer == nil {
		o.Writer = os.Stdout
	}
	if o.MaxMessageBytes <= 0 {
		o.MaxMessageBytes = defaultMaxMessageBytes
	}
	if o.MaxConcurrency <= 0 {
		o.MaxConcurrency = defaultMaxConcurrency
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var writeMu sync.Mutex
	write := func(m *protocol.Message) error {
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err = o.Writer.Write(append(raw, '\n'))
		return err
	}

	var (
		inflightMu sync.Mutex
		inflight   = make(map[protocol.RequestID]context.CancelFunc)
	)
	sem := make(chan struct{}, o.MaxConcurrency)
	var wg sync.WaitGroup

	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		br := bufio.NewReaderSize(o.Reader, 64<<10)
		for {
			line, err := readLine(br, o.MaxMessageBytes)
			if len(line) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case err := <-readErr:
			cancel() // exit promptly on EOF: abort in-flight work
			wg.Wait()
			if err == io.EOF {
				return nil
			}
			return err
		case line := <-lines:
			var msg protocol.Message
			if err := json.Unmarshal(line, &msg); err != nil {
				_ = write(protocol.NewErrorResponse(protocol.RequestID{},
					protocol.Errorf(protocol.CodeParseError, "parse error: %v", err)))
				continue
			}
			switch msg.Kind() {
			case protocol.KindNotification:
				if msg.Method == protocol.NotificationCancelled {
					var p protocol.CancelledParams
					if json.Unmarshal(msg.Params, &p) == nil {
						inflightMu.Lock()
						if c := inflight[p.RequestID]; c != nil {
							c()
						}
						inflightMu.Unlock()
					}
				}
			case protocol.KindRequest:
				rctx, rcancel := context.WithCancel(ctx)
				inflightMu.Lock()
				inflight[msg.ID] = rcancel
				inflightMu.Unlock()
				wg.Add(1)
				m := msg
				go func() {
					defer wg.Done()
					defer func() {
						inflightMu.Lock()
						delete(inflight, m.ID)
						inflightMu.Unlock()
						rcancel()
					}()
					// The concurrency slot is acquired here, not in the
					// dispatch loop: a saturated server must keep reading so
					// notifications/cancelled can still reach queued and
					// running requests.
					select {
					case sem <- struct{}{}:
					case <-rctx.Done():
						return
					}
					defer func() { <-sem }()
					h.Handle(rctx, &m, func(out *protocol.Message) error {
						// After cancellation the server must not send further
						// messages for this request.
						if err := rctx.Err(); err != nil {
							return err
						}
						return write(out)
					})
				}()
			default:
				// Clients must not send responses; ignore malformed traffic.
			}
		}
	}
}

// readLine reads one newline-delimited message with a size cap, tolerating a
// trailing \r.
func readLine(br *bufio.Reader, max int64) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if int64(len(buf)) > max {
			return nil, fmt.Errorf("stdio: message exceeds %d bytes", max)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return bytes.TrimRight(buf, "\r\n"), err
	}
}

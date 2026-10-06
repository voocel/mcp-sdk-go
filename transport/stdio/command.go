package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

const defaultTerminateDuration = 5 * time.Second

// errSubscriptionOverflow ends a subscription stream whose consumer fell behind.
var errSubscriptionOverflow = errors.New("stdio: subscription overflowed: notifications were dropped; re-listen and re-sync")

type CommandOptions struct {
	// TerminateDuration is how long each shutdown stage (stdin close,
	// interrupt) waits before escalating, ending with a hard kill.
	TerminateDuration time.Duration
	MaxMessageBytes   int64
	// Stderr receives the subprocess stderr (default os.Stderr).
	Stderr io.Writer
}

// Command runs an MCP server as a subprocess and implements
// transport.Transport over its stdio. Responses are correlated by request ID
// (string and integer alike), subscription notifications by
// _meta.subscriptionId, and progress notifications by progressToken.
type Command struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	opts  CommandOptions

	writeMu sync.Mutex

	mu      sync.Mutex
	byID    map[protocol.RequestID]*cmdStream
	byToken map[protocol.RequestID]*cmdStream
	closed  bool

	readDone chan struct{}
}

func NewCommand(cmd *exec.Cmd, opts *CommandOptions) (*Command, error) {
	c := &Command{
		cmd:      cmd,
		byID:     make(map[protocol.RequestID]*cmdStream),
		byToken:  make(map[protocol.RequestID]*cmdStream),
		readDone: make(chan struct{}),
	}
	if opts != nil {
		c.opts = *opts
	}
	if c.opts.TerminateDuration <= 0 {
		c.opts.TerminateDuration = defaultTerminateDuration
	}
	if c.opts.MaxMessageBytes <= 0 {
		c.opts.MaxMessageBytes = defaultMaxMessageBytes
	}
	if c.opts.Stderr == nil {
		c.opts.Stderr = os.Stderr
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = c.opts.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c.stdin = stdin
	go c.readLoop(stdout)
	return c, nil
}

func (c *Command) Do(ctx context.Context, req *transport.Request) (transport.Stream, error) {
	msg := req.Message
	if msg.Kind() == protocol.KindNotification {
		return nil, c.write(msg)
	}

	st := &cmdStream{
		c:    c,
		id:   msg.ID,
		ch:   make(chan *protocol.Message, 32),
		done: make(chan struct{}),
	}
	st.token = progressTokenOf(msg.Params)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("stdio: transport closed")
	}
	c.byID[msg.ID] = st
	if !st.token.IsZero() {
		c.byToken[st.token] = st
	}
	c.mu.Unlock()

	if err := c.write(msg); err != nil {
		c.unregister(st, false)
		return nil, err
	}

	// Translate ctx cancellation into the stdio cancellation protocol.
	go func() {
		select {
		case <-ctx.Done():
			_ = st.Close()
		case <-st.done:
		}
	}()
	return st, nil
}

func (c *Command) write(m *protocol.Message) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(raw, '\n'))
	return err
}

func (c *Command) readLoop(stdout io.Reader) {
	defer close(c.readDone)
	br := bufio.NewReaderSize(stdout, 64<<10)
	for {
		line, err := readLine(br, c.opts.MaxMessageBytes)
		if len(line) > 0 {
			c.route(line)
		}
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			c.failAll(err)
			return
		}
	}
}

func (c *Command) route(line []byte) {
	var msg protocol.Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return // nothing non-MCP should appear on stdout; drop garbage
	}
	switch msg.Kind() {
	case protocol.KindResponse, protocol.KindError:
		c.mu.Lock()
		st := c.byID[msg.ID]
		if st != nil {
			st.deliver(&msg, true)
			c.dropLocked(st)
		}
		c.mu.Unlock()
	case protocol.KindNotification:
		var overflowed *cmdStream
		c.mu.Lock()
		if st := c.byID[subscriptionIDOf(msg.Params)]; st != nil {
			// A subscription notification is a change the caller must not miss
			// (the server's hub ends an overflowing stream for the same reason):
			// a consumer that cannot keep up gets an error, and re-listens.
			if !st.deliver(&msg, false) {
				st.err = errSubscriptionOverflow
				c.dropLocked(st)
				overflowed = st
			}
		} else if st := c.byToken[progressTokenOfNotification(msg.Params)]; st != nil {
			st.deliver(&msg, false) // progress is superseded by the next report
		}
		c.mu.Unlock()
		if overflowed != nil {
			go c.sendCancel(overflowed.id) // stop the server-side stream too
		}
	}
}

func (c *Command) unregister(st *cmdStream, sendCancel bool) {
	c.mu.Lock()
	_, active := c.byID[st.id]
	c.dropLocked(st)
	c.mu.Unlock()
	if active && sendCancel {
		c.sendCancel(st.id)
	}
}

// sendCancel asks the server to stop the request with the given ID.
func (c *Command) sendCancel(id protocol.RequestID) {
	if note, err := protocol.NewNotification(protocol.NotificationCancelled,
		protocol.CancelledParams{RequestID: id}); err == nil {
		_ = c.write(note)
	}
}

// dropLocked removes the stream from the routing tables and closes its
// channel. Callers hold c.mu, which also serializes against deliver.
func (c *Command) dropLocked(st *cmdStream) {
	if _, ok := c.byID[st.id]; !ok {
		return
	}
	delete(c.byID, st.id)
	if !st.token.IsZero() {
		delete(c.byToken, st.token)
	}
	close(st.ch)
	close(st.done)
}

func (c *Command) failAll(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, st := range c.byID {
		st.err = err
		c.dropLocked(st)
	}
}

// Close shuts the subprocess down: close stdin, wait, interrupt, wait, kill.
func (c *Command) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		<-c.readDone
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	_ = c.stdin.Close()
	select {
	case <-c.readDone:
	case <-time.After(c.opts.TerminateDuration):
		_ = c.cmd.Process.Signal(os.Interrupt) // no-op on Windows; kill follows
		select {
		case <-c.readDone:
		case <-time.After(c.opts.TerminateDuration):
			_ = c.cmd.Process.Kill()
			<-c.readDone
		}
	}
	err := c.cmd.Wait()
	c.failAll(errors.New("stdio: transport closed"))
	return err
}

type cmdStream struct {
	c     *Command
	id    protocol.RequestID
	token protocol.RequestID
	ch    chan *protocol.Message
	done  chan struct{}
	err   error

	closeOnce sync.Once
}

// deliver is called only from the read loop, with c.mu held, so it must never
// block. One buffer slot is reserved for the final message; a notification
// that does not fit in the rest is not delivered (false) and the caller
// decides whether that matters. The single-producer len check is race-free:
// the consumer only ever shrinks the buffer.
func (st *cmdStream) deliver(m *protocol.Message, final bool) bool {
	if final {
		st.ch <- m // the reserved slot guarantees room
		return true
	}
	if len(st.ch) < cap(st.ch)-1 {
		st.ch <- m
		return true
	}
	return false
}

func (st *cmdStream) Recv() (*protocol.Message, error) {
	m, ok := <-st.ch
	if !ok {
		if st.err != nil {
			return nil, st.err
		}
		return nil, io.EOF
	}
	return m, nil
}

// Close cancels the request per the stdio protocol: a notifications/cancelled
// referencing its ID (unless it already finished).
func (st *cmdStream) Close() error {
	st.closeOnce.Do(func() { st.c.unregister(st, true) })
	return nil
}

// progressTokenOf extracts _meta.progressToken from request params.
func progressTokenOf(params json.RawMessage) protocol.RequestID {
	var probe struct {
		Meta struct {
			Token protocol.RequestID `json:"progressToken"`
		} `json:"_meta"`
	}
	_ = json.Unmarshal(params, &probe)
	return probe.Meta.Token
}

// progressTokenOfNotification extracts the top-level progressToken of a
// notifications/progress payload.
func progressTokenOfNotification(params json.RawMessage) protocol.RequestID {
	var probe struct {
		Token protocol.RequestID `json:"progressToken"`
	}
	_ = json.Unmarshal(params, &probe)
	return probe.Token
}

// subscriptionIDOf extracts _meta.subscriptionId from notification params.
func subscriptionIDOf(params json.RawMessage) protocol.RequestID {
	var probe struct {
		Meta protocol.NotificationMeta `json:"_meta"`
	}
	_ = json.Unmarshal(params, &probe)
	return probe.Meta.SubscriptionID
}

var _ transport.Transport = (*Command)(nil)

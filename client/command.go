package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
)

// CommandTransport is a Transport that runs a command and communicates with it via stdin/stdout
// using newline-delimited JSON
type CommandTransport struct {
	Command *exec.Cmd
	// TerminateDuration controls how long to wait for the process to exit after closing stdin before sending SIGTERM
	// If zero or negative, defaults to 5 seconds
	TerminateDuration time.Duration
}

// NewCommandTransport creates a new CommandTransport
func NewCommandTransport(command string, args ...string) *CommandTransport {
	return &CommandTransport{
		Command:           exec.Command(command, args...),
		TerminateDuration: 5 * time.Second,
	}
}

// Connect starts the command and connects to it via stdin/stdout
func (t *CommandTransport) Connect(ctx context.Context) (transport.Connection, error) {
	stdout, err := t.Command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stdin, err := t.Command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	if err := t.Command.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	td := t.TerminateDuration
	if td <= 0 {
		td = 5 * time.Second
	}

	return &commandConn{
		cmd:               t.Command,
		stdout:            stdout,
		stdin:             stdin,
		scanner:           bufio.NewScanner(stdout),
		terminateDuration: td,
	}, nil
}

// commandConn implements the transport.Connection interface
type commandConn struct {
	cmd               *exec.Cmd
	stdout            io.ReadCloser
	stdin             io.WriteCloser
	scanner           *bufio.Scanner
	mu                sync.Mutex
	closed            atomic.Bool
	terminateDuration time.Duration
}

func (c *commandConn) Read(ctx context.Context) (*protocol.JSONRPCMessage, error) {
	if c.closed.Load() {
		return nil, transport.ErrConnectionClosed
	}

	// Use channels to support context cancellation
	msgChan := make(chan *protocol.JSONRPCMessage, 1)
	errChan := make(chan error, 1)

	go func() {
		if !c.scanner.Scan() {
			if err := c.scanner.Err(); err != nil {
				errChan <- fmt.Errorf("scanner error: %w", err)
			} else {
				errChan <- io.EOF
			}
			return
		}

		data := c.scanner.Bytes()
		if len(data) == 0 {
			errChan <- fmt.Errorf("empty message")
			return
		}

		var msg protocol.JSONRPCMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			errChan <- fmt.Errorf("invalid JSON-RPC message: %w", err)
			return
		}

		msgChan <- &msg
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errChan:
		return nil, err
	case msg := <-msgChan:
		return msg, nil
	}
}

func (c *commandConn) Write(ctx context.Context, msg *protocol.JSONRPCMessage) error {
	if c.closed.Load() {
		return transport.ErrConnectionClosed
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	if _, err := c.stdin.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// Close closes the input stream to the subprocess and waits for the command to terminate gracefully
// If the command doesn't exit, it sends signals to terminate it, ultimately killing it
//
// Per MCP specification:
// "For stdio transport, clients should initiate shutdown by:
//  1. First, closing the input stream to the subprocess (server)
//  2. Waiting for the server to exit, or sending SIGTERM if it doesn't exit within a reasonable time
//  3. Sending SIGKILL if the server doesn't exit within a reasonable time after SIGTERM"
func (c *commandConn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Close stdin
	if err := c.stdin.Close(); err != nil {
		return fmt.Errorf("closing stdin: %w", err)
	}

	resChan := make(chan error, 1)
	go func() {
		resChan <- c.cmd.Wait()
	}()

	wait := func() (error, bool) {
		select {
		case err := <-resChan:
			return err, true
		case <-time.After(c.terminateDuration):
			return nil, false
		}
	}

	if err, ok := wait(); ok {
		return err
	}

	// Send SIGTERM
	// Note: if sending SIGTERM fails, proceed directly to SIGKILL without waiting
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err == nil {
		if err, ok := wait(); ok {
			return err
		}
	}

	// Send SIGKILL
	if err := c.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	if err, ok := wait(); ok {
		return err
	}

	return fmt.Errorf("unresponsive subprocess")
}

func (c *commandConn) SessionID() string {
	// Command connections don't have session IDs
	return ""
}

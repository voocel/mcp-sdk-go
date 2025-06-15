package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/voocel/mcp-sdk-go/transport"
)

type Transport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
}

func NewWithCommand(command string, args []string) (*Transport, error) {
	cmd := exec.Command(command, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	return &Transport{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		scanner: bufio.NewScanner(stdout),
	}, nil
}

func NewWithStdio() *Transport {
	return &Transport{
		cmd:     nil,
		stdin:   os.Stdin,
		stdout:  os.Stdout,
		scanner: bufio.NewScanner(os.Stdin),
	}
}

func (t *Transport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, err := fmt.Fprintf(t.stdin, "%s\n", data)
	return err
}

func (t *Transport) Receive(ctx context.Context) ([]byte, error) {
	done := make(chan struct{})
	var data []byte
	var scanErr error

	go func() {
		defer close(done)
		if t.scanner.Scan() {
			data = []byte(t.scanner.Text())
		} else {
			scanErr = t.scanner.Err()
			if scanErr == nil {
				scanErr = io.EOF
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		return data, scanErr
	}
}

func (t *Transport) Close() error {
	var err error

	if t.cmd != nil {
		if t.stdin != nil {
			t.stdin.Close()
		}
		err = t.cmd.Wait()
	}

	return err
}

type Server struct {
	handler transport.Handler
}

func NewServer(handler transport.Handler) *Server {
	return &Server{
		handler: handler,
	}
}

func (s *Server) Serve(ctx context.Context) error {
	t := NewWithStdio()
	defer t.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			data, err := t.Receive(ctx)
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}

			response, err := s.handler.HandleMessage(ctx, data)
			if err != nil {
				if response != nil {
					errorResp := struct {
						Error string `json:"error"`
					}{
						Error: err.Error(),
					}
					response, _ = json.Marshal(errorResp)
				}
			}
			
			if response != nil {
				if err := t.Send(ctx, response); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

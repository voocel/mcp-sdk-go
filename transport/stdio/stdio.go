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
	stderr  io.ReadCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
	closed  bool
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

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	return &Transport{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		scanner: bufio.NewScanner(stdout),
	}, nil
}

func NewWithStdio() *Transport {
	return &Transport{
		cmd:     nil,
		stdin:   os.Stdout, // 服务器写入到 stdout
		stdout:  os.Stdin,  // 服务器从 stdin 读取
		scanner: bufio.NewScanner(os.Stdin),
	}
}

func (t *Transport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("transport is closed")
	}

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
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	var errs []error

	if t.stdin != nil {
		if err := t.stdin.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close stdin: %w", err))
		}
	}
	if t.stdout != nil {
		if err := t.stdout.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close stdout: %w", err))
		}
	}
	if t.stderr != nil {
		if err := t.stderr.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close stderr: %w", err))
		}
	}
	if t.cmd != nil {
		if err := t.cmd.Wait(); err != nil {
			errs = append(errs, fmt.Errorf("command wait failed: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

func (t *Transport) Stderr() io.ReadCloser {
	return t.stderr
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
				// 如果处理出错但没有响应，尝试解析原始消息获取 ID
				if response == nil {
					var msg map[string]interface{}
					if json.Unmarshal(data, &msg) == nil {
						if id, ok := msg["id"]; ok {
							// 创建标准 JSON-RPC 错误响应
							errorResp := map[string]interface{}{
								"jsonrpc": "2.0",
								"id":      id,
								"error": map[string]interface{}{
									"code":    -32603, // Internal error
									"message": err.Error(),
								},
							}
							response, _ = json.Marshal(errorResp)
						}
					}
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

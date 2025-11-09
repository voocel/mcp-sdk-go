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

// CommandTransport 是一个运行命令并通过 stdin/stdout 与之通信的 Transport
// 使用换行符分隔的 JSON
type CommandTransport struct {
	Command *exec.Cmd
	// TerminateDuration 控制在关闭 stdin 后等待进程退出多长时间,然后发送 SIGTERM
	// 如果为零或负数,则使用默认值 5 秒
	TerminateDuration time.Duration
}

// NewCommandTransport 创建一个新的 CommandTransport
func NewCommandTransport(command string, args ...string) *CommandTransport {
	return &CommandTransport{
		Command:           exec.Command(command, args...),
		TerminateDuration: 5 * time.Second,
	}
}

// Connect 启动命令并通过 stdin/stdout 连接到它
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

// commandConn 实现 transport.Connection 接口
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

	// 使用 channel 来支持 context 取消
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

// Close 关闭到子进程的输入流,并等待命令正常终止
// 如果命令没有退出,则发送信号终止它,最终杀死它
//
// 参考 MCP 规范:
// "对于 stdio transport,客户端应该通过以下方式启动关闭:
//  1. 首先,关闭到子进程(服务器)的输入流
//  2. 等待服务器退出,或者如果服务器在合理时间内没有退出则发送 SIGTERM
//  3. 如果服务器在 SIGTERM 后的合理时间内没有退出则发送 SIGKILL"
func (c *commandConn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	// 关闭 stdin
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

	// 等待服务器退出
	if err, ok := wait(); ok {
		return err
	}

	// 发送 SIGTERM
	// 注意:如果发送 SIGTERM 失败,不等待直接进入 SIGKILL
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err == nil {
		if err, ok := wait(); ok {
			return err
		}
	}

	// 发送 SIGKILL
	if err := c.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	if err, ok := wait(); ok {
		return err
	}

	return fmt.Errorf("unresponsive subprocess")
}

func (c *commandConn) SessionID() string {
	// Command 连接没有会话 ID
	return ""
}

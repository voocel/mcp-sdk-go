package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport"
	"github.com/voocel/mcp-sdk-go/transport/sse"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

type Client interface {
	Initialize(ctx context.Context) error
	ListTools(ctx context.Context) ([]protocol.Tool, error)
	CallTool(ctx context.Context, name string, args map[string]interface{}) (*protocol.CallToolResult, error)
	ListResources(ctx context.Context) ([]protocol.Resource, error)
	ReadResource(ctx context.Context, uri string) (*protocol.ResourceContent, error)
	ListPrompts(ctx context.Context) ([]protocol.Prompt, error)
	GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error)
	Close() error
}

type MCPClient struct {
	transport       transport.Transport
	serverInfo      *protocol.ServerInfo
	pendingRequests map[string]chan *protocol.Response
	mu              sync.RWMutex
}

type Option func(*MCPClient) error

func WithTransport(t transport.Transport) Option {
	return func(c *MCPClient) error {
		c.transport = t
		return nil
	}
}

func WithStdioTransport(command string, args []string) Option {
	return func(c *MCPClient) error {
		t, err := stdio.NewWithCommand(command, args)
		if err != nil {
			return err
		}
		c.transport = t
		return nil
	}
}

func WithSSETransport(url string) Option {
	return func(c *MCPClient) error {
		t := sse.New(url)
		if err := t.Connect(context.Background()); err != nil {
			return err
		}
		c.transport = t
		return nil
	}
}

func New(options ...Option) (Client, error) {
	client := &MCPClient{
		pendingRequests: make(map[string]chan *protocol.Response),
	}

	for _, option := range options {
		if err := option(client); err != nil {
			return nil, err
		}
	}

	if client.transport == nil {
		return nil, fmt.Errorf("transport is required")
	}

	go client.receiveLoop(context.Background())

	return client, nil
}

func (c *MCPClient) receiveLoop(ctx context.Context) {
	for {
		data, err := c.transport.Receive(ctx)
		if err != nil {
			return
		}

		var response protocol.Response
		if err := json.Unmarshal(data, &response); err != nil {
			continue
		}

		if response.ID != "" {
			c.mu.RLock()
			ch, ok := c.pendingRequests[response.ID]
			c.mu.RUnlock()

			if ok {
				ch <- &response
			}
		}
	}
}

func (c *MCPClient) sendRequest(ctx context.Context, method string, params interface{}) (*protocol.Response, error) {
	id := uuid.New().String()

	var paramsJSON json.RawMessage
	if params != nil {
		bytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal parameters: %w", err)
		}
		paramsJSON = bytes
	}

	message := protocol.Message{
		ID:        id,
		Method:    method,
		Params:    paramsJSON,
		Timestamp: time.Now(),
	}

	respChan := make(chan *protocol.Response, 1)

	c.mu.Lock()
	c.pendingRequests[id] = respChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pendingRequests, id)
		c.mu.Unlock()
	}()

	msgBytes, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := c.transport.Send(ctx, msgBytes); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respChan:
		if resp.Error != nil {
			return nil, fmt.Errorf("server error: %s", resp.Error.Message)
		}
		return resp, nil
	}
}

func (c *MCPClient) Initialize(ctx context.Context) error {
	resp, err := c.sendRequest(ctx, "initialize", nil)
	if err != nil {
		return err
	}

	var serverInfo protocol.ServerInfo
	if err := json.Unmarshal(resp.Result, &serverInfo); err != nil {
		return fmt.Errorf("failed to unmarshal server info: %w", err)
	}

	c.serverInfo = &serverInfo
	return nil
}

func (c *MCPClient) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	resp, err := c.sendRequest(ctx, "listTools", nil)
	if err != nil {
		return nil, err
	}

	var toolList protocol.ToolList
	if err := json.Unmarshal(resp.Result, &toolList); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tool list: %w", err)
	}

	return toolList.Tools, nil
}

func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*protocol.CallToolResult, error) {
	params := protocol.CallToolParams{
		Name: name,
		Args: args,
	}

	resp, err := c.sendRequest(ctx, "callTool", params)
	if err != nil {
		return nil, err
	}

	var result protocol.CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tool result: %w", err)
	}

	return &result, nil
}

func (c *MCPClient) ListResources(ctx context.Context) ([]protocol.Resource, error) {
	resp, err := c.sendRequest(ctx, "listResources", nil)
	if err != nil {
		return nil, err
	}

	var resourceList protocol.ResourceList
	if err := json.Unmarshal(resp.Result, &resourceList); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource list: %w", err)
	}

	return resourceList.Resources, nil
}

func (c *MCPClient) ReadResource(ctx context.Context, uri string) (*protocol.ResourceContent, error) {
	params := protocol.ReadResourceParams{
		URI: uri,
	}

	resp, err := c.sendRequest(ctx, "readResource", params)
	if err != nil {
		return nil, err
	}

	var content protocol.ResourceContent
	if err := json.Unmarshal(resp.Result, &content); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource content: %w", err)
	}

	return &content, nil
}

func (c *MCPClient) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) {
	resp, err := c.sendRequest(ctx, "listPrompts", nil)
	if err != nil {
		return nil, err
	}

	var promptList protocol.PromptList
	if err := json.Unmarshal(resp.Result, &promptList); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompt list: %w", err)
	}

	return promptList.Prompts, nil
}

func (c *MCPClient) GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error) {
	params := protocol.GetPromptParams{
		Name: name,
		Args: args,
	}

	resp, err := c.sendRequest(ctx, "getPrompt", params)
	if err != nil {
		return nil, err
	}

	var result protocol.GetPromptResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompt result: %w", err)
	}

	return &result, nil
}

func (c *MCPClient) Close() error {
	return c.transport.Close()
}

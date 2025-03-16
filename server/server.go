package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport/grpc"
	"github.com/voocel/mcp-sdk-go/transport/sse"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
	"github.com/voocel/mcp-sdk-go/transport/websocket"
)

type ToolHandler func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error)

type ResourceHandler func(ctx context.Context) (*protocol.ResourceContent, error)

type PromptHandler func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error)

type Server interface {
	AddTool(name, description string, handler ToolHandler, parameters ...protocol.ToolParameter)

	AddResource(uri, name string, handler ResourceHandler)

	AddPrompt(name, description string, handler PromptHandler, arguments ...protocol.PromptArgument)

	Name() string

	Version() string
}

type MCPServer struct {
	name      string
	version   string
	tools     map[string]*ToolInfo
	resources map[string]*ResourceInfo
	prompts   map[string]*PromptInfo
	mu        sync.RWMutex
}

type ToolInfo struct {
	Name        string
	Description string
	Parameters  []protocol.ToolParameter
	Handler     ToolHandler
}

type ResourceInfo struct {
	URI     string
	Name    string
	Handler ResourceHandler
}

type PromptInfo struct {
	Name        string
	Description string
	Arguments   []protocol.PromptArgument
	Handler     PromptHandler
}

func New(name, version string) *FastMCP {
	return NewFastMCP(name, version)
}

func NewServer(name, version string) *MCPServer {
	return &MCPServer{
		name:      name,
		version:   version,
		tools:     make(map[string]*ToolInfo),
		resources: make(map[string]*ResourceInfo),
		prompts:   make(map[string]*PromptInfo),
	}
}

func (s *MCPServer) Name() string {
	return s.name
}

func (s *MCPServer) Version() string {
	return s.version
}

func (s *MCPServer) AddTool(name, description string, handler ToolHandler, parameters ...protocol.ToolParameter) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tools[name] = &ToolInfo{
		Name:        name,
		Description: description,
		Parameters:  parameters,
		Handler:     handler,
	}
}

func (s *MCPServer) AddResource(uri, name string, handler ResourceHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.resources[uri] = &ResourceInfo{
		URI:     uri,
		Name:    name,
		Handler: handler,
	}
}

func (s *MCPServer) AddPrompt(name, description string, handler PromptHandler, arguments ...protocol.PromptArgument) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.prompts[name] = &PromptInfo{
		Name:        name,
		Description: description,
		Arguments:   arguments,
		Handler:     handler,
	}
}

type Handler struct {
	server *MCPServer
}

func NewHandler(server *MCPServer) *Handler {
	return &Handler{
		server: server,
	}
}

func (h *Handler) HandleMessage(ctx context.Context, data []byte) ([]byte, error) {
	var message protocol.Message
	if err := json.Unmarshal(data, &message); err != nil {
		return nil, fmt.Errorf("invalid message format: %w", err)
	}

	// 记录收到的请求
	fmt.Fprintf(os.Stderr, "收到请求: %s\n", string(data))

	var result interface{}
	var err error

	switch message.Method {
	case "initialize":
		result = protocol.ServerInfo{
			Name:    h.server.name,
			Version: h.server.version,
			Capabilities: protocol.Capabilities{
				Tools:     len(h.server.tools) > 0,
				Resources: len(h.server.resources) > 0,
				Prompts:   len(h.server.prompts) > 0,
			},
		}
	case "listTools":
		result, err = h.handleListTools(ctx)
	case "callTool":
		result, err = h.handleCallTool(ctx, message.Params)
	case "listResources":
		result, err = h.handleListResources(ctx)
	case "readResource":
		result, err = h.handleReadResource(ctx, message.Params)
	case "listPrompts":
		result, err = h.handleListPrompts(ctx)
	case "getPrompt":
		result, err = h.handleGetPrompt(ctx, message.Params)
	default:
		err = fmt.Errorf("unknown method: %s", message.Method)
	}

	response := protocol.Response{
		ID:        message.ID,
		Timestamp: time.Now(),
	}

	if err != nil {
		response.Error = &protocol.Error{
			Code:    -1,
			Message: err.Error(),
		}
	} else {
		responseBytes, err := json.Marshal(result)
		if err != nil {
			response.Error = &protocol.Error{
				Code:    -2,
				Message: fmt.Sprintf("failed to marshal result: %v", err),
			}
		} else {
			response.Result = responseBytes
		}
	}

	respBytes, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	// 记录发送的响应
	fmt.Fprintf(os.Stderr, "发送响应: %s\n", string(respBytes))

	return respBytes, nil
}

func (h *Handler) handleListTools(ctx context.Context) (*protocol.ToolList, error) {
	h.server.mu.RLock()
	defer h.server.mu.RUnlock()

	tools := make([]protocol.Tool, 0, len(h.server.tools))
	for _, info := range h.server.tools {
		tools = append(tools, protocol.Tool{
			Name:        info.Name,
			Description: info.Description,
			Parameters:  info.Parameters,
		})
	}

	return &protocol.ToolList{Tools: tools}, nil
}

func (h *Handler) handleCallTool(ctx context.Context, paramsBytes json.RawMessage) (*protocol.CallToolResult, error) {
	var params protocol.CallToolParams
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return nil, fmt.Errorf("invalid tool call parameters: %w", err)
	}

	h.server.mu.RLock()
	toolInfo, ok := h.server.tools[params.Name]
	h.server.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("tool not found: %s", params.Name)
	}

	return toolInfo.Handler(ctx, params.Args)
}

func (h *Handler) handleListResources(ctx context.Context) (*protocol.ResourceList, error) {
	h.server.mu.RLock()
	defer h.server.mu.RUnlock()

	resources := make([]protocol.Resource, 0, len(h.server.resources))
	for _, info := range h.server.resources {
		resources = append(resources, protocol.Resource{
			URI:  info.URI,
			Name: info.Name,
		})
	}

	return &protocol.ResourceList{Resources: resources}, nil
}

func (h *Handler) handleReadResource(ctx context.Context, paramsBytes json.RawMessage) (*protocol.ResourceContent, error) {
	var params protocol.ReadResourceParams
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return nil, fmt.Errorf("invalid resource parameters: %w", err)
	}

	h.server.mu.RLock()
	resourceInfo, ok := h.server.resources[params.URI]
	h.server.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("resource not found: %s", params.URI)
	}

	return resourceInfo.Handler(ctx)
}

func (h *Handler) handleListPrompts(ctx context.Context) (*protocol.PromptList, error) {
	h.server.mu.RLock()
	defer h.server.mu.RUnlock()

	prompts := make([]protocol.Prompt, 0, len(h.server.prompts))
	for _, info := range h.server.prompts {
		prompts = append(prompts, protocol.Prompt{
			Name:        info.Name,
			Description: info.Description,
			Arguments:   info.Arguments,
		})
	}

	return &protocol.PromptList{Prompts: prompts}, nil
}

func (h *Handler) handleGetPrompt(ctx context.Context, paramsBytes json.RawMessage) (*protocol.GetPromptResult, error) {
	var params protocol.GetPromptParams
	if err := json.Unmarshal(paramsBytes, &params); err != nil {
		return nil, fmt.Errorf("invalid prompt parameters: %w", err)
	}

	h.server.mu.RLock()
	promptInfo, ok := h.server.prompts[params.Name]
	h.server.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("prompt not found: %s", params.Name)
	}

	return promptInfo.Handler(ctx, params.Args)
}

func ServeStdio(ctx context.Context, server *MCPServer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	handler := NewHandler(server)
	stdioServer := stdio.NewServer(handler)
	return stdioServer.Serve(ctx)
}

func ServeSSE(server *MCPServer, addr string) error {
	handler := NewHandler(server)
	sseServer := sse.NewServer(addr, handler)
	return sseServer.Serve(context.Background())
}

func ServeWebSocket(server *MCPServer, addr string) error {
	handler := NewHandler(server)
	wsServer := websocket.NewServer(addr, handler)
	return wsServer.Serve(context.Background())
}

func ServeGRPC(server *MCPServer, addr string) error {
	handler := NewHandler(server)
	grpcServer := grpc.NewServer(addr, handler)
	return grpcServer.Serve(context.Background())
}

package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/utils"
)

type FastMCP struct {
	server *MCPServer
}

type ToolBuilder struct {
	fastmcp     *FastMCP
	name        string
	description string
	inputSchema protocol.JSONSchema
}

type ResourceBuilder struct {
	fastmcp     *FastMCP
	uri         string
	name        string
	description string
	mimeType    string
}

type PromptBuilder struct {
	fastmcp     *FastMCP
	name        string
	description string
	arguments   []protocol.PromptArgument
}

func NewFastMCP(name, version string) *FastMCP {
	return &FastMCP{
		server: NewServer(name, version),
	}
}

func New(name, version string) *FastMCP {
	return NewFastMCP(name, version)
}

func (f *FastMCP) Server() *MCPServer {
	return f.server
}

func (f *FastMCP) Tool(name, description string) *ToolBuilder {
	return &ToolBuilder{
		fastmcp:     f,
		name:        name,
		description: description,
		inputSchema: protocol.JSONSchema{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (tb *ToolBuilder) WithStringParam(name, description string, required bool) *ToolBuilder {
	properties := tb.inputSchema["properties"].(map[string]interface{})
	properties[name] = map[string]interface{}{
		"type":        "string",
		"description": description,
	}

	if required {
		if reqList, ok := tb.inputSchema["required"]; ok {
			if reqArray, ok := reqList.([]string); ok {
				tb.inputSchema["required"] = append(reqArray, name)
			}
		} else {
			tb.inputSchema["required"] = []string{name}
		}
	}

	return tb
}

// WithIntParam 添加整数参数
func (tb *ToolBuilder) WithIntParam(name, description string, required bool) *ToolBuilder {
	properties := tb.inputSchema["properties"].(map[string]interface{})
	properties[name] = map[string]interface{}{
		"type":        "integer",
		"description": description,
	}

	if required {
		if reqList, ok := tb.inputSchema["required"]; ok {
			if reqArray, ok := reqList.([]string); ok {
				tb.inputSchema["required"] = append(reqArray, name)
			}
		} else {
			tb.inputSchema["required"] = []string{name}
		}
	}

	return tb
}

// WithBoolParam 添加布尔参数
func (tb *ToolBuilder) WithBoolParam(name, description string, required bool) *ToolBuilder {
	properties := tb.inputSchema["properties"].(map[string]interface{})
	properties[name] = map[string]interface{}{
		"type":        "boolean",
		"description": description,
	}

	if required {
		if reqList, ok := tb.inputSchema["required"]; ok {
			if reqArray, ok := reqList.([]string); ok {
				tb.inputSchema["required"] = append(reqArray, name)
			}
		} else {
			tb.inputSchema["required"] = []string{name}
		}
	}

	return tb
}

// WithNumberParam 添加数字参数（支持整数和浮点数）
func (tb *ToolBuilder) WithNumberParam(name, description string, required bool) *ToolBuilder {
	properties := tb.inputSchema["properties"].(map[string]interface{})
	properties[name] = map[string]interface{}{
		"type":        "number",
		"description": description,
	}

	if required {
		if reqList, ok := tb.inputSchema["required"]; ok {
			if reqArray, ok := reqList.([]string); ok {
				tb.inputSchema["required"] = append(reqArray, name)
			}
		} else {
			tb.inputSchema["required"] = []string{name}
		}
	}

	return tb
}

// WithInputSchema 设置自定义输入schema
func (tb *ToolBuilder) WithInputSchema(schema protocol.JSONSchema) *ToolBuilder {
	tb.inputSchema = schema
	return tb
}

// WithStructSchema 使用结构体自动生成schema
func (tb *ToolBuilder) WithStructSchema(v interface{}) *ToolBuilder {
	schema, err := utils.StructToJSONSchema(v)
	if err == nil {
		tb.inputSchema = schema
	}
	return tb
}

// Handle 注册工具处理器
func (tb *ToolBuilder) Handle(handler ToolHandler) error {
	return tb.fastmcp.server.RegisterTool(tb.name, tb.description, tb.inputSchema, handler)
}

// HandleWithValidation 带验证的工具处理器
func (tb *ToolBuilder) HandleWithValidation(handler ToolHandler) error {
	wrappedHandler := func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
		if err := validateToolArguments(args, tb.inputSchema); err != nil {
			return protocol.NewToolResultError(err.Error()), nil
		}
		return handler(ctx, args)
	}
	return tb.Handle(wrappedHandler)
}

// Resource 资源链式API
func (f *FastMCP) Resource(uri, name, description string) *ResourceBuilder {
	return &ResourceBuilder{
		fastmcp:     f,
		uri:         uri,
		name:        name,
		description: description,
		mimeType:    "text/plain",
	}
}

// WithMimeType 设置MIME类型
func (rb *ResourceBuilder) WithMimeType(mimeType string) *ResourceBuilder {
	rb.mimeType = mimeType
	return rb
}

// Handle 注册资源处理器
func (rb *ResourceBuilder) Handle(handler ResourceHandler) error {
	return rb.fastmcp.server.RegisterResource(rb.uri, rb.name, rb.description, rb.mimeType, handler)
}

// Prompt 提示模板链式API
func (f *FastMCP) Prompt(name, description string) *PromptBuilder {
	return &PromptBuilder{
		fastmcp:     f,
		name:        name,
		description: description,
		arguments:   []protocol.PromptArgument{},
	}
}

// WithArgument 添加参数
func (pb *PromptBuilder) WithArgument(name, description string, required bool) *PromptBuilder {
	pb.arguments = append(pb.arguments, protocol.NewPromptArgument(name, description, required))
	return pb
}

// Handle 注册提示模板处理器
func (pb *PromptBuilder) Handle(handler PromptHandler) error {
	return pb.fastmcp.server.RegisterPrompt(pb.name, pb.description, pb.arguments, handler)
}

func (f *FastMCP) HandleMessage(ctx context.Context, data []byte) ([]byte, error) {
	var msg protocol.JSONRPCMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse JSON-RPC message: %w", err)
	}

	response, err := f.server.HandleMessage(ctx, &msg)
	if err != nil {
		return nil, err
	}

	// 不返回任何数据
	if response == nil {
		return nil, nil
	}

	return json.Marshal(response)
}

func (f *FastMCP) SetNotificationHandler(handler func(method string, params interface{}) error) {
	f.server.SetNotificationHandler(handler)
}

func (f *FastMCP) SendNotification(method string, params interface{}) error {
	return f.server.SendNotification(method, params)
}

func (f *FastMCP) GetServerInfo() protocol.ServerInfo {
	return f.server.GetServerInfo()
}

func (f *FastMCP) GetCapabilities() protocol.ServerCapabilities {
	return f.server.GetCapabilities()
}

func (f *FastMCP) SimpleTextTool(name, description string, handler func(ctx context.Context, args map[string]interface{}) (string, error)) error {
	return f.Tool(name, description).Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
		text, err := handler(ctx, args)
		if err != nil {
			return protocol.NewToolResultError(err.Error()), nil
		}
		return protocol.NewToolResultText(text), nil
	})
}

func (f *FastMCP) SimpleTextResource(uri, name, description string, handler func(ctx context.Context) (string, error)) error {
	return f.Resource(uri, name, description).Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
		text, err := handler(ctx)
		if err != nil {
			return nil, err
		}
		return protocol.NewReadResourceResult(protocol.NewTextResourceContents(uri, text)), nil
	})
}

func (f *FastMCP) SimpleTextPrompt(name, description string, handler func(ctx context.Context, args map[string]string) (string, error)) error {
	return f.Prompt(name, description).Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
		text, err := handler(ctx, args)
		if err != nil {
			return nil, err
		}
		messages := []protocol.PromptMessage{
			protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(text)),
		}
		return protocol.NewGetPromptResult(description, messages...), nil
	})
}

func validateToolArguments(args map[string]interface{}, schema protocol.JSONSchema) error {
	if required, ok := schema["required"].([]string); ok {
		for _, field := range required {
			if _, exists := args[field]; !exists {
				return fmt.Errorf("required field '%s' is missing", field)
			}
		}
	}
	return nil
}

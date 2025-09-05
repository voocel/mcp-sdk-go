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
	fastmcp      *FastMCP
	name         string
	description  string
	inputSchema  protocol.JSONSchema
	outputSchema protocol.JSONSchema // MCP 2025-06-18 added
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

// WithIntParam adds integer parameter
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

// WithBoolParam adds boolean parameter
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

// WithNumberParam adds number parameter (supports integers and floats)
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

// WithInputSchema sets custom input schema
func (tb *ToolBuilder) WithInputSchema(schema protocol.JSONSchema) *ToolBuilder {
	tb.inputSchema = schema
	return tb
}

// WithStructSchema automatically generates schema using struct
func (tb *ToolBuilder) WithStructSchema(v interface{}) *ToolBuilder {
	schema, err := utils.StructToJSONSchema(v)
	if err == nil {
		tb.inputSchema = schema
	}
	return tb
}

// WithOutputSchema sets output schema (MCP 2025-06-18)
func (tb *ToolBuilder) WithOutputSchema(schema protocol.JSONSchema) *ToolBuilder {
	tb.outputSchema = schema
	return tb
}

// WithStructOutputSchema automatically generates output schema using struct
func (tb *ToolBuilder) WithStructOutputSchema(v interface{}) *ToolBuilder {
	schema, err := utils.StructToJSONSchema(v)
	if err == nil {
		tb.outputSchema = schema
	}
	return tb
}

// Handle registers tool handler
func (tb *ToolBuilder) Handle(handler ToolHandler) error {
	if len(tb.outputSchema) > 0 {
		return tb.fastmcp.server.RegisterTool(tb.name, tb.description, tb.inputSchema, handler, ToolOptions{
			OutputSchema: tb.outputSchema,
		})
	}
	return tb.fastmcp.server.RegisterTool(tb.name, tb.description, tb.inputSchema, handler)
}

// HandleWithElicitation registers tool handler with elicitation support
func (tb *ToolBuilder) HandleWithElicitation(handler ToolHandlerWithElicitation) error {
	// wrap handler to provide MCPContext
	wrappedHandler := func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
		mcpCtx := tb.fastmcp.server.CreateMCPContext(ctx)
		return handler(mcpCtx, args)
	}

	if len(tb.outputSchema) > 0 {
		return tb.fastmcp.server.RegisterTool(tb.name, tb.description, tb.inputSchema, wrappedHandler, ToolOptions{
			OutputSchema: tb.outputSchema,
		})
	}
	return tb.fastmcp.server.RegisterTool(tb.name, tb.description, tb.inputSchema, wrappedHandler)
}

// HandleWithValidation tool handler with validation
func (tb *ToolBuilder) HandleWithValidation(handler ToolHandler) error {
	wrappedHandler := func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
		if err := validateToolArguments(args, tb.inputSchema); err != nil {
			return protocol.NewToolResultError(err.Error()), nil
		}

		result, err := handler(ctx, args)
		if err != nil {
			return nil, err
		}

		// validate structured output
		if result.StructuredContent != nil && tb.outputSchema != nil {
			if err := protocol.ValidateStructuredOutput(result.StructuredContent, tb.outputSchema); err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("output validation failed: %v", err)), nil
			}
		}

		return result, nil
	}
	return tb.Handle(wrappedHandler)
}

// Resource resource fluent API
func (f *FastMCP) Resource(uri, name, description string) *ResourceBuilder {
	return &ResourceBuilder{
		fastmcp:     f,
		uri:         uri,
		name:        name,
		description: description,
		mimeType:    "text/plain",
	}
}

// WithMimeType sets MIME type
func (rb *ResourceBuilder) WithMimeType(mimeType string) *ResourceBuilder {
	rb.mimeType = mimeType
	return rb
}

// Handle registers resource handler
func (rb *ResourceBuilder) Handle(handler ResourceHandler) error {
	return rb.fastmcp.server.RegisterResource(rb.uri, rb.name, rb.description, rb.mimeType, handler)
}

// Prompt prompt template fluent API
func (f *FastMCP) Prompt(name, description string) *PromptBuilder {
	return &PromptBuilder{
		fastmcp:     f,
		name:        name,
		description: description,
		arguments:   []protocol.PromptArgument{},
	}
}

// WithArgument adds argument
func (pb *PromptBuilder) WithArgument(name, description string, required bool) *PromptBuilder {
	pb.arguments = append(pb.arguments, protocol.NewPromptArgument(name, description, required))
	return pb
}

// Handle registers prompt template handler
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

	// do not return any data
	if response == nil {
		return nil, nil
	}

	return json.Marshal(response)
}

func (f *FastMCP) SetNotificationHandler(handler func(method string, params interface{}) error) {
	f.server.SetNotificationHandler(handler)
}

func (f *FastMCP) SetElicitor(elicitor Elicitor) {
	f.server.SetElicitor(elicitor)
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

// SimpleStructuredTool creates a simple tool that returns structured data (MCP 2025-06-18)
func (f *FastMCP) SimpleStructuredTool(name, description string, outputSchema protocol.JSONSchema, handler func(ctx context.Context, args map[string]interface{}) (interface{}, error)) error {
	return f.Tool(name, description).
		WithOutputSchema(outputSchema).
		HandleWithValidation(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			data, err := handler(ctx, args)
			if err != nil {
				return protocol.NewToolResultError(err.Error()), nil
			}
			return protocol.NewToolResultWithStructured(
				[]protocol.Content{protocol.NewTextContent("Operation completed successfully")},
				data,
			), nil
		})
}

// SimpleStructuredToolWithText creates a tool that returns both text and structured data
func (f *FastMCP) SimpleStructuredToolWithText(name, description string, outputSchema protocol.JSONSchema, handler func(ctx context.Context, args map[string]interface{}) (string, interface{}, error)) error {
	return f.Tool(name, description).
		WithOutputSchema(outputSchema).
		HandleWithValidation(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			text, data, err := handler(ctx, args)
			if err != nil {
				return protocol.NewToolResultError(err.Error()), nil
			}
			return protocol.NewToolResultTextWithStructured(text, data), nil
		})
}

// SimpleElicitationTool creates a simple tool with elicitation support
func (f *FastMCP) SimpleElicitationTool(name, description string, handler func(ctx *MCPContext, args map[string]interface{}) (string, error)) error {
	return f.Tool(name, description).HandleWithElicitation(func(ctx *MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
		text, err := handler(ctx, args)
		if err != nil {
			return protocol.NewToolResultError(err.Error()), nil
		}
		return protocol.NewToolResultText(text), nil
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

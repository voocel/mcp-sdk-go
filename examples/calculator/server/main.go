package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		log.Println("Received shutdown signal")
		cancel()
	}()

	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "Calculator Service",
		Version: "1.0.0",
	}, nil)

	// Addition tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "add",
			Description: "Add two numbers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "First number",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "Second number",
					},
				},
				"required": []string{"a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			a, ok := req.Params.Arguments["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("Parameter 'a' must be a number"), nil
			}
			b, ok := req.Params.Arguments["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("Parameter 'b' must be a number"), nil
			}

			result := a + b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		},
	)

	// Subtraction tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "subtract",
			Description: "Subtract one number from another",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "Minuend",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "Subtrahend",
					},
				},
				"required": []string{"a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			a, ok := req.Params.Arguments["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("Parameter 'a' must be a number"), nil
			}
			b, ok := req.Params.Arguments["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("Parameter 'b' must be a number"), nil
			}

			result := a - b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		},
	)

	// Multiplication tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "multiply",
			Description: "Multiply two numbers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "First number",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "Second number",
					},
				},
				"required": []string{"a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			a, ok := req.Params.Arguments["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("Parameter 'a' must be a number"), nil
			}
			b, ok := req.Params.Arguments["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("Parameter 'b' must be a number"), nil
			}

			result := a * b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		},
	)

	// Division tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "divide",
			Description: "Divide one number by another",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "Dividend",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "Divisor",
					},
				},
				"required": []string{"a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			a, ok := req.Params.Arguments["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("Parameter 'a' must be a number"), nil
			}
			b, ok := req.Params.Arguments["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("Parameter 'b' must be a number"), nil
			}

			if b == 0 {
				return protocol.NewToolResultError("Cannot divide by zero"), nil
			}

			result := a / b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		},
	)

	// Help prompt
	mcpServer.AddPrompt(
		&protocol.Prompt{
			Name:        "calculator_help",
			Description: "Calculator help information",
		},
		func(ctx context.Context, req *server.GetPromptRequest) (*protocol.GetPromptResult, error) {
			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"This is a simple calculator service that supports four basic operations: addition, subtraction, multiplication, and division.")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					"How do I use this calculator?")),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					"Use the add, subtract, multiply, and divide tools to perform calculations. Each tool accepts two parameters: a and b.")),
			}
			return protocol.NewGetPromptResult("Prompt template for providing help with programming questions", messages...), nil
		},
	)

	log.Println("Starting Calculator MCP Server (STDIO)...")

	if err := mcpServer.Run(ctx, &stdio.StdioTransport{}); err != nil && err != context.Canceled {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server closed")
}

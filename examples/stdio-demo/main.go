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

func greetHandler(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
	name, ok := req.Params.Arguments["name"].(string)
	if !ok {
		return protocol.NewToolResultError("Parameter 'name' must be a string"), nil
	}
	greeting := "Hello, " + name + "! Welcome to MCP STDIO Server!"
	return &protocol.CallToolResult{
		Content: []protocol.Content{protocol.NewTextContent(greeting)},
	}, nil
}

func calculateHandler(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
	operation, ok := req.Params.Arguments["operation"].(string)
	if !ok {
		return protocol.NewToolResultError("Parameter 'operation' must be a string"), nil
	}
	a, ok := req.Params.Arguments["a"].(float64)
	if !ok {
		return protocol.NewToolResultError("Parameter 'a' must be a number"), nil
	}
	b, ok := req.Params.Arguments["b"].(float64)
	if !ok {
		return protocol.NewToolResultError("Parameter 'b' must be a number"), nil
	}
	var result float64
	switch operation {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return protocol.NewToolResultError("Divisor cannot be zero"), nil
		}
		result = a / b
	default:
		return protocol.NewToolResultError("Unsupported operation type"), nil
	}
	return &protocol.CallToolResult{
		Content: []protocol.Content{protocol.NewTextContent(fmt.Sprintf("Result: %.2f", result))},
	}, nil
}

func main() {
	mcpServer := server.NewServer(&protocol.ServerInfo{Name: "StdioMCPServer", Version: "1.0.0"}, nil)
	mcpServer.AddTool(&protocol.Tool{
		Name: "greet", Description: "Greet the user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "User name"},
			},
			"required": []string{"name"},
		},
	}, greetHandler)
	mcpServer.AddTool(&protocol.Tool{
		Name: "calculate", Description: "Perform basic mathematical operations",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{"type": "string", "description": "Operation type (add, subtract, multiply, divide)"},
				"a":         map[string]interface{}{"type": "number", "description": "First number"},
				"b":         map[string]interface{}{"type": "number", "description": "Second number"},
			},
			"required": []string{"operation", "a", "b"},
		},
	}, calculateHandler)
	mcpServer.AddResource(&protocol.Resource{
		URI: "info://server", Name: "server_info", Description: "Server information", MimeType: "text/plain",
	}, func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
		return &protocol.ReadResourceResult{
			Contents: []protocol.ResourceContents{{
				URI: "info://server", MimeType: "text/plain",
				Text: fmt.Sprintf("MCP STDIO Server v1.0.0\nProtocol: %s", protocol.MCPVersion),
			}},
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received interrupt signal, shutting down server...")
		cancel()
	}()

	log.Println("Starting STDIO MCP Server")
	log.Printf("Protocol Version: MCP %s", protocol.MCPVersion)
	log.Println("Waiting for client connection...")

	if err := mcpServer.Run(ctx, &stdio.StdioTransport{}); err != nil && err != context.Canceled {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server closed")
}

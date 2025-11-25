package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

func main() {
	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "BasicMCPServer",
		Version: "1.0.0",
	}, nil)

	// ========== Basic Tools ==========
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "greet",
			Description: "Greet the user",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "User name",
					},
				},
				"required": []string{"name"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			name, ok := req.Params.Arguments["name"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'name' must be a string"), nil
			}
			greeting := fmt.Sprintf("Hello, %s! Welcome to MCP!", name)
			return protocol.NewToolResultText(greeting), nil
		},
	)

	// ========== Tools with Metadata (MCP 2025-06-18) ==========
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "calculate",
			Description: "Perform mathematical calculations",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"description": "Operation type (add, subtract, multiply, divide)",
					},
					"a": map[string]interface{}{
						"type":        "number",
						"description": "First number",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "Second number",
					},
				},
				"required": []string{"operation", "a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			operation, _ := req.Params.Arguments["operation"].(string)
			a := int(req.Params.Arguments["a"].(float64))
			b := int(req.Params.Arguments["b"].(float64))

			var result int
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

			// Return result with metadata
			return &protocol.CallToolResult{
				Content: []protocol.Content{
					protocol.NewTextContent(fmt.Sprintf("Calculation result: %d", result)),
				},
			}, nil
		},
	)

	// ========== Get Time Tool ==========
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "get_time",
			Description: "Get current time",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			now := time.Now()
			return &protocol.CallToolResult{
				Content: []protocol.Content{
					protocol.NewTextContent(now.Format(time.RFC3339)),
				},
			}, nil
		},
	)

	// ========== Basic Resources ==========
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "info://server",
			Name:        "server_info",
			Description: "Server information",
			MimeType:    "text/plain",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			info := fmt.Sprintf("MCP Basic Server v1.0.0\nProtocol: %s\nFeatures: Tools, Resources, Prompts", protocol.MCPVersion)
			return &protocol.ReadResourceResult{
				Contents: []protocol.ResourceContents{
					{
						URI:      "info://server",
						MimeType: "text/plain",
						Text:     info,
					},
				},
			}, nil
		},
	)

	// ========== JSON Configuration Resource ==========
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "config://app",
			Name:        "app_config",
			Description: "Application configuration",
			MimeType:    "application/json",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			config := `{
  "app": "BasicMCPServer",
  "version": "1.0.0",
  "features": ["tools", "resources", "prompts"]
}`
			return &protocol.ReadResourceResult{
				Contents: []protocol.ResourceContents{
					{
						URI:      "config://app",
						MimeType: "application/json",
						Text:     config,
					},
				},
			}, nil
		},
	)

	// ========== Resource Template Example ==========
	mcpServer.AddResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "echo:///{message}",
			Name:        "echo",
			Description: "Echo message",
			MimeType:    "text/plain",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			// Extract message from URI
			message := "hello" // Simplified example, should parse from URI in practice
			return &protocol.ReadResourceResult{
				Contents: []protocol.ResourceContents{
					{
						URI:      req.Params.URI,
						MimeType: "text/plain",
						Text:     fmt.Sprintf("Echo: %s", message),
					},
				},
			}, nil
		},
	)

	// ========== Prompt Templates ==========
	mcpServer.AddPrompt(
		&protocol.Prompt{
			Name:        "code_review",
			Description: "Code review prompt",
			Arguments: []protocol.PromptArgument{
				{
					Name:        "language",
					Description: "Programming language",
					Required:    true,
				},
				{
					Name:        "code",
					Description: "Code to review",
					Required:    true,
				},
			},
		},
		func(ctx context.Context, req *server.GetPromptRequest) (*protocol.GetPromptResult, error) {
			language, _ := req.Params.Arguments["language"]
			code, _ := req.Params.Arguments["code"]

			return &protocol.GetPromptResult{
				Description: "Code review prompt",
				Messages: []protocol.PromptMessage{
					{
						Role: protocol.RoleUser,
						Content: protocol.NewTextContent(
							fmt.Sprintf("Please review the following %s code and provide improvement suggestions:\n\n```%s\n%s\n```", language, language, code),
						),
					},
				},
			}, nil
		},
	)

	// ========== Resource Link Tool ==========
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "find_file",
			Description: "Find file and return resource link",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filename": map[string]interface{}{
						"type":        "string",
						"description": "Filename to find",
					},
				},
				"required": []string{"filename"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			filename, _ := req.Params.Arguments["filename"].(string)

			fileURI := fmt.Sprintf("file:///project/src/%s", filename)
			resourceLink := protocol.NewResourceLinkContent(fileURI)

			return &protocol.CallToolResult{
				Content: []protocol.Content{
					protocol.NewTextContent(fmt.Sprintf("Found file: %s", filename)),
					resourceLink,
				},
			}, nil
		},
	)

	// ========== Start Server ==========
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received interrupt signal, shutting down server...")
		cancel()
	}()

	log.Println("========================================")
	log.Println("MCP Basic Server Started")
	log.Println("========================================")
	log.Println("Transport: STDIO")
	log.Println("Protocol Version: MCP", protocol.MCPVersion)
	log.Println("")
	log.Println("Features:")
	log.Println("  ✓ Basic tool (greet)")
	log.Println("  ✓ Calculator tool (calculate)")
	log.Println("  ✓ Time tool (get_time)")
	log.Println("  ✓ Basic resource (info://server)")
	log.Println("  ✓ JSON configuration resource (config://app)")
	log.Println("  ✓ Resource template (echo:///{message})")
	log.Println("  ✓ Prompt template (code_review)")
	log.Println("  ✓ Resource link tool (find_file)")
	log.Println("========================================")
	log.Println("Waiting for client connection...")

	if err := mcpServer.Run(ctx, &stdio.StdioTransport{}); err != nil && err != context.Canceled {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server closed")
}

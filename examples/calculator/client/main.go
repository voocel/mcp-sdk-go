package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "calculator-client",
		Version: "1.0.0",
	}, nil)

	// Create CommandTransport to connect to calculator service
	transport := client.NewCommandTransport("go", "run", "../server/main.go")

	fmt.Println("Connecting to calculator service...")
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer session.Close()

	initResult := session.InitializeResult()
	fmt.Printf("Connection successful! Server: %s v%s\n",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to get tools list: %v", err)
	}

	fmt.Println("\nAvailable tools:")
	for _, tool := range toolsResult.Tools {
		fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
	}
	fmt.Println()

	// Test addition
	fmt.Println("Testing calculation functions:")
	result, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "add",
		Arguments: map[string]any{
			"a": 5.0,
			"b": 3.0,
		},
	})
	if err != nil {
		log.Fatalf("Failed to call add tool: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  5 + 3 = %s\n", textContent.Text)
		}
	}

	// Test subtraction
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "subtract",
		Arguments: map[string]any{
			"a": 10.0,
			"b": 4.0,
		},
	})
	if err != nil {
		log.Fatalf("Failed to call subtract tool: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  10 - 4 = %s\n", textContent.Text)
		}
	}

	// Test multiplication
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "multiply",
		Arguments: map[string]any{
			"a": 6.0,
			"b": 7.0,
		},
	})
	if err != nil {
		log.Fatalf("Failed to call multiply tool: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  6 * 7 = %s\n", textContent.Text)
		}
	}

	// Test division
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "divide",
		Arguments: map[string]any{
			"a": 20.0,
			"b": 5.0,
		},
	})
	if err != nil {
		log.Fatalf("Failed to call divide tool: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  20 / 5 = %s\n", textContent.Text)
		}
	}

	// Test division by zero error
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "divide",
		Arguments: map[string]any{
			"a": 20.0,
			"b": 0.0,
		},
	})
	if err != nil {
		fmt.Printf("  Division by zero error: %v\n", err)
	} else if result.IsError && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  Division by zero error: %s\n", textContent.Text)
		}
	}

	// Get help prompt template
	fmt.Println("\nGetting help information:")
	promptResult, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
		Name:      "calculator_help",
		Arguments: map[string]string{},
	})
	if err != nil {
		log.Fatalf("Failed to get prompt template: %v", err)
	}

	fmt.Printf("  Description: %s\n", promptResult.Description)
	fmt.Println("  Conversation example:")
	for i, msg := range promptResult.Messages {
		if textContent, ok := msg.Content.(protocol.TextContent); ok {
			fmt.Printf("    %d. [%s]: %s\n", i+1, msg.Role, textContent.Text)
		}
	}

	fmt.Println("\n end!")
}

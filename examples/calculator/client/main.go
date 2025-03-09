package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, err := client.New(client.WithWebSocketTransport("ws://localhost:8080"))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	if err := c.Initialize(ctx); err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		log.Fatalf("Failed to get tool list: %v", err)
	}

	fmt.Println("Available tools:")
	for _, tool := range tools {
		fmt.Printf("- %s: %s\n", tool.Name, tool.Description)
	}
	fmt.Println()

	result, err := c.CallTool(ctx, "add", map[string]interface{}{
		"a": 5,
		"b": 3,
	})
	if err != nil {
		log.Fatalf("Failed to call add tool: %v", err)
	}
	fmt.Printf("5 + 3 = %s\n", result.Content[0].(map[string]interface{})["text"])

	result, err = c.CallTool(ctx, "subtract", map[string]interface{}{
		"a": 10,
		"b": 4,
	})
	if err != nil {
		log.Fatalf("Failed to call subtract tool: %v", err)
	}
	fmt.Printf("10 - 4 = %s\n", result.Content[0].(map[string]interface{})["text"])

	result, err = c.CallTool(ctx, "multiply", map[string]interface{}{
		"arg0": 6,
		"arg1": 7,
	})
	if err != nil {
		log.Fatalf("Failed to call multiply tool: %v", err)
	}
	fmt.Printf("6 * 7 = %s\n", result.Content[0].(map[string]interface{})["text"])

	result, err = c.CallTool(ctx, "divide", map[string]interface{}{
		"a": 20,
		"b": 5,
	})
	if err != nil {
		log.Fatalf("Failed to call divide tool: %v", err)
	}
	fmt.Printf("20 / 5 = %s\n", result.Content[0].(map[string]interface{})["text"])

	_, err = c.CallTool(ctx, "divide", map[string]interface{}{
		"a": 20,
		"b": 0,
	})
	if err != nil {
		fmt.Printf("Division by zero error: %v\n", err)
	}

	prompt, err := c.GetPrompt(ctx, "calculator_help", nil)
	if err != nil {
		log.Fatalf("Failed to get prompt: %v", err)
	}

	fmt.Println("\nCalculator help:")
	for _, msg := range prompt.Messages {
		role := msg.Role
		content := msg.Content.(map[string]interface{})
		text := content["text"].(string)
		fmt.Printf("[%s]: %s\n", role, text)
	}

	os.Exit(0)
}

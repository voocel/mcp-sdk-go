package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Println("Starting SSE client...")

	transport, err := sse.NewSSETransport("http://localhost:8080")
	if err != nil {
		log.Fatalf("Failed to create SSE Transport: %v", err)
	}

	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "SSE Demo Client",
		Version: "1.0.0",
	}, nil)

	log.Println("Connecting to server...")
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer session.Close()

	log.Println("Connected successfully!")

	// List tools
	log.Println("\nListing available tools...")
	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	fmt.Printf("Found %d tools:\n", len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
	}

	// Call greet tool
	log.Println("\nCalling greet tool...")
	greetResult, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "greet",
		Arguments: map[string]interface{}{
			"name": "Go Developer",
		},
	})
	if err != nil {
		log.Fatalf("Failed to call tool: %v", err)
	}

	if len(greetResult.Content) > 0 {
		if textContent, ok := greetResult.Content[0].(protocol.TextContent); ok {
			fmt.Printf("Result: %s\n", textContent.Text)
		}
	}

	// Call get_time tool
	log.Println("\nCalling get_time tool...")
	timeResult, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name:      "get_time",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		log.Fatalf("Failed to call tool: %v", err)
	}

	if len(timeResult.Content) > 0 {
		if textContent, ok := timeResult.Content[0].(protocol.TextContent); ok {
			fmt.Printf("Result: %s\n", textContent.Text)
		}
	}

	log.Println("\nListing available resources...")
	resourcesResult, err := session.ListResources(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to list resources: %v", err)
	}

	fmt.Printf("Found %d resources:\n", len(resourcesResult.Resources))
	for _, resource := range resourcesResult.Resources {
		fmt.Printf("  - %s: %s\n", resource.Name, resource.Description)
	}

	log.Println("\nReading server information resource...")
	resourceResult, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
		URI: "info://server",
	})
	if err != nil {
		log.Fatalf("Failed to read resource: %v", err)
	}

	if len(resourceResult.Contents) > 0 {
		content := resourceResult.Contents[0]
		if content.Text != "" {
			fmt.Printf("\n%s\n", content.Text)
		}
	}

	log.Println("\nDone!")
}

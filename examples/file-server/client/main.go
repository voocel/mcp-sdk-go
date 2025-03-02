package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := client.New(client.WithSSETransport("http://localhost:8081"))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	if err := c.Initialize(ctx); err != nil {
		log.Fatalf("Initialization failed: %v", err)
	}

	resource, err := c.ReadResource(ctx, "file://current")
	if err != nil {
		log.Fatalf("Failed to read resource: %v", err)
	}
	currentDir := resource.Content[0].(map[string]interface{})["text"].(string)
	fmt.Printf("Current working directory: %s\n\n", currentDir)

	result, err := c.CallTool(ctx, "list_directory", map[string]interface{}{
		"path": currentDir,
	})
	if err != nil {
		log.Fatalf("Failed to call list_directory tool: %v", err)
	}
	fmt.Printf("Directory contents:\n%s\n\n", result.Content[0].(map[string]interface{})["text"])

	_, currentFilePath, _, _ := runtime.Caller(0)
	result, err = c.CallTool(ctx, "read_file", map[string]interface{}{
		"path": currentFilePath,
	})
	if err != nil {
		log.Fatalf("Failed to call read_file tool: %v", err)
	}
	content := result.Content[0].(map[string]interface{})["text"].(string)
	fmt.Printf("First 100 characters of current file:\n%s\n\n", content[:min(100, len(content))])

	result, err = c.CallTool(ctx, "search_files", map[string]interface{}{
		"directory": filepath.Dir(currentDir),
		"pattern":   "MCP",
	})
	if err != nil {
		log.Fatalf("Failed to call search_files tool: %v", err)
	}
	fmt.Printf("Search results:\n%s\n", result.Content[0].(map[string]interface{})["text"])

	os.Exit(0)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

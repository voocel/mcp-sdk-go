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
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "file-client",
		Version: "1.0.0",
	}, nil)

	transport, err := sse.NewSSETransport("http://localhost:8081")
	if err != nil {
		log.Fatalf("Failed to create Transport: %v", err)
	}

	fmt.Println("Connecting to file server...")
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer session.Close()

	initResult := session.InitializeResult()
	fmt.Printf("Connection successful! Server: %s v%s\n",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	// Get current directory resource
	fmt.Println("\nGetting current working directory...")
	resourceResult, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
		URI: "file://current",
	})
	if err != nil {
		log.Fatalf("Failed to read resource: %v", err)
	}

	var currentDir string
	if len(resourceResult.Contents) > 0 && resourceResult.Contents[0].Text != "" {
		currentDir = resourceResult.Contents[0].Text
		fmt.Printf("Current working directory: %s\n", currentDir)
	} else {
		// Fall back to current directory if resource read fails
		currentDir, _ = os.Getwd()
		fmt.Printf("Current working directory: %s\n", currentDir)
	}

	// List current directory contents
	fmt.Println("\nCurrent directory contents:")
	result, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "list_directory",
		Arguments: map[string]any{
			"path": currentDir,
		},
	})
	if err != nil {
		log.Fatalf("Failed to call list_directory tool: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("%s\n", textContent.Text)
		}
	}

	// Read first 100 characters of current file
	fmt.Println("\nReading current file content preview:")
	_, currentFilePath, _, _ := runtime.Caller(0)
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "read_file",
		Arguments: map[string]any{
			"path": currentFilePath,
		},
	})
	if err != nil {
		fmt.Printf("Failed to call read_file tool: %v\n", err)
	} else if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			content := textContent.Text
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Printf("First 200 characters of file %s:\n%s\n", filepath.Base(currentFilePath), content)
		}
	}

	// Search for files containing "MCP"
	fmt.Println("\nSearching for files containing 'MCP':")
	searchDir := filepath.Dir(currentDir)
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "search_files",
		Arguments: map[string]any{
			"directory": searchDir,
			"pattern":   "MCP",
		},
	})
	if err != nil {
		fmt.Printf("Failed to call search_files tool: %v\n", err)
	} else if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("%s\n", textContent.Text)
		}
	}

	// Get file operation help
	fmt.Println("\nGetting file operation help:")
	promptResult, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
		Name:      "file_help",
		Arguments: map[string]string{},
	})
	if err != nil {
		fmt.Printf("Failed to get help prompt: %v\n", err)
	} else {
		fmt.Printf("Description: %s\n", promptResult.Description)
		fmt.Println("Help information:")
		for i, message := range promptResult.Messages {
			if textContent, ok := message.Content.(protocol.TextContent); ok {
				fmt.Printf("  %d. [%s]: %s\n", i+1, message.Role, textContent.Text)
			}
		}
	}

	fmt.Println("\nDemonstrating error handling - attempting to access non-existent directory:")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "list_directory",
		Arguments: map[string]any{
			"path": "/nonexistent/directory",
		},
	})
	if err != nil {
		fmt.Printf("Expected error: %v\n", err)
	} else if result.IsError && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("Server returned error: %s\n", textContent.Text)
		}
	}

	fmt.Println("\nDemonstrating security check - attempting path traversal:")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "read_file",
		Arguments: map[string]any{
			"path": "../../../etc/passwd",
		},
	})
	if err != nil {
		fmt.Printf("Security check effective: %v\n", err)
	} else if result.IsError && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("Security check effective: %s\n", textContent.Text)
		}
	}

	fmt.Println("\n end!")
}

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/sse"
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
		Name:    "File Server",
		Version: "1.0.0",
	}, nil)

	// Register list directory tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "list_directory",
			Description: "List files in a specified directory",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory path",
					},
				},
				"required": []string{"path"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			path, ok := req.Params.Arguments["path"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'path' must be a string"), nil
			}

			// Security check: prevent path traversal attacks
			if strings.Contains(path, "..") {
				return protocol.NewToolResultError("Access to parent directories is not allowed"), nil
			}

			files, err := ioutil.ReadDir(path)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("Cannot read directory: %v", err)), nil
			}

			var fileList []string
			for _, file := range files {
				fileType := "file"
				if file.IsDir() {
					fileType = "directory"
				}
				fileList = append(fileList, fmt.Sprintf("%s (%s, %d bytes)", file.Name(), fileType, file.Size()))
			}

			if len(fileList) == 0 {
				return protocol.NewToolResultText("Directory is empty"), nil
			}

			result := strings.Join(fileList, "\n")
			return protocol.NewToolResultText(result), nil
		},
	)

	// Register current directory resource
	currentDir, _ := os.Getwd()
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "file://current",
			Name:        "Current Working Directory",
			Description: "Path to the current working directory",
			MimeType:    "text/plain",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			contents := protocol.NewTextResourceContents("file://current", currentDir)
			return protocol.NewReadResourceResult(contents), nil
		},
	)

	// Register file read tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "read_file",
			Description: "Read file content",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path",
					},
				},
				"required": []string{"path"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			path, ok := req.Params.Arguments["path"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'path' must be a string"), nil
			}

			// Security check: prevent path traversal attacks
			if strings.Contains(path, "..") {
				return protocol.NewToolResultError("Access to parent directories is not allowed"), nil
			}

			// Check file size (limit to 1MB)
			if fileInfo, err := os.Stat(path); err == nil {
				if fileInfo.Size() > 1024*1024 {
					return protocol.NewToolResultError("File too large (exceeds 1MB limit)"), nil
				}
			}

			content, err := ioutil.ReadFile(path)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("Cannot read file: %v", err)), nil
			}

			return protocol.NewToolResultText(string(content)), nil
		},
	)

	// Register file search tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "search_files",
			Description: "Search for files containing specific content",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"directory": map[string]interface{}{
						"type":        "string",
						"description": "Search directory",
					},
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Search content",
					},
				},
				"required": []string{"directory", "pattern"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			directory, ok := req.Params.Arguments["directory"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'directory' must be a string"), nil
			}
			pattern, ok := req.Params.Arguments["pattern"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'pattern' must be a string"), nil
			}

			// Security check: prevent path traversal attacks
			if strings.Contains(directory, "..") {
				return protocol.NewToolResultError("Access to parent directories is not allowed"), nil
			}

			var results []string

			err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // Ignore inaccessible files
				}

				if info.IsDir() {
					return nil
				}

				// Only search text files smaller than 10MB
				if info.Size() < 10*1024*1024 && isTextFile(path) {
					content, err := ioutil.ReadFile(path)
					if err != nil {
						return nil // Ignore unreadable files
					}

					if strings.Contains(string(content), pattern) {
						results = append(results, path)
					}
				}

				return nil
			})

			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("Error searching files: %v", err)), nil
			}

			if len(results) == 0 {
				return protocol.NewToolResultText("No matching files found"), nil
			}

			result := fmt.Sprintf("Found %d matching files:\n%s", len(results), strings.Join(results, "\n"))
			return protocol.NewToolResultText(result), nil
		},
	)

	// Register file help prompt template
	mcpServer.AddPrompt(
		&protocol.Prompt{
			Name:        "file_help",
			Description: "File operation help",
		},
		func(ctx context.Context, req *server.GetPromptRequest) (*protocol.GetPromptResult, error) {
			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"This is a file server that provides file and directory operations. It supports listing directory contents, reading files, and searching files.")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					"How do I use the file server?")),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					"You can use the following features:\n1. list_directory - List files in a directory\n2. read_file - Read file contents\n3. search_files - Search for files containing specific content in a directory\n4. Access the file://current resource to get the current directory path")),
			}
			return protocol.NewGetPromptResult("File server operation guide", messages...), nil
		},
	)

	handler := sse.NewHTTPHandler(func(r *http.Request) *server.Server {
		return mcpServer
	})
	httpServer := &http.Server{
		Addr:    ":8081",
		Handler: handler,
	}

	log.Println("Starting File Server MCP Service (SSE) on port :8081...")

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-ctx.Done()

	log.Println("Server shutdown")
}

func isTextFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	textExtensions := []string{
		".txt", ".md", ".go", ".js", ".ts", ".html", ".css", ".json",
		".xml", ".csv", ".log", ".yaml", ".yml", ".toml", ".ini",
		".py", ".java", ".c", ".cpp", ".h", ".hpp", ".rs", ".php",
		".rb", ".sh", ".bat", ".ps1", ".dockerfile", ".makefile",
	}

	for _, ext := range textExtensions {
		if extension == ext {
			return true
		}
	}

	// Check common text files without extensions
	filename := strings.ToLower(filepath.Base(path))
	textFiles := []string{"readme", "license", "changelog", "makefile", "dockerfile"}
	for _, textFile := range textFiles {
		if filename == textFile {
			return true
		}
	}

	return false
}

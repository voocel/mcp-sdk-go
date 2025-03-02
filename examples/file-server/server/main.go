package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
)

func main() {
	mcp := server.New("File Server", "1.0.0")

	mcp.Tool("list_directory", "List files in specified directory").
		WithStringParam("path", "Directory path", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			path := args["path"].(string)

			files, err := ioutil.ReadDir(path)
			if err != nil {
				return nil, fmt.Errorf("unable to read directory: %w", err)
			}

			var fileList []string
			for _, file := range files {
				fileType := "File"
				if file.IsDir() {
					fileType = "Directory"
				}
				fileList = append(fileList, fmt.Sprintf("%s (%s, %d bytes)", file.Name(), fileType, file.Size()))
			}

			result := strings.Join(fileList, "\n")
			return protocol.NewToolResultText(result), nil
		})

	currentDir, _ := os.Getwd()
	mcp.Resource("file://current", "Current working directory", func() []string {
		return []string{currentDir}
	})

	mcp.Tool("read_file", "Read file content").
		WithStringParam("path", "File path", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			path := args["path"].(string)

			content, err := ioutil.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("unable to read file: %w", err)
			}

			return protocol.NewToolResultText(string(content)), nil
		})

	mcp.Tool("search_files", "Search for files containing specific content").
		WithStringParam("directory", "Directory to search", true).
		WithStringParam("pattern", "Content to search for", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			directory := args["directory"].(string)
			pattern := args["pattern"].(string)

			var results []string

			err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				if info.IsDir() {
					return nil
				}

				if info.Size() < 10*1024*1024 && isTextFile(path) {
					content, err := ioutil.ReadFile(path)
					if err != nil {
						return nil
					}

					if strings.Contains(string(content), pattern) {
						results = append(results, path)
					}
				}

				return nil
			})

			if err != nil {
				return nil, fmt.Errorf("error searching files: %w", err)
			}

			if len(results) == 0 {
				return protocol.NewToolResultText("No matching files found"), nil
			}

			result := fmt.Sprintf("Found %d matching files:\n%s", len(results), strings.Join(results, "\n"))
			return protocol.NewToolResultText(result), nil
		})

	log.Println("Starting SSE server on :8081...")
	if err := mcp.ServeSSE(context.Background(), ":8081"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func isTextFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	textExtensions := []string{".txt", ".md", ".go", ".js", ".html", ".css", ".json", ".xml", ".csv", ".log"}

	for _, ext := range textExtensions {
		if extension == ext {
			return true
		}
	}

	return false
}

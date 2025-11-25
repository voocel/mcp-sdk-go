package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	serverInfo := &protocol.ServerInfo{
		Name:    "SSE Demo Server",
		Version: "1.0.0",
	}
	mcpServer := server.NewServer(serverInfo, nil)

	// Register greeting tool
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
			name := req.Params.Arguments["name"].(string)
			greeting := fmt.Sprintf("Hello, %s! Welcome to SSE Transport!", name)
			return protocol.NewToolResultText(greeting), nil
		},
	)

	// Register time tool
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
			now := time.Now().Format("2006-01-02 15:04:05")
			return protocol.NewToolResultText(fmt.Sprintf("Current time: %s", now)), nil
		},
	)

	// Register resource
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "info://server",
			Name:        "Server Information",
			Description: "Get basic server information",
			MimeType:    "text/plain",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			info := fmt.Sprintf("Server: %s v%s\nTransport: SSE\nTime: %s",
				serverInfo.Name,
				serverInfo.Version,
				time.Now().Format("2006-01-02 15:04:05"))

			contents := protocol.NewTextResourceContents("info://server", info)
			return protocol.NewReadResourceResult(contents), nil
		},
	)

	handler := sse.NewHTTPHandler(func(r *http.Request) *server.Server {
		return mcpServer
	})
	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: handler,
	}

	go func() {
		log.Println("SSE Server started at http://localhost:8080")
		log.Println("Using SSE Transport")
		log.Println("Fully compliant with new Transport/Connection interface")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("\nShutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := handler.Shutdown(shutdownCtx); err != nil {
		log.Printf("Handler shutdown error: %v", err)
	}

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Closed!")
}

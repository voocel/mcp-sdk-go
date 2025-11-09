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

	// 注册问候工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "greet",
			Description: "问候用户",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "用户名称",
					},
				},
				"required": []string{"name"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			name := req.Params.Arguments["name"].(string)
			greeting := fmt.Sprintf("你好,%s!欢迎使用 SSE Transport!", name)
			return protocol.NewToolResultText(greeting), nil
		},
	)

	// 注册时间工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "get_time",
			Description: "获取当前时间",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			now := time.Now().Format("2006-01-02 15:04:05")
			return protocol.NewToolResultText(fmt.Sprintf("当前时间: %s", now)), nil
		},
	)

	// 注册资源
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "info://server",
			Name:        "服务器信息",
			Description: "获取服务器基本信息",
			MimeType:    "text/plain",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			info := fmt.Sprintf("服务器: %s v%s\n传输协议: SSE\n时间: %s",
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
		log.Println("SSE Server 启动在 http://localhost:8080")
		log.Println("使用 SSE Transport")
		log.Println("完全符合新的 Transport/Connection 接口")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("\n正在关闭服务器...")

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

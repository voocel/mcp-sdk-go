package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

func greetHandler(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
	name, ok := req.Params.Arguments["name"].(string)
	if !ok {
		return protocol.NewToolResultError("参数 'name' 必须是字符串"), nil
	}
	greeting := "Hello, " + name + "! Welcome to MCP STDIO Server!"
	return &protocol.CallToolResult{
		Content: []protocol.Content{protocol.NewTextContent(greeting)},
	}, nil
}

func calculateHandler(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
	operation, ok := req.Params.Arguments["operation"].(string)
	if !ok {
		return protocol.NewToolResultError("参数 'operation' 必须是字符串"), nil
	}
	a, ok := req.Params.Arguments["a"].(float64)
	if !ok {
		return protocol.NewToolResultError("参数 'a' 必须是数字"), nil
	}
	b, ok := req.Params.Arguments["b"].(float64)
	if !ok {
		return protocol.NewToolResultError("参数 'b' 必须是数字"), nil
	}
	var result float64
	switch operation {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return protocol.NewToolResultError("除数不能为零"), nil
		}
		result = a / b
	default:
		return protocol.NewToolResultError("不支持的运算类型"), nil
	}
	return &protocol.CallToolResult{
		Content: []protocol.Content{protocol.NewTextContent(fmt.Sprintf("Result: %.2f", result))},
	}, nil
}

func main() {
	mcpServer := server.NewServer(&protocol.ServerInfo{Name: "StdioMCPServer", Version: "1.0.0"}, nil)
	mcpServer.AddTool(&protocol.Tool{
		Name: "greet", Description: "问候用户",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "用户名称"},
			},
			"required": []string{"name"},
		},
	}, greetHandler)
	mcpServer.AddTool(&protocol.Tool{
		Name: "calculate", Description: "执行基本数学运算",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{"type": "string", "description": "运算类型 (add, subtract, multiply, divide)"},
				"a":         map[string]interface{}{"type": "number", "description": "第一个数字"},
				"b":         map[string]interface{}{"type": "number", "description": "第二个数字"},
			},
			"required": []string{"operation", "a", "b"},
		},
	}, calculateHandler)
	mcpServer.AddResource(&protocol.Resource{
		URI: "info://server", Name: "server_info", Description: "服务器信息", MimeType: "text/plain",
	}, func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
		return &protocol.ReadResourceResult{
			Contents: []protocol.ResourceContents{{
				URI: "info://server", MimeType: "text/plain",
				Text: fmt.Sprintf("MCP STDIO Server v1.0.0\nProtocol: %s", protocol.MCPVersion),
			}},
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("收到中断信号,正在关闭服务器...")
		cancel()
	}()

	log.Println("启动 STDIO MCP 服务器")
	log.Printf("协议版本: MCP %s", protocol.MCPVersion)
	log.Println("等待客户端连接...")

	if err := mcpServer.Run(ctx, &stdio.StdioTransport{}); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}

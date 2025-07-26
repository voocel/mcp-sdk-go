package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

func main() {
	mcp := server.NewFastMCP("StdioMCPServer", "1.0.0")

	err := mcp.Tool("greet", "问候用户").
		WithStringParam("name", "用户名称", true).
		Handle(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			name := args["name"].(string)
			greeting := "Hello, " + name + "! Welcome to MCP STDIO Server!"
			return protocol.NewToolResultText(greeting), nil
		})

	if err != nil {
		log.Fatalf("注册工具失败: %v", err)
	}

	// 注册一个计算工具
	err = mcp.Tool("calculate", "执行基本数学运算").
		WithStringParam("operation", "运算类型 (add, subtract, multiply, divide)", true).
		WithIntParam("a", "第一个数字", true).
		WithIntParam("b", "第二个数字", true).
		Handle(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			operation := args["operation"].(string)
			a := int(args["a"].(float64))
			b := int(args["b"].(float64))

			var result int
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

			return protocol.NewToolResultText(
				"Result: " + string(rune(result)),
			), nil
		})

	if err != nil {
		log.Fatalf("注册计算工具失败: %v", err)
	}

	// 注册一个简单的资源
	err = mcp.SimpleTextResource("info://server", "server_info", "服务器信息",
		func(ctx context.Context) (string, error) {
			return "MCP STDIO Server v1.0.0\nProtocol: " + protocol.MCPVersion, nil
		})

	if err != nil {
		log.Fatalf("注册资源失败: %v", err)
	}

	// 创建 STDIO 传输服务器
	stdioServer := stdio.NewServer(mcp)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("收到中断信号，正在关闭服务器...")
		cancel()
	}()

	// 启动 STDIO 服务器
	log.Println("启动 STDIO MCP 服务器")
	log.Println("传输协议: STDIO")
	log.Println("协议版本: MCP", protocol.MCPVersion)
	log.Println("等待客户端连接...")

	if err := stdioServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}

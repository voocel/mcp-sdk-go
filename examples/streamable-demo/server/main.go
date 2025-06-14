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
	"github.com/voocel/mcp-sdk-go/transport/streamable"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 处理优雅关闭
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		log.Println("接收到关闭信号")
		cancel()
	}()

	// 创建FastMCP服务器
	mcp := server.NewFastMCP("Streamable HTTP 演示服务", "1.0.0")

	// 注册一个简单的问候工具
	mcp.Tool("greet", "问候用户").
		WithStringParam("name", "用户名称", true).
		WithStringParam("language", "语言（可选）", false).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			name, ok := args["name"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'name' 必须是字符串"), nil
			}

			language, _ := args["language"].(string)
			var greeting string

			switch language {
			case "zh", "中文":
				greeting = fmt.Sprintf("你好，%s！欢迎使用 Streamable HTTP 传输协议！", name)
			case "en", "english":
				greeting = fmt.Sprintf("Hello, %s! Welcome to Streamable HTTP transport!", name)
			default:
				greeting = fmt.Sprintf("Hello, %s! 你好！欢迎使用 Streamable HTTP 传输协议！", name)
			}

			return protocol.NewToolResultText(greeting), nil
		})

	// 注册一个数学计算工具
	mcp.Tool("calculate", "执行数学计算").
		WithStringParam("expression", "数学表达式", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			expr, ok := args["expression"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'expression' 必须是字符串"), nil
			}

			// 简单的计算示例（实际应用中应该使用安全的表达式解析器）
			result := fmt.Sprintf("计算结果：%s = [此处应该是计算结果]", expr)
			return protocol.NewToolResultText(result), nil
		})

	// 注册一个资源
	mcp.Resource("info://server", "服务器信息", "获取服务器基本信息").
		Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
			info := `# Streamable HTTP MCP 服务器

这是一个演示 Streamable HTTP 传输协议的 MCP 服务器。

## 特性

- 🌐 **单一端点**：所有通信通过一个 HTTP 端点
- 🔄 **动态升级**：根据需要自动升级到 SSE 流
- 📱 **会话管理**：支持有状态的会话
- 🔄 **可恢复连接**：支持连接中断后的恢复
- 🛡️ **安全防护**：内置 DNS rebinding 攻击防护

## 协议版本

- MCP 版本：2025-03-26
- 传输协议：Streamable HTTP

## 可用工具

1. **greet** - 多语言问候工具
2. **calculate** - 数学计算工具（演示用）

## 可用资源

- **info://server** - 服务器信息（本资源）
`
			contents := protocol.NewTextResourceContents("info://server", info)
			return protocol.NewReadResourceResult(contents), nil
		})

	// 注册一个提示模板
	mcp.Prompt("streamable_help", "Streamable HTTP 帮助信息").
		WithArgument("topic", "帮助主题", false).
		Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
			topic := args["topic"]
			if topic == "" {
				topic = "general"
			}

			var content string
			switch topic {
			case "transport":
				content = "Streamable HTTP 是 MCP 2025-03-26 规范中的新传输协议，它统一了 HTTP 和 SSE 的优势。"
			case "session":
				content = "会话管理允许服务器在多个请求之间保持状态，提供更好的用户体验。"
			case "security":
				content = "Streamable HTTP 包含多种安全机制，包括 Origin 验证和会话管理。"
			default:
				content = "Streamable HTTP 是一个现代化的 MCP 传输协议，提供了灵活的通信方式。"
			}

			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(content)),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					fmt.Sprintf("请告诉我更多关于 %s 的信息。", topic))),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					"我很乐意为您详细解释 Streamable HTTP 传输协议的相关内容。")),
			}
			return protocol.NewGetPromptResult("Streamable HTTP 帮助提示", messages...), nil
		})

	// 创建Streamable HTTP传输服务器
	streamableServer := streamable.NewServer(":8081", mcp)

	log.Println("🚀 启动 Streamable HTTP MCP 服务器")
	log.Println("📡 监听地址: http://localhost:8081")
	log.Println("🔗 协议版本: MCP 2025-03-26")
	log.Println("⚡ 传输协议: Streamable HTTP")

	if err := streamableServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("✅ 服务器已优雅关闭")
}

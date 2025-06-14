package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 创建使用Streamable HTTP传输的客户端
	mcpClient, err := client.New(
		client.WithStreamableHTTPTransport("http://localhost:8081"),
		client.WithClientInfo("streamable-demo-client", "1.0.0"),
	)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}
	defer mcpClient.Close()

	log.Println("🌐 正在连接到 Streamable HTTP MCP 服务器...")

	// 初始化连接
	initResult, err := mcpClient.Initialize(ctx, protocol.ClientInfo{
		Name:    "Streamable HTTP 演示客户端",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	fmt.Printf("✅ 连接成功！\n")
	fmt.Printf("📡 服务器: %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	fmt.Printf("🔗 协议版本: %s\n", initResult.ProtocolVersion)

	// 发送初始化完成通知
	if err := mcpClient.SendInitialized(ctx); err != nil {
		log.Printf("发送初始化完成通知失败: %v", err)
	}

	// 列出可用工具
	log.Println("\n🔧 获取可用工具...")
	tools, err := mcpClient.ListTools(ctx, "")
	if err != nil {
		log.Fatalf("获取工具列表失败: %v", err)
	}

	fmt.Printf("📋 发现 %d 个工具:\n", len(tools.Tools))
	for i, tool := range tools.Tools {
		fmt.Printf("  %d. %s - %s\n", i+1, tool.Name, tool.Description)
	}

	// 测试问候工具
	log.Println("\n👋 测试问候工具...")
	greetResult, err := mcpClient.CallTool(ctx, "greet", map[string]interface{}{
		"name":     "Go 开发者",
		"language": "zh",
	})
	if err != nil {
		log.Fatalf("调用问候工具失败: %v", err)
	}

	if len(greetResult.Content) > 0 {
		if textContent, ok := greetResult.Content[0].(protocol.TextContent); ok {
			fmt.Printf("🎉 %s\n", textContent.Text)
		}
	}

	// 测试计算工具
	log.Println("\n🧮 测试计算工具...")
	calcResult, err := mcpClient.CallTool(ctx, "calculate", map[string]interface{}{
		"expression": "2 + 3 * 4",
	})
	if err != nil {
		log.Fatalf("调用计算工具失败: %v", err)
	}

	if len(calcResult.Content) > 0 {
		if textContent, ok := calcResult.Content[0].(protocol.TextContent); ok {
			fmt.Printf("📊 %s\n", textContent.Text)
		}
	}

	// 列出可用资源
	log.Println("\n📁 获取可用资源...")
	resources, err := mcpClient.ListResources(ctx, "")
	if err != nil {
		log.Fatalf("获取资源列表失败: %v", err)
	}

	fmt.Printf("📚 发现 %d 个资源:\n", len(resources.Resources))
	for i, resource := range resources.Resources {
		fmt.Printf("  %d. %s - %s\n", i+1, resource.Name, resource.Description)
	}

	// 读取服务器信息资源
	log.Println("\n📖 读取服务器信息...")
	serverInfo, err := mcpClient.ReadResource(ctx, "info://server")
	if err != nil {
		log.Fatalf("读取服务器信息失败: %v", err)
	}

	if len(serverInfo.Contents) > 0 {
		fmt.Printf("ℹ️ 服务器信息:\n%s\n", serverInfo.Contents[0].Text)
	}

	// 列出可用提示模板
	log.Println("\n💬 获取可用提示模板...")
	prompts, err := mcpClient.ListPrompts(ctx, "")
	if err != nil {
		log.Fatalf("获取提示模板列表失败: %v", err)
	}

	fmt.Printf("🎯 发现 %d 个提示模板:\n", len(prompts.Prompts))
	for i, prompt := range prompts.Prompts {
		fmt.Printf("  %d. %s - %s\n", i+1, prompt.Name, prompt.Description)
	}

	// 获取帮助提示模板
	log.Println("\n🆘 获取帮助信息...")
	helpPrompt, err := mcpClient.GetPrompt(ctx, "streamable_help", map[string]string{
		"topic": "transport",
	})
	if err != nil {
		log.Fatalf("获取帮助提示失败: %v", err)
	}

	fmt.Printf("💡 帮助信息:\n")
	for i, message := range helpPrompt.Messages {
		fmt.Printf("  %d. [%s] %s\n", i+1, message.Role,
			message.Content.(protocol.TextContent).Text)
	}

	// 测试会话功能
	log.Println("\n🔄 测试多轮对话...")
	for i := 0; i < 3; i++ {
		result, err := mcpClient.CallTool(ctx, "greet", map[string]interface{}{
			"name":     fmt.Sprintf("用户-%d", i+1),
			"language": "en",
		})
		if err != nil {
			log.Printf("第 %d 轮对话失败: %v", i+1, err)
			continue
		}

		if len(result.Content) > 0 {
			if textContent, ok := result.Content[0].(protocol.TextContent); ok {
				fmt.Printf("  轮次 %d: %s\n", i+1, textContent.Text)
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	log.Println("\n✅ Streamable HTTP 传输协议演示完成！")
	log.Println("🎯 主要特性已验证:")
	log.Println("   ✓ 单一端点通信")
	log.Println("   ✓ 会话管理")
	log.Println("   ✓ 工具调用")
	log.Println("   ✓ 资源访问")
	log.Println("   ✓ 提示模板")
	log.Println("   ✓ 多轮对话")
}

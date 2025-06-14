package main

import (
	"context"
	"fmt"
	"log"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
	ctx := context.Background()

	// 创建SSE客户端连接
	mcpClient, err := client.New(
		client.WithSSETransport("http://localhost:8080"),
		client.WithClientInfo("example-client", "1.0.0"),
	)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}
	defer mcpClient.Close()

	// 执行MCP初始化握手
	fmt.Println("🔄 执行MCP初始化...")
	initResult, err := mcpClient.Initialize(ctx, protocol.ClientInfo{
		Name:    "MCP Go 客户端示例",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	fmt.Printf("✅ 初始化成功！服务器: %s v%s\n",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	fmt.Printf("📋 协议版本: %s\n", initResult.ProtocolVersion)

	// 发送初始化完成通知
	if err := mcpClient.SendInitialized(ctx); err != nil {
		log.Printf("发送初始化完成通知失败: %v", err)
	}

	// 演示工具相关功能
	fmt.Println("\n🔧 获取工具列表...")
	if err := demonstrateTools(ctx, mcpClient); err != nil {
		log.Printf("工具演示失败: %v", err)
	}

	// 演示资源相关功能
	fmt.Println("\n📁 获取资源列表...")
	if err := demonstrateResources(ctx, mcpClient); err != nil {
		log.Printf("资源演示失败: %v", err)
	}

	// 演示提示模板相关功能
	fmt.Println("\n💬 获取提示模板列表...")
	if err := demonstratePrompts(ctx, mcpClient); err != nil {
		log.Printf("提示模板演示失败: %v", err)
	}

	fmt.Println("\n✨ 客户端演示完成！")
}

func demonstrateTools(ctx context.Context, client client.Client) error {
	// 获取工具列表
	toolsResult, err := client.ListTools(ctx, "")
	if err != nil {
		return fmt.Errorf("获取工具列表失败: %w", err)
	}

	fmt.Printf("📊 找到 %d 个工具:\n", len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
	}

	// 如果有工具，尝试调用第一个
	if len(toolsResult.Tools) > 0 {
		toolName := toolsResult.Tools[0].Name
		fmt.Printf("\n⚡ 调用工具: %s\n", toolName)

		// 构造示例参数（根据实际工具调整）
		args := map[string]interface{}{
			"text": "Hello from MCP Go Client!",
		}

		result, err := client.CallTool(ctx, toolName, args)
		if err != nil {
			return fmt.Errorf("调用工具失败: %w", err)
		}

		fmt.Printf("📤 工具执行结果:\n")
		for i, content := range result.Content {
			if textContent, ok := content.(protocol.TextContent); ok {
				fmt.Printf("  %d. %s\n", i+1, textContent.Text)
			}
		}

		if result.IsError {
			fmt.Printf("⚠️  工具执行过程中发生错误\n")
		}
	}

	return nil
}

func demonstrateResources(ctx context.Context, client client.Client) error {
	// 获取资源列表
	resourcesResult, err := client.ListResources(ctx, "")
	if err != nil {
		return fmt.Errorf("获取资源列表失败: %w", err)
	}

	fmt.Printf("📊 找到 %d 个资源:\n", len(resourcesResult.Resources))
	for _, resource := range resourcesResult.Resources {
		fmt.Printf("  - %s (%s): %s\n", resource.Name, resource.URI, resource.Description)
	}

	// 如果有资源，尝试读取第一个
	if len(resourcesResult.Resources) > 0 {
		resourceURI := resourcesResult.Resources[0].URI
		fmt.Printf("\n📖 读取资源: %s\n", resourceURI)

		result, err := client.ReadResource(ctx, resourceURI)
		if err != nil {
			return fmt.Errorf("读取资源失败: %w", err)
		}

		fmt.Printf("📄 资源内容:\n")
		for i, content := range result.Contents {
			fmt.Printf("  %d. URI: %s\n", i+1, content.URI)
			fmt.Printf("      类型: %s\n", content.MimeType)
			if content.Text != "" {
				// 限制输出长度
				text := content.Text
				if len(text) > 200 {
					text = text[:200] + "..."
				}
				fmt.Printf("      内容: %s\n", text)
			}
		}
	}

	return nil
}

func demonstratePrompts(ctx context.Context, client client.Client) error {
	// 获取提示模板列表
	promptsResult, err := client.ListPrompts(ctx, "")
	if err != nil {
		return fmt.Errorf("获取提示模板列表失败: %w", err)
	}

	fmt.Printf("📊 找到 %d 个提示模板:\n", len(promptsResult.Prompts))
	for _, prompt := range promptsResult.Prompts {
		fmt.Printf("  - %s: %s\n", prompt.Name, prompt.Description)
		if len(prompt.Arguments) > 0 {
			fmt.Printf("    参数: ")
			for i, arg := range prompt.Arguments {
				if i > 0 {
					fmt.Printf(", ")
				}
				fmt.Printf("%s", arg.Name)
			}
			fmt.Printf("\n")
		}
	}

	// 如果有提示模板，尝试获取第一个
	if len(promptsResult.Prompts) > 0 {
		promptName := promptsResult.Prompts[0].Name
		fmt.Printf("\n💭 获取提示模板: %s\n", promptName)

		// 构造示例参数（根据实际提示模板调整）
		args := map[string]string{
			"topic":   "Go 编程",
			"context": "MCP SDK 开发",
		}

		result, err := client.GetPrompt(ctx, promptName, args)
		if err != nil {
			return fmt.Errorf("获取提示模板失败: %w", err)
		}

		fmt.Printf("📝 提示模板内容:\n")
		if result.Description != "" {
			fmt.Printf("  描述: %s\n", result.Description)
		}
		fmt.Printf("  消息数: %d\n", len(result.Messages))

		for i, message := range result.Messages {
			fmt.Printf("    %d. 角色: %s\n", i+1, message.Role)
			if textContent, ok := message.Content.(protocol.TextContent); ok {
				// 限制输出长度
				text := textContent.Text
				if len(text) > 100 {
					text = text[:100] + "..."
				}
				fmt.Printf("       内容: %s\n", text)
			}
		}
	}

	return nil
}

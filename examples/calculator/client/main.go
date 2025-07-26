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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 创建 STDIO 客户端连接到计算器服务, 这里假设服务器作为子进程启动
	mcpClient, err := client.New(
		client.WithStdioTransport("go", []string{"run", "../server/main.go"}),
		client.WithClientInfo("calculator-client", "1.0.0"),
	)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}
	defer mcpClient.Close()

	// 初始化握手
	fmt.Println("连接到计算器服务...")
	initResult, err := mcpClient.Initialize(ctx, protocol.ClientInfo{
		Name:    "计算器客户端",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	fmt.Printf("连接成功！服务器: %s v%s\n",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	// 发送初始化完成通知
	if err := mcpClient.SendInitialized(ctx); err != nil {
		log.Printf("发送初始化完成通知失败: %v", err)
	}

	// 获取工具列表
	toolsResult, err := mcpClient.ListTools(ctx, "")
	if err != nil {
		log.Fatalf("获取工具列表失败: %v", err)
	}

	fmt.Println("\n可用工具:")
	for _, tool := range toolsResult.Tools {
		fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
	}
	fmt.Println()

	// 测试加法
	fmt.Println("测试计算功能:")
	result, err := mcpClient.CallTool(ctx, "add", map[string]any{
		"a": 5.0,
		"b": 3.0,
	})
	if err != nil {
		log.Fatalf("调用 add 工具失败: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  5 + 3 = %s\n", textContent.Text)
		}
	}

	// 测试减法
	result, err = mcpClient.CallTool(ctx, "subtract", map[string]any{
		"a": 10.0,
		"b": 4.0,
	})
	if err != nil {
		log.Fatalf("调用 subtract 工具失败: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  10 - 4 = %s\n", textContent.Text)
		}
	}

	// 测试乘法
	result, err = mcpClient.CallTool(ctx, "multiply", map[string]any{
		"a": 6.0,
		"b": 7.0,
	})
	if err != nil {
		log.Fatalf("调用 multiply 工具失败: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  6 * 7 = %s\n", textContent.Text)
		}
	}

	// 测试除法
	result, err = mcpClient.CallTool(ctx, "divide", map[string]any{
		"a": 20.0,
		"b": 5.0,
	})
	if err != nil {
		log.Fatalf("调用 divide 工具失败: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  20 / 5 = %s\n", textContent.Text)
		}
	}

	// 测试除零错误
	result, err = mcpClient.CallTool(ctx, "divide", map[string]any{
		"a": 20.0,
		"b": 0.0,
	})
	if err != nil {
		fmt.Printf("  除零错误: %v\n", err)
	} else if result.IsError && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("  除零错误: %s\n", textContent.Text)
		}
	}

	// 获取帮助提示模板
	fmt.Println("\n获取帮助信息:")
	promptResult, err := mcpClient.GetPrompt(ctx, "calculator_help", nil)
	if err != nil {
		log.Fatalf("获取提示模板失败: %v", err)
	}

	fmt.Printf("  描述: %s\n", promptResult.Description)
	fmt.Println("  对话示例:")
	for i, msg := range promptResult.Messages {
		if textContent, ok := msg.Content.(protocol.TextContent); ok {
			fmt.Printf("    %d. [%s]: %s\n", i+1, msg.Role, textContent.Text)
		}
	}

	fmt.Println("\n end!")
}

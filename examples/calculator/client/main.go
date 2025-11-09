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

	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "calculator-client",
		Version: "1.0.0",
	}, nil)

	// 创建 CommandTransport 连接到计算器服务
	transport := client.NewCommandTransport("go", "run", "../server/main.go")

	fmt.Println("连接到计算器服务...")
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer session.Close()

	initResult := session.InitializeResult()
	fmt.Printf("连接成功！服务器: %s v%s\n",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	toolsResult, err := session.ListTools(ctx, nil)
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
	result, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "add",
		Arguments: map[string]any{
			"a": 5.0,
			"b": 3.0,
		},
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
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "subtract",
		Arguments: map[string]any{
			"a": 10.0,
			"b": 4.0,
		},
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
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "multiply",
		Arguments: map[string]any{
			"a": 6.0,
			"b": 7.0,
		},
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
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "divide",
		Arguments: map[string]any{
			"a": 20.0,
			"b": 5.0,
		},
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
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "divide",
		Arguments: map[string]any{
			"a": 20.0,
			"b": 0.0,
		},
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
	promptResult, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
		Name:      "calculator_help",
		Arguments: map[string]string{},
	})
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

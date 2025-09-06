package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
	ctx := context.Background()

	// 创建客户端，设置 Sampling 处理器
	mcpClient, err := client.New(
		client.WithSSETransport("http://localhost:8080"),
		client.WithClientInfo("sampling-demo-client", "1.0.0"),
		client.WithSamplingHandler(handleSampling),
	)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}
	defer mcpClient.Close()

	initResult, err := mcpClient.Initialize(ctx, protocol.ClientInfo{
		Name:    "Sampling Demo Client",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	fmt.Printf("连接到服务器: %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	fmt.Printf("协议版本: %s\n", initResult.ProtocolVersion)

	// 发送初始化完成通知
	if err := mcpClient.SendInitialized(ctx); err != nil {
		log.Fatalf("发送初始化完成通知失败: %v", err)
	}

	// 获取可用工具
	tools, err := mcpClient.ListTools(ctx, "")
	if err != nil {
		log.Fatalf("获取工具列表失败: %v", err)
	}

	fmt.Println("\n可用工具:")
	for _, tool := range tools.Tools {
		fmt.Printf("- %s: %s\n", tool.Name, tool.Description)
	}

	// 交互式测试
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("\n=== Sampling Demo 客户端 ===")
	fmt.Println("输入命令来测试 Sampling 功能:")
	fmt.Println("1. calc <表达式>     - 使用AI计算器")
	fmt.Println("2. chat <消息>       - 与AI对话")
	fmt.Println("3. conv <消息>       - 高级AI对话")
	fmt.Println("4. quit             - 退出")
	fmt.Println()

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		parts := strings.SplitN(input, " ", 2)
		command := parts[0]

		switch command {
		case "quit", "exit":
			fmt.Println("再见!")
			return

		case "calc":
			if len(parts) < 2 {
				fmt.Println("请提供数学表达式，例如: calc 2 + 3")
				continue
			}
			testAICalculator(ctx, mcpClient, parts[1])

		case "chat":
			if len(parts) < 2 {
				fmt.Println("请提供消息，例如: chat 你好")
				continue
			}
			testAIChat(ctx, mcpClient, parts[1])

		case "conv":
			if len(parts) < 2 {
				fmt.Println("请提供消息，例如: conv 什么是MCP?")
				continue
			}
			testAIConversation(ctx, mcpClient, parts[1])

		default:
			fmt.Printf("未知命令: %s\n", command)
			fmt.Println("可用命令: calc, chat, conv, quit")
		}
	}
}

// handleSampling 处理服务器的 Sampling 请求
func handleSampling(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
	fmt.Printf("\n收到 Sampling 请求:\n")
	fmt.Printf("   最大令牌数: %d\n", request.MaxTokens)
	if request.SystemPrompt != "" {
		fmt.Printf("   系统提示: %s\n", request.SystemPrompt)
	}
	if request.Temperature != nil {
		fmt.Printf("   温度: %.1f\n", *request.Temperature)
	}
	if request.ModelPreferences != nil {
		fmt.Printf("   模型偏好: %+v\n", request.ModelPreferences)
	}

	fmt.Printf("   消息历史:\n")
	for i, msg := range request.Messages {
		if textContent, ok := msg.Content.(protocol.TextContent); ok {
			fmt.Printf("     %d. [%s] %s\n", i+1, msg.Role, textContent.Text)
		}
	}

	// 模拟用户确认
	fmt.Print("\n是否允许此 Sampling 请求? (y/n): ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		response := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if response != "y" && response != "yes" {
			return nil, fmt.Errorf("用户拒绝了 Sampling 请求")
		}
	}

	// 模拟AI响应（在实际应用中，这里会调用真实的LLM API）
	fmt.Println("正在生成AI响应...")

	// 基于最后一条用户消息生成响应
	var userMessage string
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == protocol.RoleUser {
			if textContent, ok := request.Messages[i].Content.(protocol.TextContent); ok {
				userMessage = textContent.Text
				break
			}
		}
	}

	var aiResponse string
	switch {
	case strings.Contains(userMessage, "计算") || strings.Contains(userMessage, "+") || strings.Contains(userMessage, "-") || strings.Contains(userMessage, "*") || strings.Contains(userMessage, "/"):
		aiResponse = "根据你的计算请求，我来帮你计算。不过作为演示，这里返回一个模拟结果。"
	case strings.Contains(userMessage, "MCP"):
		aiResponse = "MCP (Model Context Protocol) 是一个开放标准，用于AI应用与外部数据源和工具的安全集成。它支持工具调用、资源访问、提示模板和用户交互等功能。"
	case strings.Contains(userMessage, "你好") || strings.Contains(userMessage, "hello"):
		aiResponse = "你好！我是一个演示用的AI助手。我可以帮助你测试MCP的Sampling功能。"
	default:
		aiResponse = fmt.Sprintf("我收到了你的消息：\"%s\"。这是一个模拟的AI响应，用于演示MCP Sampling功能的工作原理。", userMessage)
	}

	result := protocol.NewCreateMessageResult(
		protocol.RoleAssistant,
		protocol.NewTextContent(aiResponse),
		"demo-gpt-4",
		protocol.StopReasonEndTurn,
	)

	fmt.Printf("AI响应已生成: %s\n\n", aiResponse)
	return result, nil
}

// testAICalculator 测试AI计算器工具
func testAICalculator(ctx context.Context, client client.Client, expression string) {
	fmt.Printf("调用AI计算器: %s\n", expression)

	result, err := client.CallTool(ctx, "ai_calculator", map[string]interface{}{
		"expression": expression,
	})
	if err != nil {
		fmt.Printf("调用失败: %v\n", err)
		return
	}

	if result.IsError {
		fmt.Printf("工具错误: %s\n", getTextFromContent(result.Content))
	} else {
		fmt.Printf("计算结果: %s\n", getTextFromContent(result.Content))
	}
}

// testAIChat 测试AI对话工具
func testAIChat(ctx context.Context, client client.Client, message string) {
	fmt.Printf("💬 与AI对话: %s\n", message)

	result, err := client.CallTool(ctx, "ai_chat", map[string]interface{}{
		"message": message,
	})
	if err != nil {
		fmt.Printf("调用失败: %v\n", err)
		return
	}

	if result.IsError {
		fmt.Printf("工具错误: %s\n", getTextFromContent(result.Content))
	} else {
		fmt.Printf("AI回复: %s\n", getTextFromContent(result.Content))
	}
}

// testAIConversation 测试高级AI对话工具
func testAIConversation(ctx context.Context, client client.Client, message string) {
	fmt.Printf("高级AI对话: %s\n", message)

	result, err := client.CallTool(ctx, "ai_conversation", map[string]interface{}{
		"user_message": message,
		"context":      "这是一个关于MCP协议的技术讨论。",
	})
	if err != nil {
		fmt.Printf("调用失败: %v\n", err)
		return
	}

	if result.IsError {
		fmt.Printf("工具错误: %s\n", getTextFromContent(result.Content))
	} else {
		fmt.Printf("AI回复: %s\n", getTextFromContent(result.Content))
	}
}

// getTextFromContent 从内容中提取文本
func getTextFromContent(content []protocol.Content) string {
	if len(content) == 0 {
		return ""
	}

	if textContent, ok := content[0].(protocol.TextContent); ok {
		return textContent.Text
	}

	return "无法解析内容"
}

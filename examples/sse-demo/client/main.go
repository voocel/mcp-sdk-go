package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 创建SSE客户端
	fmt.Println("连接到SSE服务器...")
	mcpClient, err := client.New(
		client.WithSSETransport("http://localhost:8081"),
		client.WithClientInfo("sse-demo-client", "1.0.0"),
	)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}
	defer mcpClient.Close()

	// 初始化连接
	initResult, err := mcpClient.Initialize(ctx, protocol.ClientInfo{
		Name:    "SSE客户端",
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

	// 获取服务器信息
	fmt.Println("\n获取服务器信息...")
	resource, err := mcpClient.ReadResource(ctx, "info://server")
	if err != nil {
		log.Printf("读取服务器信息失败: %v", err)
	} else if len(resource.Contents) > 0 {
		fmt.Printf("%s\n", resource.Contents[0].Text)
	}

	// 列出可用工具
	fmt.Println("\n 获取可用工具...")
	toolsResult, err := mcpClient.ListTools(ctx, "")
	if err != nil {
		log.Printf("获取工具列表失败: %v", err)
	} else {
		fmt.Printf("可用工具 (%d个):\n", len(toolsResult.Tools))
		for _, tool := range toolsResult.Tools {
			fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
		}
	}

	// echo工具
	fmt.Println("\n测试echo工具:")
	result, err := mcpClient.CallTool(ctx, "echo", map[string]any{
		"message": "Hello, SSE Demo!",
	})
	if err != nil {
		fmt.Printf("调用echo工具失败: %v\n", err)
	} else if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("   %s\n", textContent.Text)
		}
	}

	// 随机数工具
	fmt.Println("\n测试随机数工具:")
	result, err = mcpClient.CallTool(ctx, "random", map[string]any{
		"min": 1,
		"max": 100,
	})
	if err != nil {
		fmt.Printf("调用随机数工具失败: %v\n", err)
	} else if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("   %s\n", textContent.Text)
		}
	}

	// 时间工具
	fmt.Println("\n测试时间工具:")
	timeFormats := []string{"readable", "iso", "unix"}
	for _, format := range timeFormats {
		result, err = mcpClient.CallTool(ctx, "time", map[string]any{
			"format": format,
		})
		if err != nil {
			fmt.Printf("调用时间工具失败: %v\n", err)
		} else if len(result.Content) > 0 {
			if textContent, ok := result.Content[0].(protocol.TextContent); ok {
				fmt.Printf("   %s格式: %s\n", format, textContent.Text)
			}
		}
	}

	// 文本转换工具
	fmt.Println("\n测试文本转换工具:")
	testText := "Hello World"
	operations := []string{"upper", "lower", "reverse", "length"}
	for _, op := range operations {
		result, err = mcpClient.CallTool(ctx, "text_transform", map[string]any{
			"text":      testText,
			"operation": op,
		})
		if err != nil {
			fmt.Printf("调用文本转换工具失败: %v\n", err)
		} else if len(result.Content) > 0 {
			if textContent, ok := result.Content[0].(protocol.TextContent); ok {
				fmt.Printf("   %s\n", textContent.Text)
			}
		}
	}

	// 获取健康状态
	fmt.Println("\n获取服务器健康状态:")
	healthResource, err := mcpClient.ReadResource(ctx, "status://health")
	if err != nil {
		fmt.Printf("读取健康状态失败: %v\n", err)
	} else if len(healthResource.Contents) > 0 {
		fmt.Printf("%s\n", healthResource.Contents[0].Text)
	}

	// 获取使用指南提示
	fmt.Println("\n获取使用指南:")
	promptResult, err := mcpClient.GetPrompt(ctx, "usage_guide", map[string]string{
		"tool_name": "echo",
	})
	if err != nil {
		fmt.Printf("获取提示失败: %v\n", err)
	} else {
		fmt.Printf("提示描述: %s\n", promptResult.Description)
		if len(promptResult.Messages) > 0 {
			for i, msg := range promptResult.Messages {
				if textContent, ok := msg.Content.(protocol.TextContent); ok {
					fmt.Printf("   消息%d (%s): %s\n", i+1, msg.Role, textContent.Text)
				}
			}
		}
	}

	// 交互式模式
	fmt.Println("\n进入交互式模式 (输入 'help' 查看命令，'exit' 退出):")
	scanner := bufio.NewScanner(os.Stdin)
	
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		
		if input == "exit" || input == "quit" {
			break
		}
		
		if input == "help" {
			showHelp()
			continue
		}
		
		handleInteractiveCommand(ctx, mcpClient, input)
	}

	fmt.Println("\n使用SSE客户端完成！")
}

func showHelp() {
	fmt.Println(`
  可用命令:
  echo <message>              - 回声工具
  random <min> <max>          - 生成随机数
  time [format]               - 获取当前时间 (format: readable/iso/unix)
  transform <text> <op>       - 文本转换 (op: upper/lower/reverse/length)
  info                        - 服务器信息
  health                      - 健康状态
  tools                       - 列出工具
  help                        - 显示帮助
  exit                        - 退出程序`)
}

func handleInteractiveCommand(ctx context.Context, client client.Client, input string) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}
	
	command := parts[0]
	
	switch command {
	case "echo":
		if len(parts) < 2 {
			fmt.Println("用法: echo <message>")
			return
		}
		message := strings.Join(parts[1:], " ")
		callTool(ctx, client, "echo", map[string]any{"message": message})
		
	case "random":
		if len(parts) < 3 {
			fmt.Println("用法: random <min> <max>")
			return
		}
		min, err1 := strconv.Atoi(parts[1])
		max, err2 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil {
			fmt.Println("min和max必须是整数")
			return
		}
		callTool(ctx, client, "random", map[string]any{"min": min, "max": max})
		
	case "time":
		format := "readable"
		if len(parts) > 1 {
			format = parts[1]
		}
		callTool(ctx, client, "time", map[string]any{"format": format})
		
	case "transform":
		if len(parts) < 3 {
			fmt.Println("用法: transform <text> <operation>")
			return
		}
		text := parts[1]
		operation := parts[2]
		callTool(ctx, client, "text_transform", map[string]any{
			"text": text, "operation": operation})
		
	case "info":
		readResource(ctx, client, "info://server")
		
	case "health":
		readResource(ctx, client, "status://health")
		
	case "tools":
		listTools(ctx, client)
		
	default:
		fmt.Printf("未知命令: %s (输入 'help' 查看帮助)\n", command)
	}
}

func callTool(ctx context.Context, client client.Client, name string, args map[string]any) {
	result, err := client.CallTool(ctx, name, args)
	if err != nil {
		fmt.Printf("调用工具失败: %v\n", err)
		return
	}
	
	if result.IsError && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("%s\n", textContent.Text)
		}
	} else if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("%s\n", textContent.Text)
		}
	}
}

func readResource(ctx context.Context, client client.Client, uri string) {
	resource, err := client.ReadResource(ctx, uri)
	if err != nil {
		fmt.Printf("读取资源失败: %v\n", err)
		return
	}
	
	if len(resource.Contents) > 0 {
		fmt.Printf("%s\n", resource.Contents[0].Text)
	}
}

func listTools(ctx context.Context, client client.Client) {
	toolsResult, err := client.ListTools(ctx, "")
	if err != nil {
		fmt.Printf("获取工具列表失败: %v\n", err)
		return
	}
	
	fmt.Printf("可用工具 (%d个):\n", len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
	}
}

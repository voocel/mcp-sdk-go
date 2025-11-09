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
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	ctx := context.Background()
	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "chatbot-client",
		Version: "1.0.0",
	}, nil)

	transport, err := sse.NewSSETransport("http://localhost:8082")
	if err != nil {
		log.Fatalf("创建 Transport 失败: %v", err)
	}

	fmt.Println("连接到聊天机器人服务...")
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer session.Close()

	initResult := session.InitializeResult()
	fmt.Printf("连接成功！服务器: %s v%s\n",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	fmt.Print("\n请输入你的姓名: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	username := strings.TrimSpace(scanner.Text())
	if username == "" {
		username = "朋友"
	}

	// 获取问候语
	result, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "greeting",
		Arguments: map[string]any{
			"name": username,
		},
	})
	if err != nil {
		log.Fatalf("调用问候工具失败: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Println("\n" + textContent.Text)
		}
	}

	// 获取聊天模板
	promptResult, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
		Name: "chat_template",
		Arguments: map[string]string{
			"username": username,
		},
	})
	if err != nil {
		log.Fatalf("获取聊天模板失败: %v", err)
	}

	// 显示助手的欢迎消息
	if len(promptResult.Messages) >= 3 {
		assistantMessage := promptResult.Messages[len(promptResult.Messages)-1]
		if textContent, ok := assistantMessage.Content.(protocol.TextContent); ok {
			fmt.Println(textContent.Text)
		}
	}

	fmt.Println("\n你可以输入以下命令:")
	fmt.Println("- 'weather [城市]' 查看天气")
	fmt.Println("- 'translate [文本] to [zh/en]' 翻译文本")
	fmt.Println("- 'exit' 退出聊天")
	fmt.Println()

	for {
		fmt.Print("> ")
		scanner.Scan()
		input := strings.TrimSpace(scanner.Text())

		if input == "exit" || input == "退出" {
			break
		}

		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "weather ") || strings.HasPrefix(input, "天气 ") {
			// 处理天气查询
			var city string
			if strings.HasPrefix(input, "weather ") {
				city = strings.TrimPrefix(input, "weather ")
			} else {
				city = strings.TrimPrefix(input, "天气 ")
			}

			city = strings.TrimSpace(city)
			if city == "" {
				fmt.Println("请指定城市名称")
				continue
			}

			result, err := session.CallTool(ctx, &protocol.CallToolParams{
				Name: "weather",
				Arguments: map[string]any{
					"city": city,
				},
			})
			if err != nil {
				fmt.Printf("错误: %v\n", err)
				continue
			}

			if len(result.Content) > 0 {
				if textContent, ok := result.Content[0].(protocol.TextContent); ok {
					fmt.Printf(" %s\n", textContent.Text)
				}
			}
		} else if strings.Contains(input, " to ") {
			// 处理翻译请求
			parts := strings.Split(input, " to ")
			if len(parts) != 2 || !strings.HasPrefix(parts[0], "translate ") {
				fmt.Println("格式错误。请使用: translate [文本] to [zh/en]")
				continue
			}

			text := strings.TrimSpace(strings.TrimPrefix(parts[0], "translate "))
			targetLang := strings.TrimSpace(parts[1])

			if text == "" {
				fmt.Println("请提供要翻译的文本")
				continue
			}

			if targetLang != "zh" && targetLang != "en" {
				fmt.Println("目标语言必须是 'zh' 或 'en'")
				continue
			}

			result, err := session.CallTool(ctx, &protocol.CallToolParams{
				Name: "translate",
				Arguments: map[string]any{
					"text":        text,
					"target_lang": targetLang,
				},
			})
			if err != nil {
				fmt.Printf("错误: %v\n", err)
				continue
			}

			if result.IsError && len(result.Content) > 0 {
				if textContent, ok := result.Content[0].(protocol.TextContent); ok {
					fmt.Printf("%s\n", textContent.Text)
				}
			} else if len(result.Content) > 0 {
				if textContent, ok := result.Content[0].(protocol.TextContent); ok {
					fmt.Printf("翻译结果: %s\n", textContent.Text)
				}
			}
		} else if input == "help" || input == "帮助" {
			// 显示帮助信息
			fmt.Println("可用命令:")
			fmt.Println("  - weather [城市] 或 天气 [城市] - 查看指定城市的天气")
			fmt.Println("  - translate [文本] to [zh/en] - 翻译中英文")
			fmt.Println("  - help 或 帮助 - 显示此帮助信息")
			fmt.Println("  - exit 或 退出 - 退出程序")
		} else {
			// 未识别的命令
			fmt.Printf("未知命令: '%s'\n", input)
			fmt.Println("请尝试 'weather [城市]'、'translate [文本] to [zh/en]'、'help' 或 'exit'")
		}
	}

	fmt.Println("\n end!")
}

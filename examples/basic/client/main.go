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
	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "BasicMCPClient",
		Version: "1.0.0",
	}, &client.ClientOptions{
		ElicitationHandler:   handleElicitation,
		CreateMessageHandler: handleSampling,
	})

	transport := client.NewCommandTransport("go", "run", "../main.go")

	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer session.Close()

	initResult := session.InitializeResult()

	log.Println("========================================")
	log.Println("MCP Basic Client 已连接")
	log.Println("========================================")
	log.Printf("服务器: %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	log.Println("")

	demonstrateAllFeatures(ctx, session)
}

func demonstrateAllFeatures(ctx context.Context, session *client.ClientSession) {
	fmt.Println("\n========== 列出所有工具 ==========")
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Printf("列出工具失败: %v", err)
	} else {
		for _, tool := range tools.Tools {
			fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
			if tool.Title != "" {
				fmt.Printf("    标题: %s\n", tool.Title)
			}
			if len(tool.Meta) > 0 {
				fmt.Printf("    元数据: %v\n", tool.Meta)
			}
		}
	}

	fmt.Println("\n========== 调用基础工具 (greet) ==========")
	result, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "greet",
		Arguments: map[string]interface{}{
			"name": "Alice",
		},
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== 调用带元数据的工具 (calculate) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "add",
			"a":         10,
			"b":         20,
		},
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== 调用带输出 Schema 的工具 (get_time) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name:      "get_time",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== 列出所有资源 ==========")
	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		log.Printf("列出资源失败: %v", err)
	} else {
		for _, resource := range resources.Resources {
			fmt.Printf("  - %s (%s): %s\n", resource.Name, resource.URI, resource.Description)
			if len(resource.Meta) > 0 {
				fmt.Printf("    元数据: %v\n", resource.Meta)
			}
		}
	}

	fmt.Println("\n========== 读取资源 (info://server) ==========")
	resourceResult, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
		URI: "info://server",
	})
	if err != nil {
		log.Printf("读取资源失败: %v", err)
	} else {
		for _, content := range resourceResult.Contents {
			fmt.Printf("内容: %s\n", content.Text)
		}
	}

	fmt.Println("\n========== 列出资源模板 ==========")
	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil {
		log.Printf("列出资源模板失败: %v", err)
	} else {
		for _, template := range templates.Resources {
			fmt.Printf("  - %s (%s): %s\n", template.Name, template.URI, template.Description)
			if len(template.Meta) > 0 {
				fmt.Printf("    元数据: %v\n", template.Meta)
			}
		}
	}

	fmt.Println("\n========== 列出提示模板 ==========")
	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		log.Printf("列出提示失败: %v", err)
	} else {
		for _, prompt := range prompts.Prompts {
			fmt.Printf("  - %s: %s\n", prompt.Name, prompt.Description)
			if prompt.Title != "" {
				fmt.Printf("    标题: %s\n", prompt.Title)
			}
			if len(prompt.Meta) > 0 {
				fmt.Printf("    元数据: %v\n", prompt.Meta)
			}
		}
	}

	fmt.Println("\n========== 获取提示 (code_review) ==========")
	promptResult, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
		Name: "code_review",
		Arguments: map[string]string{
			"language": "go",
			"code":     "func main() { fmt.Println(\"hello\") }",
		},
	})
	if err != nil {
		log.Printf("获取提示失败: %v", err)
	} else {
		fmt.Printf("描述: %s\n", promptResult.Description)
		for i, msg := range promptResult.Messages {
			fmt.Printf("消息 %d (%s): %v\n", i+1, msg.Role, msg.Content)
		}
		if len(promptResult.Meta) > 0 {
			fmt.Printf("元数据: %v\n", promptResult.Meta)
		}
	}

	fmt.Println("\n========== 调用交互式工具 (interactive_greet) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name:      "interactive_greet",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== 调用 AI 工具 (ai_assistant) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "ai_assistant",
		Arguments: map[string]interface{}{
			"question": "什么是 MCP 协议?",
		},
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== 调用资源链接工具 (find_file) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "find_file",
		Arguments: map[string]interface{}{
			"filename": "main.go",
		},
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
		for i, content := range result.Content {
			if rlc, ok := content.(protocol.ResourceLinkContent); ok {
				fmt.Printf("\n  资源链接 %d:\n", i+1)
				fmt.Printf("    URI: %s\n", rlc.URI)
				fmt.Printf("    名称: %s\n", rlc.Name)
				fmt.Printf("    描述: %s\n", rlc.Description)
				fmt.Printf("    MIME类型: %s\n", rlc.MimeType)
				if rlc.Annotations != nil {
					fmt.Printf("    注解:\n")
					if len(rlc.Annotations.Audience) > 0 {
						fmt.Printf("      受众: %v\n", rlc.Annotations.Audience)
					}
					if rlc.Annotations.Priority > 0 {
						fmt.Printf("      优先级: %.1f\n", rlc.Annotations.Priority)
					}
				}
			}
		}
	}

	fmt.Println("\n=================== END =====================")
}

// handleElicitation 处理服务器的用户交互请求
func handleElicitation(ctx context.Context, params *protocol.ElicitationCreateParams) (*protocol.ElicitationResult, error) {
	fmt.Printf("\n[Elicitation] 服务器请求: %s\n", params.Message)

	// 从标准输入读取用户输入
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("请输入: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return protocol.NewElicitationDecline(), nil
	}

	input = strings.TrimSpace(input)

	return protocol.NewElicitationAccept(map[string]interface{}{
		"name": input,
	}), nil
}

// handleSampling 处理服务器的 LLM 推理请求
func handleSampling(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
	fmt.Printf("\n[Sampling] 服务器请求 LLM 推理\n")
	fmt.Printf("  MaxTokens: %d\n", request.MaxTokens)
	fmt.Printf("  Messages: %d 条\n", len(request.Messages))

	// 提取用户消息
	var userMessage string
	for _, msg := range request.Messages {
		if msg.Role == protocol.RoleUser {
			if textContent, ok := msg.Content.(protocol.TextContent); ok {
				userMessage = textContent.Text
				fmt.Printf("  用户消息: %s\n", userMessage)
			}
		}
	}

	// 模拟 AI 响应
	response := fmt.Sprintf("这是一个模拟的 AI 响应。问题是: %s\n\nMCP (Model Context Protocol) 是一个开放协议,用于连接 AI 应用与外部数据源和工具。", userMessage)

	return protocol.NewCreateMessageResult(
		protocol.RoleAssistant,
		protocol.NewTextContent(response),
		"mock-llm-v1",
		protocol.StopReasonEndTurn,
	), nil
}

// printToolResult 打印工具调用结果
func printToolResult(result *protocol.CallToolResult) {
	for _, content := range result.Content {
		if textContent, ok := content.(protocol.TextContent); ok {
			fmt.Printf("结果: %s\n", textContent.Text)
		}
	}
	if len(result.Meta) > 0 {
		fmt.Printf("元数据: %v\n", result.Meta)
	}
	if result.StructuredContent != nil {
		fmt.Printf("结构化内容: %v\n", result.StructuredContent)
	}
}

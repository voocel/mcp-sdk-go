package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
	ctx := context.Background()

	// 创建客户端,配置 Elicitation 和 Sampling 处理器
	mcpClient, err := client.New(
		client.WithStdioTransport("go", []string{"run", "../main.go"}),
		client.WithClientInfo("BasicMCPClient", "1.0.0"),
		client.WithElicitationHandler(handleElicitation),
		client.WithSamplingHandler(handleSampling),
	)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}
	defer mcpClient.Close()

	initResult, err := mcpClient.Initialize(ctx, protocol.ClientInfo{
		Name:    "BasicMCPClient",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	if err := mcpClient.SendInitialized(ctx); err != nil {
		log.Fatalf("发送 initialized 失败: %v", err)
	}

	log.Println("========================================")
	log.Println("MCP Basic Client 已连接")
	log.Println("========================================")
	log.Printf("服务器: %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	log.Println("")

	// 演示所有功能
	demonstrateAllFeatures(ctx, mcpClient)
}

func demonstrateAllFeatures(ctx context.Context, mcpClient client.Client) {
	// 列出所有工具
	fmt.Println("\n========== 1. 列出所有工具 ==========")
	tools, err := mcpClient.ListTools(ctx, "")
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

	// 调用基础工具
	fmt.Println("\n========== 2. 调用基础工具 (greet) ==========")
	result, err := mcpClient.CallTool(ctx, "greet", map[string]interface{}{
		"name": "Alice",
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	// 调用带元数据的工具
	fmt.Println("\n========== 3. 调用带元数据的工具 (calculate) ==========")
	result, err = mcpClient.CallTool(ctx, "calculate", map[string]interface{}{
		"operation": "add",
		"a":         10,
		"b":         20,
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	// 调用带输出 Schema 的工具
	fmt.Println("\n========== 4. 调用带输出 Schema 的工具 (get_time) ==========")
	result, err = mcpClient.CallTool(ctx, "get_time", map[string]interface{}{})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	// 列出所有资源
	fmt.Println("\n========== 5. 列出所有资源 ==========")
	resources, err := mcpClient.ListResources(ctx, "")
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

	// 读取资源
	fmt.Println("\n========== 6. 读取资源 (info://server) ==========")
	resourceResult, err := mcpClient.ReadResource(ctx, "info://server")
	if err != nil {
		log.Printf("读取资源失败: %v", err)
	} else {
		for _, content := range resourceResult.Contents {
			fmt.Printf("内容: %s\n", content.Text)
		}
	}

	// 列出资源模板
	fmt.Println("\n========== 7. 列出资源模板 ==========")
	templates, err := mcpClient.ListResourceTemplates(ctx, "")
	if err != nil {
		log.Printf("列出资源模板失败: %v", err)
	} else {
		for _, template := range templates.ResourceTemplates {
			fmt.Printf("  - %s: %s\n", template.URITemplate, template.Description)
			if len(template.Meta) > 0 {
				fmt.Printf("    元数据: %v\n", template.Meta)
			}
		}
	}

	// 列出提示模板
	fmt.Println("\n========== 8. 列出提示模板 ==========")
	prompts, err := mcpClient.ListPrompts(ctx, "")
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

	// 获取提示
	fmt.Println("\n========== 9. 获取提示 (code_review) ==========")
	promptResult, err := mcpClient.GetPrompt(ctx, "code_review", map[string]string{
		"language": "go",
		"code":     "func main() { fmt.Println(\"hello\") }",
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

	// 调用交互式工具 (Elicitation)
	fmt.Println("\n========== 10. 调用交互式工具 (interactive_greet) ==========")
	result, err = mcpClient.CallTool(ctx, "interactive_greet", map[string]interface{}{})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	// 调用 AI 工具 (Sampling)
	fmt.Println("\n========== 11. 调用 AI 工具 (ai_assistant) ==========")
	result, err = mcpClient.CallTool(ctx, "ai_assistant", map[string]interface{}{
		"question": "什么是 MCP 协议?",
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
	}

	// 调用资源链接工具 (Resource Links)
	fmt.Println("\n========== 12. 调用资源链接工具 (find_file) ==========")
	result, err = mcpClient.CallTool(ctx, "find_file", map[string]interface{}{
		"filename": "main.go",
	})
	if err != nil {
		log.Printf("调用工具失败: %v", err)
	} else {
		printToolResult(result)
		// 特别展示资源链接
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

	// 测试参数自动补全 (Completion)
	fmt.Println("\n========== 9. 测试参数自动补全 (Completion) ==========")
	testCompletion(ctx, mcpClient)

	fmt.Println("\n=================== END =====================")
}

// testCompletion 测试参数自动补全功能
func testCompletion(ctx context.Context, mcpClient client.Client) {
	// 测试提示参数补全
	fmt.Println("测试提示参数补全 (code_review.language):")

	// 补全 "py" -> ["python", "pytorch", "pyside"]
	completeReq := protocol.CompleteRequest{
		Ref: map[string]any{
			"type": "ref/prompt",
			"name": "code_review",
		},
		Argument: protocol.CompletionArgument{
			Name:  "language",
			Value: "py",
		},
	}

	response, err := mcpClient.SendRequest(ctx, "completion/complete", completeReq)
	if err != nil {
		log.Printf("  补全请求失败: %v", err)
		return
	}

	var result protocol.CompleteResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		log.Printf("  解析补全结果失败: %v", err)
		return
	}

	fmt.Printf("  输入: 'py'\n")
	fmt.Printf("  建议: %v\n", result.Completion.Values)
	if result.Completion.Total != nil {
		fmt.Printf("  总数: %d\n", *result.Completion.Total)
	}
	fmt.Printf("  更多: %v\n", result.Completion.HasMore)

	// 测试资源 URI 补全
	fmt.Println("\n测试资源 URI 补全 (file path):")

	completeReq2 := protocol.CompleteRequest{
		Ref: map[string]any{
			"type": "ref/resource",
			"uri":  "file:///{path}",
		},
		Argument: protocol.CompletionArgument{
			Name:  "path",
			Value: "/home",
		},
	}

	response2, err := mcpClient.SendRequest(ctx, "completion/complete", completeReq2)
	if err != nil {
		log.Printf("  补全请求失败: %v", err)
		return
	}

	var result2 protocol.CompleteResult
	if err := json.Unmarshal(response2.Result, &result2); err != nil {
		log.Printf("  解析补全结果失败: %v", err)
		return
	}

	fmt.Printf("  输入: '/home'\n")
	fmt.Printf("  建议: %v\n", result2.Completion.Values)
	fmt.Printf("  更多: %v\n", result2.Completion.HasMore)
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

	// 返回用户输入
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

	// 模拟 AI 响应 (实际应用中应该调用真实的 LLM API)
	response := fmt.Sprintf("这是一个模拟的 AI 响应。您的问题是: %s\n\nMCP (Model Context Protocol) 是一个开放协议,用于连接 AI 应用与外部数据源和工具。", userMessage)

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

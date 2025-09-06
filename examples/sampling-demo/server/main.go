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
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("收到停止信号，正在关闭服务器...")
		cancel()
	}()

	mcp := server.NewFastMCP("Sampling Demo Server", "1.0.0")

	// 设置模拟 elicitor（包含sampling支持）
	mcp.SetElicitor(server.NewMockElicitorWithSampling(
		// Elicitation handler
		func(ctx context.Context, method string, params interface{}) (*protocol.ElicitationResult, error) {
			if method != "elicitation/create" {
				return nil, fmt.Errorf("unsupported method: %s", method)
			}
			// 简单的模拟响应
			return protocol.NewElicitationAccept(map[string]interface{}{"response": "模拟用户输入"}), nil
		},
		// Sampling handler
		func(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
			// 模拟AI响应，基于请求内容生成不同的回复
			if len(request.Messages) > 0 {
				userMessage := ""
				if textContent, ok := request.Messages[0].Content.(protocol.TextContent); ok {
					userMessage = textContent.Text
				}

				var response string
				switch {
				case userMessage == "计算 2 + 3":
					response = "2 + 3 = 5"
				case userMessage == "什么是MCP?":
					response = "MCP (Model Context Protocol) 是一个用于AI应用与外部数据源和工具集成的开放标准协议。"
				default:
					response = fmt.Sprintf("我收到了你的消息: %s。这是一个模拟的AI响应。", userMessage)
				}

				return protocol.NewCreateMessageResult(
					protocol.RoleAssistant,
					protocol.NewTextContent(response),
					"mock-gpt-4",
					protocol.StopReasonEndTurn,
				), nil
			}

			return protocol.NewCreateMessageResult(
				protocol.RoleAssistant,
				protocol.NewTextContent("我是一个模拟的AI助手。"),
				"mock-gpt-4",
				protocol.StopReasonEndTurn,
			), nil
		},
	))

	// 注册一个使用 Sampling 的工具
	err := mcp.Tool("ai_calculator", "使用AI进行数学计算").
		WithStringParam("expression", "数学表达式", true).
		HandleWithElicitation(func(ctx *server.MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
			expression := args["expression"].(string)

			// 使用 Sampling 请求AI进行计算
			result, err := ctx.CreateTextMessageWithSystem(
				"你是一个数学计算助手。请计算用户提供的数学表达式，只返回计算结果。",
				fmt.Sprintf("计算 %s", expression),
				100,
			)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("AI计算失败: %v", err)), nil
			}

			// 提取AI的响应
			var aiResponse string
			if textContent, ok := result.Content.(protocol.TextContent); ok {
				aiResponse = textContent.Text
			} else {
				aiResponse = "无法解析AI响应"
			}

			return protocol.NewToolResultText(fmt.Sprintf("AI计算结果: %s (使用模型: %s)", aiResponse, result.Model)), nil
		})
	if err != nil {
		log.Fatalf("注册AI计算器工具失败: %v", err)
	}

	// 注册一个AI问答工具
	err = mcp.Tool("ai_chat", "与AI进行对话").
		WithStringParam("message", "要发送给AI的消息", true).
		WithStringParam("system_prompt", "系统提示（可选）", false).
		HandleWithElicitation(func(ctx *server.MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
			message := args["message"].(string)
			systemPrompt, _ := args["system_prompt"].(string)

			var result *protocol.CreateMessageResult
			var err error

			if systemPrompt != "" {
				// 使用自定义系统提示
				result, err = ctx.CreateTextMessageWithSystem(systemPrompt, message, 200)
			} else {
				// 使用默认设置
				result, err = ctx.CreateTextMessage(message, 200)
			}

			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("AI对话失败: %v", err)), nil
			}

			// 提取AI的响应
			var aiResponse string
			if textContent, ok := result.Content.(protocol.TextContent); ok {
				aiResponse = textContent.Text
			} else {
				aiResponse = "无法解析AI响应"
			}

			return protocol.NewToolResultText(fmt.Sprintf("AI回复: %s\n\n模型: %s\n停止原因: %s",
				aiResponse, result.Model, result.StopReason)), nil
		})
	if err != nil {
		log.Fatalf("注册AI对话工具失败: %v", err)
	}

	// 注册一个高级Sampling工具，演示更复杂的用法
	err = mcp.Tool("ai_conversation", "进行多轮AI对话").
		WithStringParam("user_message", "用户消息", true).
		WithStringParam("context", "对话上下文（可选）", false).
		HandleWithElicitation(func(ctx *server.MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
			userMessage := args["user_message"].(string)
			context, _ := args["context"].(string)

			// 构建对话历史
			messages := []protocol.SamplingMessage{}

			if context != "" {
				// 添加上下文作为助手的先前回复
				messages = append(messages, protocol.NewSamplingMessage(
					protocol.RoleAssistant,
					protocol.NewTextContent(context),
				))
			}

			// 添加用户消息
			messages = append(messages, protocol.NewSamplingMessage(
				protocol.RoleUser,
				protocol.NewTextContent(userMessage),
			))

			// 创建带有模型偏好的请求
			request := protocol.NewCreateMessageRequest(messages, 300).
				WithSystemPrompt("你是一个友好的AI助手，请提供有用和准确的回答。").
				WithModelPreferences(
					protocol.NewModelPreferences().
						WithHints(protocol.NewModelHint("gpt-4")).
						WithIntelligencePriority(0.8).
						WithSpeedPriority(0.5),
				).
				WithTemperature(0.7).
				WithIncludeContext(protocol.IncludeContextThisServer)

			result, err := ctx.CreateMessage(request)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("AI对话失败: %v", err)), nil
			}

			// 提取AI的响应
			var aiResponse string
			if textContent, ok := result.Content.(protocol.TextContent); ok {
				aiResponse = textContent.Text
			} else {
				aiResponse = "无法解析AI响应"
			}

			return protocol.NewToolResultText(fmt.Sprintf(
				"AI回复: %s\n\n详细信息:\n- 模型: %s\n- 停止原因: %s\n- 角色: %s",
				aiResponse, result.Model, result.StopReason, result.Role)), nil
		})
	if err != nil {
		log.Fatalf("注册AI对话工具失败: %v", err)
	}

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	log.Printf("Sampling Demo Server 启动在端口 %s", port)
	log.Printf("可用工具: ai_calculator, ai_chat, ai_conversation")
	log.Printf("协议版本: %s", protocol.MCPVersion)
	log.Printf("支持功能: Sampling (LLM采样), Elicitation (用户交互)")

	sseServer := sse.NewServer(":"+port, mcp)

	if err := sseServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}

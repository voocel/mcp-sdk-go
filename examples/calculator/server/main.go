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
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		log.Println("接收到关闭信号")
		cancel()
	}()

	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "计算器服务",
		Version: "1.0.0",
	}, nil)

	// 加法工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "add",
			Description: "两个数字相加",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "第一个数字",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "第二个数字",
					},
				},
				"required": []string{"a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			a, ok := req.Params.Arguments["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'a' 必须是数字"), nil
			}
			b, ok := req.Params.Arguments["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'b' 必须是数字"), nil
			}

			result := a + b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		},
	)

	// 减法工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "subtract",
			Description: "一个数字减去另一个数字",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "被减数",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "减数",
					},
				},
				"required": []string{"a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			a, ok := req.Params.Arguments["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'a' 必须是数字"), nil
			}
			b, ok := req.Params.Arguments["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'b' 必须是数字"), nil
			}

			result := a - b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		},
	)

	// 乘法工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "multiply",
			Description: "两个数字相乘",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "第一个数字",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "第二个数字",
					},
				},
				"required": []string{"a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			a, ok := req.Params.Arguments["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'a' 必须是数字"), nil
			}
			b, ok := req.Params.Arguments["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'b' 必须是数字"), nil
			}

			result := a * b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		},
	)

	// 除法工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "divide",
			Description: "一个数字除以另一个数字",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "被除数",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "除数",
					},
				},
				"required": []string{"a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			a, ok := req.Params.Arguments["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'a' 必须是数字"), nil
			}
			b, ok := req.Params.Arguments["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'b' 必须是数字"), nil
			}

			if b == 0 {
				return protocol.NewToolResultError("不能除以零"), nil
			}

			result := a / b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		},
	)

	// 帮助提示
	mcpServer.AddPrompt(
		&protocol.Prompt{
			Name:        "calculator_help",
			Description: "计算器帮助信息",
		},
		func(ctx context.Context, req *server.GetPromptRequest) (*protocol.GetPromptResult, error) {
			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"这是一个简单的计算器服务，支持四种基本运算：加法、减法、乘法和除法。")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					"我该如何使用这个计算器？")),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					"使用 add、subtract、multiply 和 divide 工具来执行计算。每个工具都接受两个参数：a 和 b。")),
			}
			return protocol.NewGetPromptResult("为编程问题提供帮助的提示模板", messages...), nil
		},
	)

	log.Println("启动计算器 MCP 服务器 (STDIO)...")

	if err := mcpServer.Run(ctx, &stdio.StdioTransport{}); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}

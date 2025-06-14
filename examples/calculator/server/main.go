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

	mcp := server.NewFastMCP("计算器服务", "1.0.0")

	mcp.Tool("add", "两个数字相加").
		WithIntParam("a", "第一个数字", true).
		WithIntParam("b", "第二个数字", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			a, ok := args["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'a' 必须是数字"), nil
			}
			b, ok := args["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'b' 必须是数字"), nil
			}
			
			result := a + b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		})

	mcp.Tool("subtract", "一个数字减去另一个数字").
		WithIntParam("a", "被减数", true).
		WithIntParam("b", "减数", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			a, ok := args["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'a' 必须是数字"), nil
			}
			b, ok := args["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'b' 必须是数字"), nil
			}
			
			result := a - b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		})

	mcp.Tool("multiply", "两个数字相乘").
		WithIntParam("a", "第一个数字", true).
		WithIntParam("b", "第二个数字", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			a, ok := args["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'a' 必须是数字"), nil
			}
			b, ok := args["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'b' 必须是数字"), nil
			}
			
			result := a * b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		})

	mcp.Tool("divide", "一个数字除以另一个数字").
		WithIntParam("a", "被除数", true).
		WithIntParam("b", "除数", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			a, ok := args["a"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'a' 必须是数字"), nil
			}
			b, ok := args["b"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'b' 必须是数字"), nil
			}

			if b == 0 {
				return protocol.NewToolResultError("不能除以零"), nil
			}

			result := a / b
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
		})

	mcp.Prompt("calculator_help", "计算器帮助信息").
		Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"这是一个简单的计算器服务，支持四种基本运算：加法、减法、乘法和除法。")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					"我该如何使用这个计算器？")),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					"使用 add、subtract、multiply 和 divide 工具来执行计算。每个工具都接受两个参数：a 和 b。")),
			}
			return protocol.NewGetPromptResult("为编程问题提供帮助的提示模板", messages...), nil
		})

	stdioServer := stdio.NewServer(mcp)
	
	log.Println("启动计算器 MCP 服务器 (STDIO)...")
	if err := stdioServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}

package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	// 设置随机种子
	rand.Seed(time.Now().UnixNano())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 处理优雅关闭
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		log.Println("接收到关闭信号")
		cancel()
	}()

	// 创建FastMCP服务器
	mcp := server.NewFastMCP("SSE服务器", "1.0.0")

	// 注册一个简单的回声工具
	mcp.Tool("echo", "回声工具，返回输入的文本").
		WithStringParam("message", "要回声的消息", true).
		Handle(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			message, ok := args["message"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'message' 必须是字符串"), nil
			}
			
			response := fmt.Sprintf("回声: %s", message)
			return protocol.NewToolResultText(response), nil
		})

	// 注册一个随机数生成工具
	mcp.Tool("random", "生成指定范围内的随机数").
		WithIntParam("min", "最小值", true).
		WithIntParam("max", "最大值", true).
		Handle(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			min, ok := args["min"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'min' 必须是整数"), nil
			}
			max, ok := args["max"].(float64)
			if !ok {
				return protocol.NewToolResultError("参数 'max' 必须是整数"), nil
			}

			minInt := int(min)
			maxInt := int(max)

			if minInt >= maxInt {
				return protocol.NewToolResultError("最小值必须小于最大值"), nil
			}

			randomNum := rand.Intn(maxInt-minInt) + minInt
			result := fmt.Sprintf("随机数: %d (范围: %d-%d)", randomNum, minInt, maxInt-1)
			return protocol.NewToolResultText(result), nil
		})

	// 注册一个时间工具
	mcp.Tool("time", "获取当前时间").
		WithStringParam("format", "时间格式 (iso, unix, readable)", false).
		Handle(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			format, ok := args["format"].(string)
			if !ok {
				format = "readable"
			}

			now := time.Now()
			var timeStr string

			switch strings.ToLower(format) {
			case "iso":
				timeStr = now.Format(time.RFC3339)
			case "unix":
				timeStr = fmt.Sprintf("%d", now.Unix())
			case "readable":
				fallthrough
			default:
				timeStr = now.Format("2006-01-02 15:04:05")
			}

			result := fmt.Sprintf("当前时间: %s", timeStr)
			return protocol.NewToolResultText(result), nil
		})

	// 注册文本处理工具
	mcp.Tool("text_transform", "文本转换工具").
		WithStringParam("text", "要转换的文本", true).
		WithStringParam("operation", "操作类型 (upper, lower, reverse, length)", true).
		Handle(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			text, ok := args["text"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'text' 必须是字符串"), nil
			}
			operation, ok := args["operation"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'operation' 必须是字符串"), nil
			}

			var result string
			switch strings.ToLower(operation) {
			case "upper":
				result = fmt.Sprintf("大写: %s", strings.ToUpper(text))
			case "lower":
				result = fmt.Sprintf("小写: %s", strings.ToLower(text))
			case "reverse":
				runes := []rune(text)
				for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
					runes[i], runes[j] = runes[j], runes[i]
				}
				result = fmt.Sprintf("反转: %s", string(runes))
			case "length":
				result = fmt.Sprintf("长度: %d 个字符", len([]rune(text)))
			default:
				return protocol.NewToolResultError("不支持的操作类型。支持: upper, lower, reverse, length"), nil
			}

			return protocol.NewToolResultText(result), nil
		})

	// 注册服务器信息资源
	mcp.Resource("info://server", "服务器信息", "获取SSE服务器的基本信息").
		Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
			info := fmt.Sprintf(`SSE服务器
版本: 1.0.0
协议: MCP %s
传输: Server-Sent Events (SSE)
启动时间: %s
功能: 回声、随机数、时间、文本转换`,
				protocol.MCPVersion,
				time.Now().Format("2006-01-02 15:04:05"))
			
			contents := protocol.NewTextResourceContents("info://server", info)
			return protocol.NewReadResourceResult(contents), nil
		})

	// 注册状态资源
	mcp.Resource("status://health", "健康状态", "服务器健康检查信息").
		WithMimeType("application/json").
		Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
			status := fmt.Sprintf(`{
  "status": "healthy",
  "timestamp": "%s",
  "uptime": "%s",
  "tools_count": 4,
  "resources_count": 2
}`, time.Now().Format(time.RFC3339), time.Since(time.Now().Add(-time.Hour)).String())
			
			contents := protocol.NewTextResourceContents("status://health", status)
			return protocol.NewReadResourceResult(contents), nil
		})

	// 注册使用指南提示模板
	mcp.Prompt("usage_guide", "使用指南").
		WithArgument("tool_name", "工具名称", false).
		Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
			toolName := args["tool_name"]
			
			var content string
			if toolName != "" {
				content = fmt.Sprintf("请告诉我如何使用 '%s' 工具。", toolName)
			} else {
				content = "请告诉我这个SSE服务器有哪些功能，以及如何使用它们。"
			}

			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"你是一个友好的助手，专门帮助用户了解和使用SSE服务器的功能。")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(content)),
			}
			
			return protocol.NewGetPromptResult("SSE服务器使用指南", messages...), nil
		})

	// 设置通知处理器
	mcp.SetNotificationHandler(func(method string, params any) error {
		log.Printf("发送通知: %s", method)
		return nil
	})

	// 创建SSE服务器
	sseServer := sse.NewServer(":8081", mcp)
	
	log.Println("SSE服务器启动在 http://localhost:8081")
	log.Println("可用工具: echo, random, time, text_transform")
	log.Println("可用资源: info://server, status://health")
	log.Println("可用提示: usage_guide")
	log.Println()
	
	if err := sseServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}

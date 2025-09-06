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

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		log.Println("接收到关闭信号")
		cancel()
	}()

	mcp := server.NewFastMCP("Elicitation Demo Server", "1.0.0")

	// 设置模拟 elicitation 处理器（用于演示）
	// 在实际应用中，这个处理器会通过传输层连接到真实的客户端
	mcp.SetElicitor(server.NewMockElicitor(func(ctx context.Context, method string, params interface{}) (*protocol.ElicitationResult, error) {
		// 只处理 elicitation/create 请求
		if method != "elicitation/create" {
			return nil, fmt.Errorf("unsupported method: %s", method)
		}

		elicitParams, ok := params.(*protocol.ElicitationCreateParams)
		if !ok {
			return nil, fmt.Errorf("invalid params type")
		}

		// 根据消息内容返回模拟响应
		switch elicitParams.Message {
		case "请输入你的姓名":
			return protocol.NewElicitationAccept(map[string]interface{}{"name": "张三"}), nil
		case "请选择你喜欢的颜色":
			return protocol.NewElicitationAccept(map[string]interface{}{"color": "blue"}), nil
		case "请输入你的年龄":
			return protocol.NewElicitationAccept(map[string]interface{}{"age": 25}), nil
		default:
			return protocol.NewElicitationAccept(map[string]interface{}{"response": "默认响应"}), nil
		}
	}))

	// 注册一个简单的测试工具（不使用 elicitation）
	err := mcp.Tool("simple_test", "简单测试工具").
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			return protocol.NewToolResultText("简单测试工具工作正常！"), nil
		})
	if err != nil {
		log.Fatalf("注册简单测试工具失败: %v", err)
	}

	// 注册一个需要用户输入的工具
	err = mcp.Tool("user_profile", "创建用户档案，需要收集用户信息").
		HandleWithElicitation(func(ctx *server.MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
			// 请求用户姓名
			name, err := ctx.ElicitString("请输入你的姓名", "name", "你的全名", true)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("获取姓名失败: %v", err)), nil
			}

			// 请求用户喜欢的颜色
			color, err := ctx.ElicitChoice(
				"请选择你喜欢的颜色",
				"color",
				"你最喜欢的颜色",
				[]string{"red", "green", "blue", "yellow"},
				[]string{"红色", "绿色", "蓝色", "黄色"},
				true,
			)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("获取颜色失败: %v", err)), nil
			}

			// 请求用户年龄
			age, err := ctx.ElicitNumber("请输入你的年龄", "age", "你的年龄", func() *float64 { v := 18.0; return &v }(), func() *float64 { v := 100.0; return &v }(), true)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("获取年龄失败: %v", err)), nil
			}

			// 创建用户档案
			profile := fmt.Sprintf("用户档案已创建:\n姓名: %s\n喜欢的颜色: %s\n年龄: %.0f岁", name, color, age)
			return protocol.NewToolResultText(profile), nil
		})

	if err != nil {
		log.Fatalf("注册工具失败: %v", err)
	}

	// 注册一个餐厅预订工具（演示复杂的 elicitation 流程）
	err = mcp.Tool("book_restaurant", "预订餐厅，如果首选日期不可用会询问备选方案").
		WithStringParam("date", "预订日期 (YYYY-MM-DD)", true).
		WithIntParam("party_size", "用餐人数", true).
		HandleWithElicitation(func(ctx *server.MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
			date := args["date"].(string)
			partySize := int(args["party_size"].(float64))

			// 模拟检查日期可用性
			if date == "2024-12-25" {
				// 圣诞节不可用，询问用户是否选择其他日期
				schema := protocol.CreateElicitationSchema()
				schema["properties"] = map[string]interface{}{
					"checkAlternative": map[string]interface{}{
						"type":        "boolean",
						"description": "是否检查其他日期？",
					},
					"alternativeDate": map[string]interface{}{
						"type":        "string",
						"description": "备选日期 (YYYY-MM-DD)",
						"default":     "2024-12-26",
					},
				}
				schema["required"] = []string{"checkAlternative"}

				result, err := ctx.Elicit(
					fmt.Sprintf("抱歉，%s 没有 %d 人的空位。是否要检查其他日期？", date, partySize),
					schema,
				)
				if err != nil {
					return protocol.NewToolResultError(fmt.Sprintf("获取用户选择失败: %v", err)), nil
				}

				if !result.IsAccepted() {
					return protocol.NewToolResultText("预订已取消"), nil
				}

				contentMap := result.Content.(map[string]interface{})
				checkAlternative, _ := contentMap["checkAlternative"].(bool)

				if !checkAlternative {
					return protocol.NewToolResultText("预订已取消"), nil
				}

				alternativeDate, _ := contentMap["alternativeDate"].(string)
				return protocol.NewToolResultText(fmt.Sprintf("预订成功！\n日期: %s\n人数: %d 人\n备注: 已改为备选日期", alternativeDate, partySize)), nil
			}

			// 日期可用
			return protocol.NewToolResultText(fmt.Sprintf("预订成功！\n日期: %s\n人数: %d 人", date, partySize)), nil
		})

	if err != nil {
		log.Fatalf("注册餐厅预订工具失败: %v", err)
	}

	// 注册一个简单的计算器工具（不需要elicitation）
	err = mcp.SimpleTextTool("calculator", "简单计算器", func(ctx context.Context, args map[string]interface{}) (string, error) {
		operation := args["operation"].(string)
		a := args["a"].(float64)
		b := args["b"].(float64)

		switch operation {
		case "add":
			return fmt.Sprintf("%.2f + %.2f = %.2f", a, b, a+b), nil
		case "subtract":
			return fmt.Sprintf("%.2f - %.2f = %.2f", a, b, a-b), nil
		case "multiply":
			return fmt.Sprintf("%.2f × %.2f = %.2f", a, b, a*b), nil
		case "divide":
			if b == 0 {
				return "", fmt.Errorf("除数不能为零")
			}
			return fmt.Sprintf("%.2f ÷ %.2f = %.2f", a, b, a/b), nil
		default:
			return "", fmt.Errorf("不支持的操作: %s", operation)
		}
	})

	// 为计算器工具添加参数
	mcp.Tool("calculator", "简单计算器").
		WithStringParam("operation", "操作类型 (add, subtract, multiply, divide)", true).
		WithNumberParam("a", "第一个数字", true).
		WithNumberParam("b", "第二个数字", true)

	if err != nil {
		log.Fatalf("注册计算器工具失败: %v", err)
	}

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	log.Printf("Elicitation Demo Server 启动在端口 %s", port)
	log.Printf("可用工具: user_profile, book_restaurant, calculator", port)
	log.Printf("协议版本: %s", protocol.MCPVersion)

	sseServer := sse.NewServer(":"+port, mcp)

	if err := sseServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}

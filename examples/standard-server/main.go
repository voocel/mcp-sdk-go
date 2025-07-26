package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/sse"
	"github.com/voocel/mcp-sdk-go/utils"
)

func main() {
	mcp := server.NewFastMCP("StandardMCPServer", "1.0.0")

	// 注册一个计算工具
	err := mcp.Tool("calculate", "执行基本数学运算").
		WithStringParam("operation", "运算类型 (add, subtract, multiply, divide)", true).
		WithIntParam("a", "第一个数字", true).
		WithIntParam("b", "第二个数字", true).
		Handle(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			operation := args["operation"].(string)
			a := int(args["a"].(float64))
			b := int(args["b"].(float64))

			var result int
			switch operation {
			case "add":
				result = a + b
			case "subtract":
				result = a - b
			case "multiply":
				result = a * b
			case "divide":
				if b == 0 {
					return protocol.NewToolResultError("除数不能为零"), nil
				}
				result = a / b
			default:
				return protocol.NewToolResultError("不支持的运算类型"), nil
			}

			return protocol.NewToolResultText(
				fmt.Sprintf("%d %s %d = %d", a, operation, b, result),
			), nil
		})

	if err != nil {
		log.Fatalf("注册工具失败: %v", err)
	}

	// 注册一个问候工具（使用结构体schema）
	type GreetArgs struct {
		Name     string `json:"name" jsonschema:"required,description=要问候的人的姓名"`
		Language string `json:"language" jsonschema:"description=语言代码 (zh, en)"`
		Formal   bool   `json:"formal" jsonschema:"description=是否使用正式问候"`
	}

	err = mcp.Tool("greet", "生成个性化问候语").
		WithStructSchema(GreetArgs{}).
		HandleWithValidation(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			var greetArgs GreetArgs
			jsonData, err := utils.StructToJSON(args)
			if err != nil {
				return protocol.NewToolResultError("参数序列化错误"), nil
			}
			if err := utils.JSONToStruct(jsonData, &greetArgs); err != nil {
				return protocol.NewToolResultError("参数解析错误"), nil
			}

			var greeting string
			if greetArgs.Language == "zh" {
				if greetArgs.Formal {
					greeting = fmt.Sprintf("你好，%s先生/女士！", greetArgs.Name)
				} else {
					greeting = fmt.Sprintf("你好，%s！", greetArgs.Name)
				}
			} else {
				if greetArgs.Formal {
					greeting = fmt.Sprintf("Good day, Mr./Ms. %s!", greetArgs.Name)
				} else {
					greeting = fmt.Sprintf("Hello, %s!", greetArgs.Name)
				}
			}

			return protocol.NewToolResultText(greeting), nil
		})

	if err != nil {
		log.Fatalf("注册问候工具失败: %v", err)
	}

	// 注册一个配置资源
	err = mcp.Resource("config://server", "server_config", "服务器配置信息").
		WithMimeType("application/json").
		Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
			configData := `{
				"name": "StandardMCPServer",
				"version": "1.0.0",
				"capabilities": ["tools", "resources", "prompts"],
				"max_connections": 100
			}`
			return protocol.NewReadResourceResult(
				protocol.NewTextResourceContents("config://server", configData),
			), nil
		})

	if err != nil {
		log.Fatalf("注册资源失败: %v", err)
	}

	// 注册一个系统状态资源
	err = mcp.SimpleTextResource("status://system", "system_status", "系统状态信息",
		func(ctx context.Context) (string, error) {
			caps := mcp.Server().GetCapabilities()
			toolCount := 0
			if caps.Tools != nil {
				// 工具数量需要从服务器内部获取
				toolCount = 2 // 已注册的工具数量
			}
			return fmt.Sprintf("服务器运行正常\n工具数量: %d\n协议版本: %s",
				toolCount, protocol.MCPVersion), nil
		})

	if err != nil {
		log.Fatalf("注册状态资源失败: %v", err)
	}

	// 注册一个提示模板
	err = mcp.Prompt("code_review", "代码审查提示模板").
		WithArgument("language", "编程语言", true).
		WithArgument("code", "要审查的代码", true).
		Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
			language := args["language"]
			code := args["code"]

			systemMsg := protocol.NewPromptMessage(protocol.RoleSystem,
				protocol.NewTextContent("你是一个专业的代码审查专家，请仔细审查提供的代码并给出建设性的反馈。"))

			userMsg := protocol.NewPromptMessage(protocol.RoleUser,
				protocol.NewTextContent(fmt.Sprintf("请审查以下%s代码：\n\n```%s\n%s\n```",
					language, language, code)))

			return protocol.NewGetPromptResult(
				"代码审查提示模板，帮助进行专业的代码审查",
				systemMsg, userMsg,
			), nil
		})

	if err != nil {
		log.Fatalf("注册提示模板失败: %v", err)
	}

	// 简单的文本提示模板
	err = mcp.SimpleTextPrompt("hello_template", "简单问候模板",
		func(ctx context.Context, args map[string]string) (string, error) {
			name := args["name"]
			if name == "" {
				name = "朋友"
			}
			return fmt.Sprintf("你好，%s！欢迎使用MCP标准服务器！", name), nil
		})

	if err != nil {
		log.Fatalf("注册简单提示模板失败: %v", err)
	}

	// 设置通知处理器
	mcp.SetNotificationHandler(func(method string, params any) error {
		log.Printf("发送通知: %s, 参数: %+v", method, params)
		return nil
	})

	log.Printf("MCP标准服务器启动成功")
	log.Printf("服务器信息: %+v", mcp.GetServerInfo())
	log.Printf("服务器能力: %+v", mcp.GetCapabilities())

	ctx := context.Background()

	// 初始化请求
	initMsg, _ := utils.NewJSONRPCRequest("initialize", protocol.InitializeRequest{
		ProtocolVersion: protocol.MCPVersion,
		ClientInfo: protocol.ClientInfo{
			Name:    "TestClient",
			Version: "1.0.0",
		},
		Capabilities: protocol.ClientCapabilities{},
	})

	initData, _ := json.Marshal(initMsg)
	responseData, err := mcp.HandleMessage(ctx, initData)
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}
	log.Printf("初始化响应: %s", string(responseData))

	// 列出工具
	toolsMsg, _ := utils.NewJSONRPCRequest("tools/list", nil)
	toolsData, _ := json.Marshal(toolsMsg)
	responseData, err = mcp.HandleMessage(ctx, toolsData)
	if err != nil {
		log.Fatalf("获取工具列表失败: %v", err)
	}
	log.Printf("工具列表响应: %s", string(responseData))

	// 调用工具
	callMsg, _ := utils.NewJSONRPCRequest("tools/call", protocol.CallToolRequest{
		Name: "calculate",
		Arguments: map[string]any{
			"operation": "add",
			"a":         10,
			"b":         5,
		},
	})

	callData, _ := json.Marshal(callMsg)
	responseData, err = mcp.HandleMessage(ctx, callData)
	if err != nil {
		log.Fatalf("调用工具失败: %v", err)
	}
	log.Printf("工具调用响应: %s", string(responseData))

	// 演示SSE Transport使用
	log.Printf("\n=== SSE Transport 开始 ===")

	// 启动SSE服务器
	sseServer := sse.NewServer(":8080", mcp)
	go func() {
		log.Printf("SSE服务器启动在 :8080")
		if err := sseServer.Serve(ctx); err != nil {
			log.Printf("SSE服务器错误: %v", err)
		}
	}()

	// 等待一下让服务器启动
	time.Sleep(time.Second)

	// 创建SSE客户端
	sseTransport := sse.New("http://localhost:8080",
		sse.WithProtocolVersion(protocol.MCPVersion))

	// 连接SSE流
	if err := sseTransport.Connect(ctx); err != nil {
		log.Printf("SSE连接失败: %v", err)
	} else {
		log.Printf("SSE客户端连接成功，Session ID: %s", sseTransport.GetSessionID())

		// 通过SSE发送消息
		testMsg, _ := utils.NewJSONRPCRequest("tools/list", nil)
		testData, _ := json.Marshal(testMsg)

		if err := sseTransport.Send(ctx, testData); err != nil {
			log.Printf("SSE发送失败: %v", err)
		} else {
			log.Printf("通过SSE发送了工具列表请求")

			// 接收响应
			if response, err := sseTransport.Receive(ctx); err != nil {
				log.Printf("SSE接收失败: %v", err)
			} else {
				log.Printf("SSE接收到响应: %s", string(response))
			}
		}

		sseTransport.Close()
	}

	log.Printf("\n=== 完成 ===")
}

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

func main() {
	mcp := server.NewFastMCP("BasicMCPServer", "1.0.0")

	// ========== 1. 基础工具 ==========
	mcp.Tool("greet", "问候用户").
		WithStringParam("name", "用户名称", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			name := args["name"].(string)
			greeting := fmt.Sprintf("Hello, %s! Welcome to MCP!", name)
			return protocol.NewToolResultText(greeting), nil
		})

	// ========== 2. 带元数据和标题的工具 (MCP 2025-06-18) ==========
	mcp.Tool("calculate", "执行数学计算").
		WithTitle("计算器工具"). // 人类友好的标题
		WithStringParam("operation", "运算类型 (add, subtract, multiply, divide)", true).
		WithIntParam("a", "第一个数字", true).
		WithIntParam("b", "第二个数字", true).
		WithMeta("category", "math").                  // 分类
		WithMeta("version", "2.0").                    // 版本
		WithMeta("tags", []string{"math", "utility"}). // 标签
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
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

			// 返回带元数据的结果
			return &protocol.CallToolResult{
				Content: []protocol.Content{
					protocol.NewTextContent(fmt.Sprintf("计算结果: %d", result)),
				},
				Meta: map[string]interface{}{
					"operation":      operation,
					"processingTime": "0.001s",
				},
			}, nil
		})

	// ========== 3. 带输出 Schema 的工具 (MCP 2025-06-18) ==========
	mcp.Tool("get_time", "获取当前时间").
		WithOutputSchema(protocol.JSONSchema{
			"type": "object",
			"properties": map[string]interface{}{
				"timestamp": map[string]string{"type": "string"},
				"timezone":  map[string]string{"type": "string"},
			},
		}).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			now := time.Now()
			return &protocol.CallToolResult{
				Content: []protocol.Content{
					protocol.NewTextContent(now.Format(time.RFC3339)),
				},
				StructuredContent: map[string]interface{}{
					"timestamp": now.Unix(),
					"timezone":  now.Location().String(),
				},
			}, nil
		})

	// ========== 4. 基础资源 ==========
	mcp.SimpleTextResource("info://server", "server_info", "服务器信息",
		func(ctx context.Context) (string, error) {
			return fmt.Sprintf("MCP Basic Server v1.0.0\nProtocol: %s\nFeatures: Tools, Resources, Prompts, Meta", protocol.MCPVersion), nil
		})

	// ========== 5. 带元数据的资源 (MCP 2025-06-18) ==========
	mcp.Resource("config://app", "app_config", "应用配置").
		WithMimeType("application/json").
		WithMeta("environment", "development"). // 环境
		WithMeta("version", "1.0.0").           // 版本
		Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
			config := `{
  "app": "BasicMCPServer",
  "version": "1.0.0",
  "features": ["tools", "resources", "prompts", "meta"]
}`
			return &protocol.ReadResourceResult{
				Contents: []protocol.ResourceContents{
					{
						URI:      "config://app",
						MimeType: "application/json",
						Text:     config,
					},
				},
			}, nil
		})

	// ========== 6. 资源模板 (MCP 2025-06-18) ==========
	// 注册资源模板声明 (告诉客户端支持的 URI 模式)
	mcp.ResourceTemplate("echo:///{message}", "echo", "回显消息").
		WithMimeType("text/plain").
		WithMeta("scope", "echo").
		Register()

	// 注册一个示例资源 (客户端可以根据模板构造 URI 来访问)
	mcp.Resource("echo:///hello", "echo_hello", "回显 hello").
		WithMimeType("text/plain").
		Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
			return &protocol.ReadResourceResult{
				Contents: []protocol.ResourceContents{
					{
						URI:      "echo:///hello",
						MimeType: "text/plain",
						Text:     "Echo: hello",
					},
				},
			}, nil
		})

	// ========== 7. 提示模板 (带标题) ==========
	mcp.Prompt("code_review", "代码审查提示").
		WithTitle("代码审查助手"). // 人类友好的标题
		WithArgument("language", "编程语言", true).
		WithArgument("code", "要审查的代码", true).
		WithMeta("domain", "software-engineering").
		WithMeta("difficulty", "intermediate").
		Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
			language := args["language"]
			code := args["code"]

			return &protocol.GetPromptResult{
				Description: "代码审查提示",
				Messages: []protocol.PromptMessage{
					{
						Role: protocol.RoleUser,
						Content: protocol.NewTextContent(
							fmt.Sprintf("请审查以下 %s 代码并提供改进建议:\n\n```%s\n%s\n```", language, language, code),
						),
					},
				},
				Meta: map[string]interface{}{
					"language": language,
					"codeSize": len(code),
				},
			}, nil
		})

	// ========== 8. 带 Elicitation 的交互式工具 (MCP 2025-06-18) ==========
	mcp.Tool("interactive_greet", "交互式问候").
		WithStringParam("greeting", "问候语", false).
		HandleWithElicitation(func(ctx *server.MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
			// 如果没有提供问候语,请求用户输入
			greeting, ok := args["greeting"].(string)
			if !ok || greeting == "" {
				name, err := ctx.ElicitString("请输入您的名字", "name", "用户名", true)
				if err != nil {
					return protocol.NewToolResultError(fmt.Sprintf("获取用户输入失败: %v", err)), nil
				}
				greeting = fmt.Sprintf("你好, %s!", name)
			}

			return protocol.NewToolResultText(greeting), nil
		})

	// ========== 9. 带 Sampling 的 AI 工具 (MCP 2025-06-18) ==========
	mcp.Tool("ai_assistant", "AI 助手 - 使用 LLM 回答问题").
		WithStringParam("question", "要问的问题", true).
		HandleWithElicitation(func(ctx *server.MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
			question := args["question"].(string)

			// 发起LLM推理请求
			result, err := ctx.CreateTextMessageWithSystem(
				"你是一个友好的助手,用简洁的语言回答问题。",
				question,
				500, // maxTokens
			)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("AI 推理失败: %v", err)), nil
			}

			// 提取AI响应
			if textContent, ok := result.Content.(protocol.TextContent); ok {
				return &protocol.CallToolResult{
					Content: []protocol.Content{
						protocol.NewTextContent(textContent.Text),
					},
					Meta: map[string]interface{}{
						"model":      result.Model,
						"stopReason": result.StopReason,
					},
				}, nil
			}

			return protocol.NewToolResultError("无法解析 AI 响应"), nil
		})

	// ========== 10. 资源链接工具 - Resource Links (MCP 2025-06-18) ==========
	mcp.Tool("find_file", "查找文件并返回资源链接").
		WithStringParam("filename", "要查找的文件名", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			filename := args["filename"].(string)

			// 模拟文件查找
			fileURI := fmt.Sprintf("file:///project/src/%s", filename)

			// 创建带注解的资源链接
			annotation := protocol.NewAnnotation().
				WithAudience(protocol.RoleAssistant).
				WithPriority(0.9)

			resourceLink := protocol.NewResourceLinkContentWithDetails(
				fileURI,
				filename,
				"Found file in project",
				"text/plain",
			)
			resourceLink.WithAnnotations(annotation)

			return &protocol.CallToolResult{
				Content: []protocol.Content{
					protocol.NewTextContent(fmt.Sprintf("找到文件: %s", filename)),
					resourceLink,
				},
			}, nil
		})

	// ========== 启动服务器 ==========
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdioServer := stdio.NewServer(mcp)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// ========== 10. 参数自动补全 (Completion) - MCP 2025-06-18 ==========
	mcp.Completion(func(ctx context.Context, ref protocol.CompletionReference, argument protocol.CompletionArgument, context *protocol.CompletionContext) (*protocol.CompletionResult, error) {
		// 根据引用类型处理补全
		switch r := ref.(type) {
		case protocol.PromptReference:
			// 提示参数补全
			if r.Name == "code_review" && argument.Name == "language" {
				// 根据当前输入值过滤语言列表
				allLanguages := []string{"python", "pytorch", "pyside", "javascript", "java", "go", "rust", "typescript"}
				var matches []string
				for _, lang := range allLanguages {
					if len(argument.Value) == 0 || len(lang) >= len(argument.Value) && lang[:len(argument.Value)] == argument.Value {
						matches = append(matches, lang)
					}
				}
				result := protocol.NewCompletionResultWithTotal(matches, len(allLanguages), len(matches) < len(allLanguages))
				return &result, nil
			}

		case protocol.ResourceReference:
			// 资源 URI 补全
			if argument.Name == "path" {
				// 模拟文件路径补全
				paths := []string{"/home/user/file1.txt", "/home/user/file2.txt", "/home/user/documents/"}
				var matches []string
				for _, path := range paths {
					if len(argument.Value) == 0 || len(path) >= len(argument.Value) && path[:len(argument.Value)] == argument.Value {
						matches = append(matches, path)
					}
				}
				result := protocol.NewCompletionResult(matches, len(matches) < len(paths))
				return &result, nil
			}
		}

		// 默认返回空结果
		result := protocol.NewCompletionResult([]string{}, false)
		return &result, nil
	})

	go func() {
		<-sigChan
		log.Println("收到中断信号，正在关闭服务器...")
		cancel()
	}()

	log.Println("========================================")
	log.Println("MCP Basic Server 启动")
	log.Println("========================================")
	log.Println("传输协议: STDIO")
	log.Println("协议版本: MCP", protocol.MCPVersion)
	log.Println("")
	log.Println("功能列表:")
	log.Println("  ✓ 基础工具 (greet)")
	log.Println("  ✓ 带元数据和标题的工具 (calculate)")
	log.Println("  ✓ 带输出 Schema 的工具 (get_time)")
	log.Println("  ✓ 基础资源 (info://server)")
	log.Println("  ✓ 带元数据的资源 (config://app)")
	log.Println("  ✓ 资源模板 (echo:///{message})")
	log.Println("  ✓ 提示模板 - 带标题 (code_review)")
	log.Println("  ✓ 交互式工具 - Elicitation (interactive_greet)")
	log.Println("  ✓ AI 工具 - Sampling (ai_assistant)")
	log.Println("  ✓ 资源链接工具 - Resource Links (find_file)")
	log.Println("  ✓ 参数自动补全 - Completion (code_review.language, resource paths)")
	log.Println("========================================")
	log.Println("等待客户端连接...")

	if err := stdioServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}

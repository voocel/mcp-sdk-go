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
	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "BasicMCPServer",
		Version: "1.0.0",
	}, nil)

	// ========== 基础工具 ==========
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "greet",
			Description: "问候用户",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "用户名称",
					},
				},
				"required": []string{"name"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			name, ok := req.Params.Arguments["name"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'name' 必须是字符串"), nil
			}
			greeting := fmt.Sprintf("Hello, %s! Welcome to MCP!", name)
			return protocol.NewToolResultText(greeting), nil
		},
	)

	// ========== 带元数据的工具 (MCP 2025-06-18) ==========
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "calculate",
			Description: "执行数学计算",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"description": "运算类型 (add, subtract, multiply, divide)",
					},
					"a": map[string]interface{}{
						"type":        "number",
						"description": "第一个数字",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "第二个数字",
					},
				},
				"required": []string{"operation", "a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			operation, _ := req.Params.Arguments["operation"].(string)
			a := int(req.Params.Arguments["a"].(float64))
			b := int(req.Params.Arguments["b"].(float64))

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
			}, nil
		},
	)

	// ========== 获取时间工具 ==========
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "get_time",
			Description: "获取当前时间",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			now := time.Now()
			return &protocol.CallToolResult{
				Content: []protocol.Content{
					protocol.NewTextContent(now.Format(time.RFC3339)),
				},
			}, nil
		},
	)

	// ========== 基础资源 ==========
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "info://server",
			Name:        "server_info",
			Description: "服务器信息",
			MimeType:    "text/plain",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			info := fmt.Sprintf("MCP Basic Server v1.0.0\nProtocol: %s\nFeatures: Tools, Resources, Prompts", protocol.MCPVersion)
			return &protocol.ReadResourceResult{
				Contents: []protocol.ResourceContents{
					{
						URI:      "info://server",
						MimeType: "text/plain",
						Text:     info,
					},
				},
			}, nil
		},
	)

	// ========== JSON 配置资源 ==========
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "config://app",
			Name:        "app_config",
			Description: "应用配置",
			MimeType:    "application/json",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			config := `{
  "app": "BasicMCPServer",
  "version": "1.0.0",
  "features": ["tools", "resources", "prompts"]
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
		},
	)

	// ========== 资源模板示例 ==========
	mcpServer.AddResourceTemplate(
		&protocol.ResourceTemplate{
			URITemplate: "echo:///{message}",
			Name:        "echo",
			Description: "回显消息",
			MimeType:    "text/plain",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			// 从 URI 中提取消息
			message := "hello" // 简化示例,实际应该从 URI 解析
			return &protocol.ReadResourceResult{
				Contents: []protocol.ResourceContents{
					{
						URI:      req.Params.URI,
						MimeType: "text/plain",
						Text:     fmt.Sprintf("Echo: %s", message),
					},
				},
			}, nil
		},
	)

	// ========== 提示模板 ==========
	mcpServer.AddPrompt(
		&protocol.Prompt{
			Name:        "code_review",
			Description: "代码审查提示",
			Arguments: []protocol.PromptArgument{
				{
					Name:        "language",
					Description: "编程语言",
					Required:    true,
				},
				{
					Name:        "code",
					Description: "要审查的代码",
					Required:    true,
				},
			},
		},
		func(ctx context.Context, req *server.GetPromptRequest) (*protocol.GetPromptResult, error) {
			language, _ := req.Params.Arguments["language"]
			code, _ := req.Params.Arguments["code"]

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
			}, nil
		},
	)

	// ========== 资源链接工具 ==========
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "find_file",
			Description: "查找文件并返回资源链接",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filename": map[string]interface{}{
						"type":        "string",
						"description": "要查找的文件名",
					},
				},
				"required": []string{"filename"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			filename, _ := req.Params.Arguments["filename"].(string)

			fileURI := fmt.Sprintf("file:///project/src/%s", filename)
			resourceLink := protocol.NewResourceLinkContent(fileURI)

			return &protocol.CallToolResult{
				Content: []protocol.Content{
					protocol.NewTextContent(fmt.Sprintf("找到文件: %s", filename)),
					resourceLink,
				},
			}, nil
		},
	)

	// ========== 启动服务器 ==========
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

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
	log.Println("  ✓ 计算器工具 (calculate)")
	log.Println("  ✓ 时间工具 (get_time)")
	log.Println("  ✓ 基础资源 (info://server)")
	log.Println("  ✓ JSON 配置资源 (config://app)")
	log.Println("  ✓ 资源模板 (echo:///{message})")
	log.Println("  ✓ 提示模板 (code_review)")
	log.Println("  ✓ 资源链接工具 (find_file)")
	log.Println("========================================")
	log.Println("等待客户端连接...")

	if err := mcpServer.Run(ctx, &stdio.StdioTransport{}); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}

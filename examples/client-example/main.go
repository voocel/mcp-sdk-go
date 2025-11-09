package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx := context.Background()
	command := os.Args[1]
	args := os.Args[2:]

	log.Printf("Starting MCP Client")
	log.Printf("   Server: %s %v\n", command, args)

	mcpClient := createClient()
	transport := client.NewCommandTransport(command, args...)

	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer session.Close()

	log.Println("Connected to server")

	printServerInfo(session)

	demonstrateAllFeatures(ctx, session)

	log.Println("\nDone!")
}

func printUsage() {
	fmt.Println("MCP Client Example")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  client-example <command> [args...]")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # 连接 Python 服务器")
	fmt.Println("  client-example python server.py")
	fmt.Println()
	fmt.Println("  # 连接 Node.js 服务器")
	fmt.Println("  client-example node server.js")
	fmt.Println()
	fmt.Println("  # 连接 Go 服务器")
	fmt.Println("  client-example ./calculator-server")
}

// createClient 创建配置好的 MCP 客户端
func createClient() *client.Client {
	return client.NewClient(&client.ClientInfo{
		Name:    "MCP Client Example",
		Version: "1.0.0",
	}, &client.ClientOptions{
		// 处理工具列表变更通知
		ToolListChangedHandler: func(ctx context.Context, notification *protocol.ToolsListChangedNotification) {
			log.Println("Tool list changed notification received")
		},
		// 处理资源列表变更通知
		ResourceListChangedHandler: func(ctx context.Context, notification *protocol.ResourceListChangedParams) {
			log.Println("Resource list changed notification received")
		},
		// 处理提示列表变更通知
		PromptListChangedHandler: func(ctx context.Context, notification *protocol.PromptListChangedParams) {
			log.Println("Prompt list changed notification received")
		},
		// 处理服务器日志消息
		LoggingMessageHandler: func(ctx context.Context, notification *protocol.LoggingMessageParams) {
			log.Printf("Server log [%s]: %v", notification.Level, notification.Data)
		},
		// 处理进度通知
		ProgressNotificationHandler: func(ctx context.Context, notification *protocol.ProgressNotificationParams) {
			log.Printf("Progress: %.0f/%.0f - %s", notification.Progress, notification.Total, notification.Message)
		},
		// 启用 keepalive (每 30 秒 ping 一次)
		KeepAlive: 30 * time.Second,
	})
}

// printServerInfo 打印服务器信息和能力
func printServerInfo(session *client.ClientSession) {
	info := session.InitializeResult()

	log.Printf("\nServer Information:")
	log.Printf("  Name:     %s", info.ServerInfo.Name)
	log.Printf("  Version:  %s", info.ServerInfo.Version)
	log.Printf("  Protocol: %s", info.ProtocolVersion)

	if info.Instructions != "" {
		log.Printf("  Instructions: %s", info.Instructions)
	}

	// 打印服务器能力
	caps := info.Capabilities
	log.Printf("\nServer Capabilities:")

	if caps.Tools != nil {
		log.Printf("  Tools")
		if caps.Tools.ListChanged {
			log.Printf("     - Supports list change notifications")
		}
	}

	if caps.Resources != nil {
		log.Printf("  Resources")
		if caps.Resources.Subscribe {
			log.Printf("     - Supports subscriptions")
		}
		if caps.Resources.ListChanged {
			log.Printf("     - Supports list change notifications")
		}
	}

	if caps.Prompts != nil {
		log.Printf("  Prompts")
		if caps.Prompts.ListChanged {
			log.Printf("     - Supports list change notifications")
		}
	}

	if caps.Logging != nil {
		log.Printf("  Logging")
	}
}

func demonstrateAllFeatures(ctx context.Context, session *client.ClientSession) {
	// Ping
	demonstratePing(ctx, session)

	// Tools
	demonstrateTools(ctx, session)

	// Resources
	demonstrateResources(ctx, session)

	// Prompts
	demonstratePrompts(ctx, session)

	// 等待通知
	log.Println("\nWaiting for notifications (3 seconds)...")
	time.Sleep(3 * time.Second)
}

// demonstratePing Ping 功能
func demonstratePing(ctx context.Context, session *client.ClientSession) {
	log.Println("\nTesting Ping...")
	if err := session.Ping(ctx, nil); err != nil {
		log.Printf("   Ping failed: %v", err)
	} else {
		log.Println("   Ping successful")
	}
}

// demonstrateTools 工具相关功能
func demonstrateTools(ctx context.Context, session *client.ClientSession) {
	log.Println("\nTesting Tools...")

	// 列出工具
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Printf("   Failed to list tools: %v", err)
		return
	}

	log.Printf("   Found %d tools:", len(tools.Tools))
	for i, tool := range tools.Tools {
		log.Printf("      %d. %s - %s", i+1, tool.Name, tool.Description)
	}

	// 如果有工具,尝试调用第一个
	if len(tools.Tools) > 0 {
		tool := tools.Tools[0]
		log.Printf("\n   Calling tool: %s", tool.Name)

		// 构造参数 (根据 schema 或使用空参数)
		args := make(map[string]any)

		result, err := session.CallTool(ctx, &protocol.CallToolParams{
			Name:      tool.Name,
			Arguments: args,
		})

		if err != nil {
			log.Printf("      Tool call failed: %v", err)
		} else {
			log.Printf("      Tool call successful:")
			for _, content := range result.Content {
				if textContent, ok := content.(protocol.TextContent); ok {
					log.Printf("         %s", textContent.Text)
				}
			}
		}
	}
}

// demonstrateResources 资源相关功能
func demonstrateResources(ctx context.Context, session *client.ClientSession) {
	log.Println("\nTesting Resources...")

	// 列出资源
	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		log.Printf("   Failed to list resources: %v", err)
		return
	}

	log.Printf("   Found %d resources:", len(resources.Resources))
	for i, resource := range resources.Resources {
		log.Printf("      %d. %s", i+1, resource.Name)
		log.Printf("         URI: %s", resource.URI)
		if resource.Description != "" {
			log.Printf("         Description: %s", resource.Description)
		}
	}

	// 如果有资源,尝试读取第一个
	if len(resources.Resources) > 0 {
		resource := resources.Resources[0]
		log.Printf("\n   Reading resource: %s", resource.URI)

		result, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
			URI: resource.URI,
		})

		if err != nil {
			log.Printf("      Read failed: %v", err)
		} else {
			log.Printf("      Read successful:")
			for _, content := range result.Contents {
				if content.Text != "" {
					log.Printf("         %s", content.Text)
				}
			}
		}
	}
}

// demonstratePrompts Prompts相关功能
func demonstratePrompts(ctx context.Context, session *client.ClientSession) {
	log.Println("\nTesting Prompts...")

	// 列出提示
	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		log.Printf("   Failed to list prompts: %v", err)
		return
	}

	log.Printf("   Found %d prompts:", len(prompts.Prompts))
	for i, prompt := range prompts.Prompts {
		log.Printf("      %d. %s", i+1, prompt.Name)
		if prompt.Description != "" {
			log.Printf("         Description: %s", prompt.Description)
		}
	}

	// 如果有提示,尝试获取第一个
	if len(prompts.Prompts) > 0 {
		prompt := prompts.Prompts[0]
		log.Printf("\n   Getting prompt: %s", prompt.Name)

		result, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
			Name:      prompt.Name,
			Arguments: map[string]string{},
		})

		if err != nil {
			log.Printf("      Get failed: %v", err)
		} else {
			log.Printf("      Get successful:")
			if result.Description != "" {
				log.Printf("         Description: %s", result.Description)
			}
			log.Printf("         Messages: %d", len(result.Messages))
		}
	}
}

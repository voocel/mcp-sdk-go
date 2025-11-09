package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Println("启动 SSE 客户端...")

	transport, err := sse.NewSSETransport("http://localhost:8080")
	if err != nil {
		log.Fatalf("创建 SSE Transport 失败: %v", err)
	}

	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "SSE Demo Client",
		Version: "1.0.0",
	}, nil)

	log.Println("连接到服务器...")
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer session.Close()

	log.Println("连接成功!")

	// 列出工具
	log.Println("\n列出可用工具...")
	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Fatalf("列出工具失败: %v", err)
	}

	fmt.Printf("找到 %d 个工具:\n", len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
	}

	// 调用 greet 工具
	log.Println("\n调用 greet 工具...")
	greetResult, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "greet",
		Arguments: map[string]interface{}{
			"name": "Go 开发者",
		},
	})
	if err != nil {
		log.Fatalf("调用工具失败: %v", err)
	}

	if len(greetResult.Content) > 0 {
		if textContent, ok := greetResult.Content[0].(protocol.TextContent); ok {
			fmt.Printf("结果: %s\n", textContent.Text)
		}
	}

	// 调用 get_time 工具
	log.Println("\n调用 get_time 工具...")
	timeResult, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name:      "get_time",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		log.Fatalf("调用工具失败: %v", err)
	}

	if len(timeResult.Content) > 0 {
		if textContent, ok := timeResult.Content[0].(protocol.TextContent); ok {
			fmt.Printf("结果: %s\n", textContent.Text)
		}
	}

	log.Println("\n列出可用资源...")
	resourcesResult, err := session.ListResources(ctx, nil)
	if err != nil {
		log.Fatalf("列出资源失败: %v", err)
	}

	fmt.Printf("找到 %d 个资源:\n", len(resourcesResult.Resources))
	for _, resource := range resourcesResult.Resources {
		fmt.Printf("  - %s: %s\n", resource.Name, resource.Description)
	}

	log.Println("\n读取服务器信息资源...")
	resourceResult, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
		URI: "info://server",
	})
	if err != nil {
		log.Fatalf("读取资源失败: %v", err)
	}

	if len(resourceResult.Contents) > 0 {
		content := resourceResult.Contents[0]
		if content.Text != "" {
			fmt.Printf("\n%s\n", content.Text)
		}
	}

	log.Println("\nDone!")
}

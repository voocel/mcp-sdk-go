package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mcpClient, err := client.New(
		client.WithSSETransport("http://localhost:8080"),
		client.WithClientInfo("resource-template-demo", "1.0.0"),
	)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}
	defer mcpClient.Close()

	initResult, err := mcpClient.Initialize(ctx, protocol.ClientInfo{Name: "资源模板客户端", Version: "1.0.0"})
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	fmt.Printf("已连接到 %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	if err := mcpClient.SendInitialized(ctx); err != nil {
		log.Printf("发送initialized通知失败: %v", err)
	}

	templates, err := mcpClient.ListResourceTemplates(ctx, "")
	if err != nil {
		log.Fatalf("获取资源模板失败: %v", err)
	}

	fmt.Println("可用资源模板:")
	for _, tpl := range templates.ResourceTemplates {
		fmt.Printf("- 模板: %s (%s)\n  描述: %s\n", tpl.URITemplate, tpl.MimeType, tpl.Description)
	}

	resourceID := "log://app/latest"
	result, err := mcpClient.ReadResource(ctx, resourceID)
	if err != nil {
		log.Fatalf("读取资源失败: %v", err)
	}

	if len(result.Contents) > 0 {
		fmt.Printf("\n%s 内容:\n%s\n", resourceID, result.Contents[0].Text)
	}
}
